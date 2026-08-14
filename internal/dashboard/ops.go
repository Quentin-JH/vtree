package dashboard

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
)

// An op is one action run through the vtree binary itself — the same code
// path as the terminal, so every guard behaves identically in both faces.
// Output lines are kept for replay and broadcast to subscribers.
type op struct {
	ID     string   `json:"id"`
	Args   []string `json:"args"`
	mu     sync.Mutex
	lines  []string
	done   bool
	failed bool
	subs   map[chan string]bool
	cmd    *exec.Cmd
}

func (o *op) append(line string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lines = append(o.lines, line)
	for ch := range o.subs {
		select {
		case ch <- line:
		default: // a stalled subscriber must not block the op
		}
	}
}

func (o *op) finish(failed bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.done, o.failed = true, failed
	for ch := range o.subs {
		close(ch)
	}
	o.subs = map[chan string]bool{}
}

// snapshot returns lines so far, completion state, and a live channel for
// what follows (nil when already done).
func (o *op) snapshot() ([]string, bool, bool, chan string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	lines := append([]string(nil), o.lines...)
	if o.done {
		return lines, true, o.failed, nil
	}
	ch := make(chan string, 256)
	o.subs[ch] = true
	return lines, false, false, ch
}

func (o *op) unsubscribe(ch chan string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.subs[ch] {
		delete(o.subs, ch)
		close(ch)
	}
}

// registry serializes mutating ops: one at a time per served workspace, the
// way ramp's server holds a per-project mutex. Reads (status) stay free.
type registry struct {
	mu      sync.Mutex
	nextID  int
	ops     map[string]*op
	running *op
}

func newRegistry() *registry {
	return &registry{ops: map[string]*op{}}
}

// start launches `vtree <args>` in root. A second mutating op while one runs
// is refused with the running op's id, so the UI can attach to it instead.
func (r *registry) start(root string, args []string) (*op, *op, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running != nil {
		return nil, r.running, fmt.Errorf("an operation is already running")
	}
	self, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(self, args...)
	cmd.Dir = root
	// Its own process group, so cancel kills the whole tree of children
	// (concurrently, vite, artisan …), not just the wrapper.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	r.nextID++
	o := &op{ID: strconv.Itoa(r.nextID), Args: args, subs: map[chan string]bool{}, cmd: cmd}
	r.ops[o.ID] = o
	r.running = o

	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			o.append(sc.Text())
		}
		err := cmd.Wait()
		o.finish(err != nil)
		r.mu.Lock()
		if r.running == o {
			r.running = nil
		}
		r.mu.Unlock()
	}()
	return o, nil, nil
}

func (r *registry) get(id string) *op {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ops[id]
}

func (r *registry) cancel(id string) error {
	o := r.get(id)
	if o == nil {
		return fmt.Errorf("no such operation")
	}
	o.mu.Lock()
	done, cmd := o.done, o.cmd
	o.mu.Unlock()
	if done {
		return nil
	}
	// Negative pid = the process group.
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}

// streamSSE replays an op's lines and follows it live as server-sent events.
func streamSSE(w http.ResponseWriter, r *http.Request, o *op) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	writeLine := func(line string) {
		fmt.Fprintf(w, "data: %s\n\n", line)
	}
	writeDone := func(failed bool) {
		fmt.Fprintf(w, "event: done\ndata: {\"failed\": %v}\n\n", failed)
		fl.Flush()
	}

	lines, done, failed, ch := o.snapshot()
	for _, l := range lines {
		writeLine(l)
	}
	fl.Flush()
	if done {
		writeDone(failed)
		return
	}
	defer o.unsubscribe(ch)
	for {
		select {
		case l, open := <-ch:
			if !open {
				o.mu.Lock()
				failed := o.failed
				o.mu.Unlock()
				writeDone(failed)
				return
			}
			writeLine(l)
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}
