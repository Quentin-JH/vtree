package tree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Quentin-JH/vtree/internal/db"
	"github.com/Quentin-JH/vtree/internal/gitx"
	"github.com/Quentin-JH/vtree/internal/manifest"
	"github.com/Quentin-JH/vtree/internal/naming"
	"github.com/Quentin-JH/vtree/internal/ports"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

// Teardown removes a tree's ports, schemas, worktrees, branches, and
// directory. It makes no safety decision — the caller (down's guard, or up's
// rollback of a tree that holds no user work yet) has already made it.
//
// Every step is warn-and-continue: a stopped MySQL, a half-created worktree,
// or a missing manifest must not wedge a teardown and strand a tree in a
// state no tool can produce on purpose. It always reaches directory removal.
func Teardown(ws *workspace.Workspace, name string) (warnings []string) {
	warn := func(format string, a ...any) {
		warnings = append(warnings, fmt.Sprintf(format, a...))
	}
	treeDir := filepath.Join(ws.TreesPath(), name)

	m, err := manifest.Read(treeDir)
	if err != nil {
		warn("unreadable manifest: %v", err)
	}

	// Kill processes on this tree's ports — but never a port another tree
	// claims, so a lying manifest cannot take down a neighbor's dev server.
	for _, p := range treePortsOf(ws, treeDir, m) {
		if claimedElsewhere(ws, name, p) {
			warn("port %d is also claimed by another tree — not killing it", p)
			continue
		}
		killPort(p)
	}

	// Schemas are local dev data only — setup rebuilds them from scratch.
	if ws.Config.Database != nil && (m == nil || m.Legacy == "") {
		schemas := schemasOf(ws, name, m)
		if ldb := localDB(ws); ldb == nil {
			warn("cannot drop schemas %v: no database block in %s", schemas, ws.LocalPath())
		} else if conn, err := db.Connect(ldb); err != nil {
			warn("cannot drop schemas %v: %v", schemas, err)
		} else {
			defer conn.Close()
			for _, s := range schemas {
				if err := db.DropSchema(conn, s); err != nil {
					warn("could not drop %s: %v", s, err)
				} else {
					fmt.Println("dropped", s)
				}
			}
		}
	}

	for _, repo := range ws.Config.Repos {
		wt := filepath.Join(treeDir, repo.Name)
		// Branch name captured BEFORE removal — unresolvable after.
		branch, _ := gitx.Run(wt, "rev-parse", "--abbrev-ref", "HEAD")
		src := ws.RepoPath(repo.Name)
		// Forced removal: the guard already decided; ignore-listed noise and
		// generated .env files would block a plain remove.
		if _, err := gitx.Run(src, "worktree", "remove", "--force", wt); err != nil {
			warn("worktree remove %s: %v", repo.Name, err)
		}
		if branch != "" && branch != "HEAD" {
			if _, err := gitx.Run(src, "branch", "-D", branch); err != nil {
				warn("branch -D %s in %s: %v", branch, repo.Name, err)
			}
		}
	}

	if err := os.RemoveAll(treeDir); err != nil {
		warn("removing %s: %v", treeDir, err)
	}
	for _, repo := range ws.Config.Repos {
		if _, err := gitx.Run(ws.RepoPath(repo.Name), "worktree", "prune"); err != nil {
			warn("worktree prune %s: %v", repo.Name, err)
		}
	}
	return warnings
}

// treePortsOf merges the manifest's port claims with what the tree's rendered
// .env files actually say — the .env is what the app binds, so it is included
// even when the manifest disagrees.
func treePortsOf(ws *workspace.Workspace, treeDir string, m *manifest.Manifest) []int {
	seen := map[int]bool{}
	if m != nil {
		for _, p := range m.Ports {
			seen[p] = true
		}
	}
	for _, p := range ports.EnvPorts(treeDir, ws.Config) {
		seen[p] = true
	}
	out := make([]int, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

func claimedElsewhere(ws *workspace.Workspace, name string, port int) bool {
	entries, err := os.ReadDir(ws.TreesPath())
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == name {
			continue
		}
		otherDir := filepath.Join(ws.TreesPath(), e.Name())
		if om, _ := manifest.Read(otherDir); om != nil {
			for _, p := range om.Ports {
				if p == port {
					return true
				}
			}
		}
		for _, p := range ports.EnvPorts(otherDir, ws.Config) {
			if p == port {
				return true
			}
		}
	}
	return false
}

// killPort terminates listeners on the port. lsof is used when present; when
// it is not (some linux boxes), the processes are left running and teardown
// continues — a surviving dev server is annoying, a wedged teardown is worse.
func killPort(port int) {
	lsof, err := exec.LookPath("lsof")
	if err != nil {
		return
	}
	out, err := exec.Command(lsof, "-ti:"+strconv.Itoa(port)).Output()
	if err != nil {
		return // nothing listening
	}
	for _, pid := range strings.Fields(string(out)) {
		exec.Command("kill", pid).Run()
	}
}

func schemasOf(ws *workspace.Workspace, name string, m *manifest.Manifest) []string {
	if m != nil && len(m.Schemas) > 0 {
		out := make([]string, 0, len(m.Schemas))
		for _, s := range m.Schemas {
			out = append(out, s)
		}
		return out
	}
	set := naming.SchemaSet(ws.Config.Database.Prefix, name, ws.Config.Database.Schemas)
	out := make([]string, 0, len(set))
	for _, s := range set {
		out = append(out, s)
	}
	return out
}
