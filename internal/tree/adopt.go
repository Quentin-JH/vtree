package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Quentin-JH/vtree/internal/db"
	"github.com/Quentin-JH/vtree/internal/gitx"
	"github.com/Quentin-JH/vtree/internal/manifest"
	"github.com/Quentin-JH/vtree/internal/naming"
	"github.com/Quentin-JH/vtree/internal/ports"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

// LegacyUnmanagedDB marks an adopted tree whose env does not reference the
// schemas this workspace would derive for it (a pre-MySQL tree, e.g. sqlite).
const LegacyUnmanagedDB = "unmanaged-db"

// Adopt brings a pre-vtree tree under management by writing its manifest.
//
// It writes ONLY the manifest and tree-root template files — never anything
// inside a repo worktree. Live trees routinely hold parallel sessions' dirty
// state; an in-repo write would corrupt someone's working tree.
func Adopt(ws *workspace.Workspace, name string) (warnings []string, err error) {
	treeDir := filepath.Join(ws.TreesPath(), name)
	if st, serr := os.Stat(treeDir); serr != nil || !st.IsDir() {
		return nil, fmt.Errorf("no tree named %q — try: vtree ls", name)
	}
	if m, merr := manifest.Read(treeDir); merr != nil {
		return nil, merr
	} else if m != nil {
		return nil, fmt.Errorf("tree %q already has a manifest", name)
	}

	m := &manifest.Manifest{
		Name:      name,
		CreatedAt: time.Now().UTC(),
		Ports:     namedEnvPorts(ws, treeDir),
		Branches:  map[string]string{},
	}
	for _, repo := range ws.Config.Repos {
		wt := filepath.Join(treeDir, repo.Name)
		if branch, berr := gitx.Run(wt, "rev-parse", "--abbrev-ref", "HEAD"); berr == nil {
			m.Branches[repo.Name] = branch
		} else {
			warnings = append(warnings, fmt.Sprintf("no worktree for repo %s", repo.Name))
		}
	}

	if ws.Config.Database != nil {
		schemas := naming.SchemaSet(ws.Config.Database.Prefix, name, ws.Config.Database.Schemas)
		if envReferencesSchemas(ws, treeDir, schemas) {
			m.Schemas = schemas
			// Ensure the schemas exist — pre-vtree trees often never got a
			// _test schema. Non-fatal: adopt must work with MySQL down.
			if ldb := localDB(ws); ldb != nil {
				if conn, cerr := db.Connect(ldb); cerr != nil {
					warnings = append(warnings, fmt.Sprintf("cannot ensure schemas: %v", cerr))
				} else {
					for _, s := range schemas {
						if eerr := db.EnsureSchema(conn, s); eerr != nil {
							warnings = append(warnings, fmt.Sprintf("cannot ensure %s: %v", s, eerr))
						}
					}
					conn.Close()
				}
			} else {
				warnings = append(warnings, "schemas recorded but not ensured — no database block in local.yaml")
			}
		} else {
			m.Legacy = LegacyUnmanagedDB
			warnings = append(warnings, fmt.Sprintf("tree %q does not use this workspace's schemas — marked legacy (%s); db-dependent commands will not inject VTREE_DB_*", name, LegacyUnmanagedDB))
		}
	}

	if err := manifest.Claim(treeDir, m); err != nil {
		return warnings, err
	}

	// Refresh tree-root templates (CLAUDE.md and friends). They live outside
	// every repo worktree, so rewriting them cannot dirty anyone's work. A
	// template whose dest would land inside a repo dir is skipped.
	repoNames := map[string]bool{}
	for _, r := range ws.Config.Repos {
		repoNames[r.Name] = true
	}
	for _, t := range ws.Config.Templates {
		first := strings.SplitN(filepath.ToSlash(t.Dest), "/", 2)[0]
		if repoNames[first] {
			warnings = append(warnings, fmt.Sprintf("template %s targets repo dir %s — skipped during adopt", t.Source, first))
			continue
		}
		if err := copyFile(filepath.Join(ws.Root, t.Source), filepath.Join(treeDir, t.Dest), 0o644); err != nil {
			warnings = append(warnings, fmt.Sprintf("template %s: %v", t.Source, err))
		}
	}
	return warnings, nil
}

// VerifyAdopted returns the tree names that still lack a manifest.
func VerifyAdopted(ws *workspace.Workspace) ([]string, error) {
	entries, err := os.ReadDir(ws.TreesPath())
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := manifest.Read(filepath.Join(ws.TreesPath(), e.Name()))
		if err != nil {
			return nil, err
		}
		if m == nil {
			missing = append(missing, e.Name())
		}
	}
	return missing, nil
}

// namedEnvPorts maps port names to values by finding, for each configured
// port name, an env key whose template fills it with ${VTREE_PORT_<NAME>} and
// reading that key's rendered value back out of the tree.
func namedEnvPorts(ws *workspace.Workspace, treeDir string) map[string]int {
	if ws.Config.Ports == nil {
		return nil
	}
	out := map[string]int{}
	for _, pname := range ws.Config.Ports.Names {
		ref := "${VTREE_PORT_" + strings.ToUpper(pname) + "}"
		for _, repo := range ws.Config.Repos {
			for _, ef := range repo.EnvFiles {
				for _, key := range ef.Set.Keys {
					if !strings.Contains(ef.Set.Values[key], ref) {
						continue
					}
					data, err := os.ReadFile(filepath.Join(treeDir, repo.Name, ef.Dest))
					if err != nil {
						continue
					}
					for _, line := range strings.Split(string(data), "\n") {
						if v, ok := strings.CutPrefix(line, key+"="); ok {
							if ps := ports.PortsInValue(v); len(ps) > 0 {
								out[pname] = ps[0]
							}
						}
					}
				}
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// envReferencesSchemas reports whether the tree's rendered env carries the
// schema names this workspace derives for it — the signal that the tree is on
// the managed database story rather than something older.
func envReferencesSchemas(ws *workspace.Workspace, treeDir string, schemas map[string]string) bool {
	wanted := map[string]string{}
	for sname, full := range schemas {
		wanted["${VTREE_DB_"+strings.ToUpper(sname)+"}"] = full
	}
	for _, repo := range ws.Config.Repos {
		for _, ef := range repo.EnvFiles {
			for _, key := range ef.Set.Keys {
				full, refs := "", false
				for ref, f := range wanted {
					if strings.Contains(ef.Set.Values[key], ref) {
						full, refs = f, true
						break
					}
				}
				if !refs {
					continue
				}
				data, err := os.ReadFile(filepath.Join(treeDir, repo.Name, ef.Dest))
				if err != nil {
					continue
				}
				for _, line := range strings.Split(string(data), "\n") {
					if v, ok := strings.CutPrefix(line, key+"="); ok && strings.TrimSpace(v) == full {
						return true
					}
				}
			}
		}
	}
	return false
}
