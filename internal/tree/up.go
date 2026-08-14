// Package tree creates and removes trees: the multi-repo worktree
// directories under trees/<name>.
package tree

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Quentin-JH/vtree/internal/config"
	"github.com/Quentin-JH/vtree/internal/db"
	"github.com/Quentin-JH/vtree/internal/envfile"
	"github.com/Quentin-JH/vtree/internal/gitx"
	"github.com/Quentin-JH/vtree/internal/manifest"
	"github.com/Quentin-JH/vtree/internal/naming"
	"github.com/Quentin-JH/vtree/internal/ports"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

type UpOptions struct {
	Name string
	// BaseOverrides maps repo name → ref, overriding that repo's configured
	// base_ref for this invocation. The empty-string key applies to a
	// single-repo workspace (`--base origin/main` without naming the repo).
	BaseOverrides map[string]string
}

// Up creates a tree. On any failure after the tree is claimed, it rolls the
// partial tree back and returns the original error.
func Up(ws *workspace.Workspace, opt UpOptions) error {
	cfg := ws.Config
	name := opt.Name
	if err := naming.ValidateTreeName(name); err != nil {
		return err
	}
	treeDir := filepath.Join(ws.TreesPath(), name)
	if _, err := os.Stat(treeDir); err == nil {
		return fmt.Errorf("tree %q already exists", name)
	}

	for _, repo := range cfg.Repos {
		if st, err := os.Stat(ws.RepoPath(repo.Name)); err != nil || !st.IsDir() {
			return fmt.Errorf("no clone of %s at %s — run `vtree install` first", repo.Name, ws.RepoPath(repo.Name))
		}
	}

	// Database requirements are checked before anything is created: no
	// local.yaml means no defaults to fall back to, so refuse up front.
	var schemas map[string]string
	var conn *sql.DB
	if cfg.Database != nil {
		ldb := localDB(ws)
		if ldb == nil {
			return fmt.Errorf("this workspace provisions MySQL schemas per tree, but %s has no database block — create it before `vtree up` (vtree has no built-in connection defaults)", ws.LocalPath())
		}
		schemas = naming.SchemaSet(cfg.Database.Prefix, name, cfg.Database.Schemas)
		if err := checkSchemaCollisions(ws, name, schemas); err != nil {
			return err
		}
		c, err := db.Connect(ldb)
		if err != nil {
			return err
		}
		conn = c
		defer conn.Close()
	}

	// Fetch first, then verify the base refs exist. Cutting from a stale
	// origin/<branch> is its own incident class.
	for _, repo := range cfg.Repos {
		base := baseRef(repo, opt.BaseOverrides, len(cfg.Repos))
		fmt.Printf("fetching %s\n", repo.Name)
		if _, err := gitx.Run(ws.RepoPath(repo.Name), "fetch", "-q", "origin"); err != nil {
			return err
		}
		if _, err := gitx.Run(ws.RepoPath(repo.Name), "rev-parse", "--verify", "-q", base); err != nil {
			return fmt.Errorf("base ref %q does not exist in %s", base, repo.Name)
		}
		branch := repo.BranchPrefix + name
		if _, err := gitx.Run(ws.RepoPath(repo.Name), "show-ref", "--verify", "-q", "refs/heads/"+branch); err == nil {
			return fmt.Errorf("branch %q already exists in %s — pick another tree name or delete the branch", branch, repo.Name)
		}
	}

	allocated, err := ports.Allocate(ws.TreesPath(), cfg)
	if err != nil {
		return err
	}

	// Claim: the manifest goes down immediately after allocation, before the
	// worktrees and before any long-running step, so a concurrent `up` sees
	// the port claim and any name collision instantly.
	m := &manifest.Manifest{
		Name:      name,
		CreatedAt: time.Now().UTC(),
		Ports:     allocated,
		Schemas:   schemas,
		Branches:  map[string]string{},
	}
	for _, repo := range cfg.Repos {
		m.Branches[repo.Name] = repo.BranchPrefix + name
	}
	if err := manifest.Claim(treeDir, m); err != nil {
		return err
	}

	fmt.Printf("%s — ports %v\n", name, allocated)
	if err := buildTree(ws, name, treeDir, m, conn, opt); err != nil {
		fmt.Fprintf(os.Stderr, "up failed: %v\nrolling back partial tree %q\n", err, name)
		for _, w := range Teardown(ws, name) {
			fmt.Fprintln(os.Stderr, "  warning:", w)
		}
		return err
	}
	fmt.Printf("%s ready\n", name)
	return nil
}

