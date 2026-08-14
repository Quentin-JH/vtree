// Package workspace locates a vtree workspace from any directory inside it,
// the way git finds its repository root — so the command works from a tree,
// a repo checkout, or the workspace root itself.
package workspace

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Quentin-JH/vtree/internal/config"
)

const (
	ConfigDir   = ".vtree"
	ConfigFile  = "vtree.yaml"
	LocalFile   = "local.yaml"
	TreesDir    = "trees"
	ReposDir    = "repos"
	ScriptsDir  = "scripts"
	TemplateDir = "templates"
)

type Workspace struct {
	Root   string
	Config *config.Config
	// Local is nil when .vtree/local.yaml does not exist. Anything that
	// touches a database must refuse while it is nil rather than fall back to
	// defaults.
	Local *config.Local
}

func (w *Workspace) ConfigPath() string { return filepath.Join(w.Root, ConfigDir, ConfigFile) }
func (w *Workspace) LocalPath() string  { return filepath.Join(w.Root, ConfigDir, LocalFile) }
func (w *Workspace) TreesPath() string  { return filepath.Join(w.Root, TreesDir) }
func (w *Workspace) RepoPath(name string) string {
	return filepath.Join(w.Root, ReposDir, name)
}

// FindRoot walks upward from start looking for .vtree/vtree.yaml.
func FindRoot(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ConfigDir, ConfigFile)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no %s/%s found in %s or any parent — run from inside a vtree workspace, or create one with `vtree init`", ConfigDir, ConfigFile, start)
		}
		dir = parent
	}
}

// Open finds and fully loads the workspace containing start. A missing
// local.yaml is not an error here; it leaves Local nil.
func Open(start string) (*Workspace, error) {
	root, err := FindRoot(start)
	if err != nil {
		return nil, err
	}
	w := &Workspace{Root: root}
	w.Config, err = config.Load(w.ConfigPath())
	if err != nil {
		return nil, err
	}
	loc, err := config.LoadLocal(w.LocalPath())
	switch {
	case err == nil:
		w.Local = loc
	case os.IsNotExist(err):
		// fine — commands that need it refuse individually
	default:
		return nil, err
	}
	return w, nil
}
