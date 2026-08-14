package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Quentin-JH/vtree/internal/gitx"
	"github.com/Quentin-JH/vtree/internal/manifest"
	"github.com/Quentin-JH/vtree/internal/ports"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

// RepoState is one repo's work-at-risk accounting inside a tree.
type RepoState struct {
	Repo     string   `json:"repo"`
	Branch   string   `json:"branch"`
	Dirty    []string `json:"dirty,omitempty"`    // porcelain lines, ignore-filtered
	Unpushed []string `json:"unpushed,omitempty"` // oneline commits on no remote
	// Err records a repo whose state could not be verified. The down guard
	// treats this as a refusal: an uninspectable repo must never read as
	// clean and then be deleted.
	Err string `json:"error,omitempty"`
}

func (r RepoState) AtRisk() bool {
	return len(r.Dirty) > 0 || len(r.Unpushed) > 0 || r.Err != ""
}

// Inspect gathers per-repo state for a tree. Dirty means every `git status
// --porcelain` entry — staged, unstaged, AND untracked; untracked files are
// exactly what the 2026-08-04 data loss was made of — minus lines matching
// the workspace's ignore_dirty patterns (substring regex against the line).
func Inspect(ws *workspace.Workspace, name string) []RepoState {
	var ignores []*regexp.Regexp
	for _, pat := range ws.Config.IgnoreDirty {
		// Patterns were validated at config load.
		ignores = append(ignores, regexp.MustCompile(pat))
	}

	treeDir := filepath.Join(ws.TreesPath(), name)
	out := make([]RepoState, 0, len(ws.Config.Repos))
	for _, repo := range ws.Config.Repos {
		st := RepoState{Repo: repo.Name}
		wt := filepath.Join(treeDir, repo.Name)
		if _, err := os.Stat(wt); err != nil {
			st.Err = fmt.Sprintf("worktree missing at %s", wt)
			out = append(out, st)
			continue
		}
		branch, err := gitx.Run(wt, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			st.Err = err.Error()
			out = append(out, st)
			continue
		}
		st.Branch = branch

		porcelain, err := gitx.Run(wt, "status", "--porcelain")
		if err != nil {
			st.Err = err.Error()
			out = append(out, st)
			continue
		}
	lines:
		for _, line := range splitLines(porcelain) {
			for _, re := range ignores {
				if re.MatchString(line) {
					continue lines
				}
			}
			st.Dirty = append(st.Dirty, line)
		}

		unpushed, err := gitx.Run(wt, "log", "--oneline", "HEAD", "--not", "--remotes")
		if err != nil {
			st.Err = err.Error()
			out = append(out, st)
			continue
		}
		st.Unpushed = splitLines(unpushed)
		out = append(out, st)
	}
	return out
}

// TreeInfo is one row of ls / status.
type TreeInfo struct {
	Name          string            `json:"name"`
	Ports         map[string]int    `json:"ports,omitempty"`
	PortsDiverged bool              `json:"ports_diverged,omitempty"`
	HasManifest   bool              `json:"has_manifest"`
	Legacy        string            `json:"legacy,omitempty"`
	Repos         []RepoState       `json:"repos"`
	Schemas       map[string]string `json:"schemas,omitempty"`
}

func (t TreeInfo) DirtyCount() int {
	n := 0
	for _, r := range t.Repos {
		n += len(r.Dirty)
	}
	return n
}

func (t TreeInfo) UnpushedCount() int {
	n := 0
	for _, r := range t.Repos {
		n += len(r.Unpushed)
	}
	return n
}

// Collect builds the ls/status rows for every tree directory.
func Collect(ws *workspace.Workspace) ([]TreeInfo, error) {
	entries, err := os.ReadDir(ws.TreesPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []TreeInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		treeDir := filepath.Join(ws.TreesPath(), name)
		info := TreeInfo{Name: name, Repos: Inspect(ws, name)}

		m, merr := manifest.Read(treeDir)
		if merr == nil && m != nil {
			info.HasManifest = true
			info.Ports = m.Ports
			info.Legacy = m.Legacy
			info.Schemas = m.Schemas
		}

		// Reconcile port claims against the rendered .env — the .env is what
		// the app binds, so a disagreement is worth a flag, and for a
		// manifest-less tree the .env is the only source there is.
		envPorts := ports.EnvPorts(treeDir, ws.Config)
		if info.HasManifest && len(envPorts) > 0 {
			claimed := map[int]bool{}
			for _, p := range info.Ports {
				claimed[p] = true
			}
			for _, p := range envPorts {
				if !claimed[p] {
					info.PortsDiverged = true
				}
			}
		}
		if !info.HasManifest && len(envPorts) > 0 {
			// EnvPorts returns one entry per env key occurrence; several keys
			// carry the same port. Dedupe for display.
			uniq := map[int]bool{}
			for _, p := range envPorts {
				uniq[p] = true
			}
			var distinct []int
			for p := range uniq {
				distinct = append(distinct, p)
			}
			sort.Ints(distinct)
			info.Ports = map[string]int{}
			for i, p := range distinct {
				info.Ports[fmt.Sprintf("p%d", i+1)] = p
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}