// buildTree runs everything after the claim; a failure anywhere in here
// triggers rollback in Up.
func buildTree(ws *workspace.Workspace, name, treeDir string, m *manifest.Manifest, conn *sql.DB, opt UpOptions) error {
	cfg := ws.Config

	for _, repo := range cfg.Repos {
		base := baseRef(repo, opt.BaseOverrides, len(cfg.Repos))
		branch := m.Branches[repo.Name]
		path := filepath.Join(treeDir, repo.Name)
		fmt.Printf("worktree %s @ %s (%s)\n", repo.Name, branch, base)
		if _, err := gitx.Run(ws.RepoPath(repo.Name), "worktree", "add", "-q", "-b", branch, path, base); err != nil {
			return err
		}
	}

	// Templates first: an agent dispatched into a half-built tree must still
	// inherit the convention chain (CLAUDE.md and friends).
	for _, t := range cfg.Templates {
		src := filepath.Join(ws.Root, t.Source)
		dst := filepath.Join(treeDir, t.Dest)
		if err := copyFile(src, dst, 0o644); err != nil {
			return fmt.Errorf("template %s: %w", t.Source, err)
		}
	}

	// Hooks land in the source clone's SHARED hooks directory — worktrees
	// share the common git dir, so this affects every tree of that repo and
	// is intentionally idempotent (same content each time).
	for _, repo := range cfg.Repos {
		wt := filepath.Join(treeDir, repo.Name)
		for _, hook := range repo.GitHooks {
			if err := installHook(ws, wt, hook); err != nil {
				return err
			}
		}
	}

	if cfg.Database != nil {
		for _, full := range m.Schemas {
			fmt.Printf("schema %s\n", full)
			if err := db.EnsureSchema(conn, full); err != nil {
				return err
			}
		}
	}

	vars := BuildVars(ws, name, m.Ports, m.Schemas)
	for _, repo := range cfg.Repos {
		wt := filepath.Join(treeDir, repo.Name)
		for _, ef := range repo.EnvFiles {
			src := filepath.Join(wt, ef.Source)
			content, err := os.ReadFile(src)
			if os.IsNotExist(err) && ef.Optional {
				continue
			}
			if err != nil {
				return fmt.Errorf("env template %s: %w", src, err)
			}
			rendered, err := envfile.Render(string(content), ef, vars)
			if err != nil {
				return fmt.Errorf("env template %s: %w", ef.Source, err)
			}
			if err := os.WriteFile(filepath.Join(wt, ef.Dest), []byte(rendered), 0o644); err != nil {
				return err
			}
		}
	}

	if cfg.Setup != "" {
		fmt.Printf("setup %s\n", cfg.Setup)
		cmd := exec.Command("bash", filepath.Join(ws.Root, cfg.Setup))
		cmd.Dir = treeDir
		cmd.Env = Environ(vars)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("setup script failed: %w", err)
		}
	}
	return nil
}

// checkSchemaCollisions refuses when this tree's derived schema names collide
// with any other tree's — derivable pairs exist (`X` and `X-test` share
// `<prefix>x_test`), and a collision means one tree's `down` would DROP the
// other tree's data.
func checkSchemaCollisions(ws *workspace.Workspace, name string, schemas map[string]string) error {
	mine := map[string]bool{}
	for _, s := range schemas {
		mine[s] = true
	}
	entries, err := os.ReadDir(ws.TreesPath())
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == name {
			continue
		}
		other, err := manifest.Read(filepath.Join(ws.TreesPath(), e.Name()))
		if err != nil {
			return err
		}
		var otherSchemas map[string]string
		if other != nil && len(other.Schemas) > 0 {
			otherSchemas = other.Schemas
		} else {
			otherSchemas = naming.SchemaSet(ws.Config.Database.Prefix, e.Name(), ws.Config.Database.Schemas)
		}
		for _, s := range otherSchemas {
			if mine[s] {
				return fmt.Errorf("schema %q for tree %q collides with tree %q — `down` on one would drop the other's data; pick a different name", s, name, e.Name())
			}
		}
	}
	return nil
}

func baseRef(repo config.Repo, overrides map[string]string, repoCount int) string {
	if ref, ok := overrides[repo.Name]; ok {
		return ref
	}
	if ref, ok := overrides[""]; ok && repoCount == 1 {
		return ref
	}
	return repo.BaseRef
}

func installHook(ws *workspace.Workspace, worktreePath, hookSource string) error {
	src := filepath.Join(ws.Root, hookSource)
	out, err := gitx.Run(worktreePath, "rev-parse", "--git-path", "hooks")
	if err != nil {
		return err
	}
	hooksDir := out
	if !filepath.IsAbs(hooksDir) {
		hooksDir = filepath.Join(worktreePath, hooksDir)
	}
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		return err
	}
	return copyFile(src, filepath.Join(hooksDir, filepath.Base(hookSource)), 0o755)
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

func localDB(ws *workspace.Workspace) *config.LocalDatabase {
	if ws.Local == nil {
		return nil
	}
	return ws.Local.Database
}
