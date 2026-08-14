package tree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Quentin-JH/vtree/internal/config"
	"github.com/Quentin-JH/vtree/internal/db"
	"github.com/Quentin-JH/vtree/internal/manifest"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

// Run executes a workspace-defined command.
//
// Tree-scoped commands run in trees/<name> with the full VTREE_* variable set
// from the tree's manifest; the configured schemas are ensured to exist first
// (idempotent — this is also the backfill path for adopted trees whose _test
// schema was never created). Legacy trees get no VTREE_DB_* injection and no
// schema provisioning: their database story predates the workspace config,
// and pointing env at schemas that don't exist helps nobody.
func Run(ws *workspace.Workspace, name string, treeName string, extraArgs []string) error {
	var cc *config.Command
	for i := range ws.Config.Commands {
		if ws.Config.Commands[i].Name == name {
			cc = &ws.Config.Commands[i]
			break
		}
	}
	if cc == nil {
		var names []string
		for _, c := range ws.Config.Commands {
			names = append(names, c.Name)
		}
		return fmt.Errorf("no command %q in this workspace — defined commands: %s", name, strings.Join(names, ", "))
	}

	var dir string
	var vars map[string]string

	switch cc.Scope {
	case "workspace":
		dir = ws.Root
		vars = BuildVars(ws, "", nil, nil)
		delete(vars, "VTREE_TREE")
		delete(vars, "VTREE_TREE_DIR")
	case "tree":
		if treeName == "" {
			return fmt.Errorf("command %q is tree-scoped: vtree %s <tree>", name, name)
		}
		treeDir := filepath.Join(ws.TreesPath(), treeName)
		if _, err := os.Stat(treeDir); err != nil {
			return fmt.Errorf("no tree named %q — try: vtree ls", treeName)
		}
		m, err := manifest.Read(treeDir)
		if err != nil {
			return err
		}
		if m == nil {
			return fmt.Errorf("tree %q predates vtree (no manifest) — run `vtree adopt %s` first so ports and schemas are known", treeName, treeName)
		}
		dir = treeDir
		if m.Legacy != "" {
			fmt.Fprintf(os.Stderr, "note: %s is a legacy (%s) tree — VTREE_DB_* is not injected and no schemas are provisioned\n", treeName, m.Legacy)
			vars = BuildVars(ws, treeName, m.Ports, nil)
			for k := range vars {
				if strings.HasPrefix(k, "VTREE_DB_") {
					delete(vars, k)
				}
			}
		} else {
			if ws.Config.Database != nil && len(m.Schemas) > 0 {
				ldb := localDB(ws)
				if ldb == nil {
					return fmt.Errorf("this workspace provisions MySQL schemas, but %s has no database block", ws.LocalPath())
				}
				conn, err := db.Connect(ldb)
				if err != nil {
					return err
				}
				for _, s := range m.Schemas {
					if err := db.EnsureSchema(conn, s); err != nil {
						conn.Close()
						return err
					}
				}
				conn.Close()
			}
			vars = BuildVars(ws, treeName, m.Ports, m.Schemas)
		}
	default:
		return fmt.Errorf("command %q has unknown scope %q", name, cc.Scope)
	}

	vars["VTREE_ARGS"] = strings.Join(extraArgs, " ")

	// Same rule as ramp: no spaces → a script path (workspace-root-relative),
	// spaces → a shell command line.
	var cmd *exec.Cmd
	if strings.ContainsAny(cc.Command, " \t") {
		shellArgs := append([]string{"-lc", cc.Command + ` "$@"`, "vtree"}, extraArgs...)
		cmd = exec.Command("bash", shellArgs...)
	} else {
		cmd = exec.Command("bash", append([]string{filepath.Join(ws.Root, cc.Command)}, extraArgs...)...)
	}
	cmd.Dir = dir
	cmd.Env = Environ(vars)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
