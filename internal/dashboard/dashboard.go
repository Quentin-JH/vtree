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
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Quentin-JH/vtree/internal/gitx"
	"github.com/Quentin-JH/vtree/internal/manifest"
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

// allowed maps action names to the argv they run. Only these reach the
// binary: the API must never become a general command runner, and custom
// workspace commands go through `run` where scope rules apply.
func actionArgs(root string, action string, body map[string]string) ([]string, error) {
	name := body["name"]
	switch action {
	case "up":
		args := []string{"up", name}
		if body["base"] != "" {
			args = append(args, "--base", body["base"])
		}
		return args, nil
	case "down":
		args := []string{"down", name}
		if body["force"] == "true" {
			args = append(args, "--force")
		}
		return args, nil
	case "run":
		if body["command"] == "" {
			return nil, fmt.Errorf("command required")
		}
		args := []string{"run", body["command"]}
		if name != "" {
			args = append(args, name)
		}
		return args, nil
	case "install":
		return []string{"install"}, nil
	case "adopt":
		return []string{"adopt", name}, nil
	default:
		return nil, fmt.Errorf("unknown action %q", action)
	}
}

// Serve blocks, serving the dashboard for the workspace at root.
func Serve(root, version string, port int) error {
	mux := Handler(root, version)
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %w (another vtree ui running? pass --port)", addr, err)
	}
	fmt.Printf("vtree dashboard → http://%s\n", addr)
	return http.Serve(ln, mux)
}

// Handler builds the dashboard routes — shared by `vtree ui` and the app.
func Handler(root, version string) *http.ServeMux {
	mux := http.NewServeMux()
	reg := newRegistry()

	// Mutating actions: POST /api/actions/{up|down|run|install|adopt}.
	// Body: {"name": ..., "base": ..., "force": "true", "command": ...}.
	// One at a time; a busy registry answers 409 with the running op.
	mux.HandleFunc("/api/actions/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		args, err := actionArgs(root, strings.TrimPrefix(r.URL.Path, "/api/actions/"), body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		o, running, err := reg.start(root, args)
		if err != nil {
			if running != nil {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "running": running.ID, "args": running.Args})
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": o.ID})
	})

	// GET /api/ops/{id}/stream — SSE replay + follow. POST /api/ops/{id}/cancel.
	mux.HandleFunc("/api/ops/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/ops/")
		id, verb, _ := strings.Cut(rest, "/")
		o := reg.get(id)
		if o == nil {
			http.NotFound(w, r)
			return
		}
		switch verb {
		case "stream":
			streamSSE(w, r, o)
		case "cancel":
			if r.Method != http.MethodPost {
				http.Error(w, "POST only", http.StatusMethodNotAllowed)
				return
			}
			if err := reg.cancel(id); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	})

	// GET /api/inspect/{tree} — the down guard's view: exact dirty lines and
	// unpushed commits per repo, for the delete dialog.
	mux.HandleFunc("/api/inspect/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/api/inspect/")
		ws, err := workspace.Open(root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tree.Inspect(ws, name))
	})

	// GET /api/meta — workspace identity, stat row, repo rows, commands.
	mux.HandleFunc("/api/meta", func(w http.ResponseWriter, r *http.Request) {
		ws, err := workspace.Open(root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		type repoMeta struct {
			Name      string `json:"name"`
			Installed bool   `json:"installed"`
			BaseRef   string `json:"base_ref"`
			Branch    string `json:"branch,omitempty"`
			Behind    int    `json:"behind"` // source clone vs its origin (as of last fetch)
		}
		var repos []repoMeta
		for _, repo := range ws.Config.Repos {
			rm := repoMeta{Name: repo.Name, BaseRef: repo.BaseRef}
			path := ws.RepoPath(repo.Name)
			if st, err := os.Stat(path); err == nil && st.IsDir() {
				rm.Installed = true
				rm.Branch, _ = gitx.Run(path, "rev-parse", "--abbrev-ref", "HEAD")
				if n, err := gitx.Run(path, "rev-list", "--count", "HEAD..origin/"+rm.Branch); err == nil {
					fmt.Sscanf(n, "%d", &rm.Behind)
				}
			}
			repos = append(repos, rm)
		}
		var commands []string
		for _, c := range ws.Config.Commands {
			if c.Scope == "tree" {
				commands = append(commands, c.Name)
			}
		}
		out := map[string]any{
			"name": ws.Config.Name, "path": ws.Root,
			"repos": repos, "commands": commands,
			"base_port": 0, "branch_prefix": "",
		}
		if ws.Config.Ports != nil {
			out["base_port"] = ws.Config.Ports.Base
		}
		if len(ws.Config.Repos) > 0 {
			out["branch_prefix"] = ws.Config.Repos[0].BranchPrefix
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})

	// POST /api/rename — set or clear a tree's display name. Instant and
	// non-destructive, so it skips the op ceremony.
	mux.HandleFunc("/api/rename", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		ws, err := workspace.Open(root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		treeDir := filepath.Join(ws.TreesPath(), body["name"])
		if _, err := os.Stat(treeDir); err != nil {
			http.Error(w, "no such tree", http.StatusNotFound)
			return
		}
		if err := manifest.SetDisplayName(treeDir, strings.TrimSpace(body["display"])); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// POST /api/refresh — fetch origin in every source repo, so the behind
	// counts and the next `up` see fresh remote refs. Synchronous; seconds.
	mux.HandleFunc("/api/refresh", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		ws, err := workspace.Open(root)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, repo := range ws.Config.Repos {
			if _, err := gitx.Run(ws.RepoPath(repo.Name), "fetch", "-q", "origin"); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})

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

	return mux
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
