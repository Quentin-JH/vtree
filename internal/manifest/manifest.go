// Package manifest reads and writes the per-tree manifest,
// trees/<name>/.vtree-tree.json.
//
// The manifest is a claim, not the truth. It is colocated with the tree so it
// dies with the tree (a central registry drifts the moment anything creates
// or removes a tree without updating it), and it is written with O_EXCL
// immediately after port allocation — before worktree creation, before any
// long-running step — so a concurrent `up` sees both the port claim and a
// name collision instantly. Where a rendered .env also records a port, the
// .env wins on divergence: it is what the app actually binds.
package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const FileName = ".vtree-tree.json"

type Manifest struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	// DisplayName is a human label shown alongside the tree name — the tree
	// name stays the identifier everywhere (directories, branches, schemas).
	DisplayName string            `json:"display_name,omitempty"`
	Ports       map[string]int    `json:"ports,omitempty"`
	Schemas     map[string]string `json:"schemas,omitempty"`
	Branches    map[string]string `json:"branches,omitempty"` // repo name → branch
	// Legacy marks trees adopted from before this tool whose database story
	// does not match the workspace config (e.g. "sqlite"). Database-touching
	// commands refuse on legacy trees rather than pointing env at schemas
	// that do not exist.
	Legacy string `json:"legacy,omitempty"`
}

func Path(treeDir string) string { return filepath.Join(treeDir, FileName) }

// Claim creates the tree directory and writes the manifest exclusively.
// A pre-existing manifest (or tree directory content racing us) is an error.
func Claim(treeDir string, m *Manifest) error {
	if err := os.MkdirAll(treeDir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(Path(treeDir), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("tree %q is already claimed (manifest exists) — a concurrent `vtree up`, or a leftover from a failed one", m.Name)
		}
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(m)
}

// SetDisplayName updates the label in a tree's manifest. Written via a temp
// file + rename so a concurrent reader never sees a half-written manifest.
func SetDisplayName(treeDir, display string) error {
	m, err := Read(treeDir)
	if err != nil {
		return err
	}
	if m == nil {
		return fmt.Errorf("tree has no manifest — run `vtree adopt` first")
	}
	m.DisplayName = display
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path(treeDir) + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path(treeDir))
}

// Read returns the manifest for a tree directory, or (nil, nil) when the tree
// has none — pre-vtree trees exist and must not break scans.
func Read(treeDir string) (*Manifest, error) {
	data, err := os.ReadFile(Path(treeDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", Path(treeDir), err)
	}
	return &m, nil
}
