// Package dashboard serves the browser view of a workspace: the same data
// `vtree status --json` reports, on a local page that refreshes itself. It
// binds 127.0.0.1 only — this is a window onto your machine's trees, not a
// network service.
package dashboard

import (
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Quentin-JH/vtree/internal/tree"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

//go:embed page.html
var pageFS embed.FS

type payload struct {
	Workspace   string     `json:"workspace"`
	Version     string     `json:"version"`
	GeneratedAt time.Time  `json:"generated_at"`
	Trees       []treeView `json:"trees"`
}

type treeView struct {
	tree.TreeInfo
	// Listening maps port name → whether something is serving on it right
	// now — the "is my dev server up" signal.
	Listening map[string]bool `json:"listening,omitempty"`
	Dirty     int             `json:"dirty"`
	Unpushed  int             `json:"unpushed"`
}

// Serve blocks, serving the dashboard for the workspace at root.
func Serve(root, version string, port int) error {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		// Reopen per request: config edits show up without a restart.
		ws, err := workspace.Open(root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		infos, err := tree.Collect(ws)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out := payload{
			Workspace:   ws.Config.Name,
			Version:     version,
			GeneratedAt: time.Now(),
			Trees:       make([]treeView, len(infos)),
		}
		var wg sync.WaitGroup
		for i, info := range infos {
			out.Trees[i] = treeView{TreeInfo: info, Dirty: info.DirtyCount(), Unpushed: info.UnpushedCount()}
			if len(info.Ports) == 0 {
				continue
			}
			wg.Add(1)
			go func(i int, ports map[string]int) {
				defer wg.Done()
				l := map[string]bool{}
				for name, p := range ports {
					l[name] = dialing(p)
				}
				out.Trees[i].Listening = l
			}(i, info.Ports)
		}
		wg.Wait()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		page, _ := pageFS.ReadFile("page.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(page)
	})

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w (another vtree ui running? pass --port)", addr, err)
	}
	fmt.Printf("vtree dashboard → http://%s\n", addr)
	return http.Serve(ln, mux)
}

// dialing reports whether something accepts connections on the port — unlike
// a bind probe, this answers "is a server actually running", and never
// competes for the port.
func dialing(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
