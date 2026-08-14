// Package scaffold writes new workspace files from collected answers. It is
// deliberately separate from the interactive forms so the generated output is
// testable: every generated vtree.yaml is round-tripped through the strict
// config loader before this package reports success.
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Quentin-JH/vtree/internal/config"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

type RepoAnswer struct {
	Name    string
	Git     string
	BaseRef string
}

type Answers struct {
	Name      string
	Repos     []RepoAnswer
	PortBase  int      // 0 = no ports block
	PortNames []string
	DBPrefix  string // "" = no database block
	DBSchemas []string
	PRBase    string // "" = no pr block
	PRGuard   []string
}

// WriteWorkspace scaffolds .vtree/vtree.yaml, .gitignore entries, and the
// scripts directory in dir. It refuses to overwrite an existing vtree.yaml.
func WriteWorkspace(dir string, a Answers) ([]string, error) {
	cfgPath := filepath.Join(dir, workspace.ConfigDir, workspace.ConfigFile)
	if _, err := os.Stat(cfgPath); err == nil {
		return nil, fmt.Errorf("%s already exists — this is already a vtree workspace", cfgPath)
	}

	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("name: %s\n", a.Name)
	w("repos:\n")
	for _, r := range a.Repos {
		w("  - name: %s\n", r.Name)
		w("    git: %s\n", r.Git)
		w("    base_ref: %s\n", r.BaseRef)
	}
	if a.PortBase > 0 {
		w("ports:\n  base: %d\n  names: [%s]\n", a.PortBase, strings.Join(a.PortNames, ", "))
	}
	if a.DBPrefix != "" {
		w("database:\n  prefix: %s\n  schemas: [%s]\n", a.DBPrefix, strings.Join(a.DBSchemas, ", "))
	}
	w("# Uncomment to run a script after `vtree up` (cwd = the new tree,\n")
	w("# VTREE_* variables in the environment):\n")
	w("# setup: .vtree/scripts/setup.sh\n")
	if a.PRBase != "" {
		w("pr:\n  base: %s\n", a.PRBase)
		if len(a.PRGuard) > 0 {
			w("  guard_against: [%s]\n", strings.Join(a.PRGuard, ", "))
		}
	}

	if err := os.MkdirAll(filepath.Join(dir, workspace.ConfigDir, workspace.ScriptsDir), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(cfgPath, []byte(b.String()), 0o644); err != nil {
		return nil, err
	}
	// The strict loader is the arbiter of whether we generated a valid file.
	if _, err := config.Load(cfgPath); err != nil {
		return nil, fmt.Errorf("scaffold produced an invalid config (bug): %w", err)
	}

	giPath := filepath.Join(dir, ".gitignore")
	created := []string{cfgPath}
	if p, err := ensureGitignore(giPath); err != nil {
		return nil, err
	} else if p {
		created = append(created, giPath)
	}
	return created, nil
}

// WriteLocal writes .vtree/local.yaml — the machine-specific half a teammate
// creates on first run in a cloned workspace.
func WriteLocal(dir, host string, port int, user, pass string) (string, error) {
	path := filepath.Join(dir, workspace.ConfigDir, workspace.LocalFile)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}
	content := fmt.Sprintf("database: { host: %s, port: %d, user: %s, password: %q }\n", host, port, user, pass)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	if _, err := config.LoadLocal(path); err != nil {
		return "", fmt.Errorf("scaffold produced an invalid local.yaml (bug): %w", err)
	}
	return path, nil
}

// ensureGitignore appends the vtree-managed entries that are missing.
// Returns whether the file was touched.
func ensureGitignore(path string) (bool, error) {
	entries := []string{"repos/", "trees/", ".vtree/local.yaml"}
	existing, _ := os.ReadFile(path)
	lines := map[string]bool{}
	for _, l := range strings.Split(string(existing), "\n") {
		lines[strings.TrimSpace(l)] = true
	}
	var missing []string
	for _, e := range entries {
		if !lines[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}
	out := string(existing)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += strings.Join(missing, "\n") + "\n"
	return true, os.WriteFile(path, []byte(out), 0o644)
}
