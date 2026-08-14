package tree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quentin-JH/vtree/internal/gitx"
	"github.com/Quentin-JH/vtree/internal/manifest"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

// Base port for tests — high and unusual to avoid colliding with anything
// running on the machine.
const testBase = 24700

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)
	out, err := gitx.Run(dir, full...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

// makeRemote builds the repo that plays "origin": a main branch plus a
// staging branch, shipping a velera-shaped .env.example (sqlite live, MySQL
// commented out).
func makeRemote(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "remote")
	os.MkdirAll(filepath.Join(dir, "api"), 0o755)
	git(t, filepath.Dir(dir), "init", "-q", "-b", "main", dir)
	os.WriteFile(filepath.Join(dir, "api", ".env.example"), []byte(
		"APP_URL=http://localhost:8000\nDB_CONNECTION=sqlite\n# DB_HOST=127.0.0.1\n# DB_DATABASE=app\n"), 0o644)
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-q", "-m", "seed")
	git(t, dir, "branch", "staging")
	return dir
}

func makeWS(t *testing.T, remote string, extraYAML string) *workspace.Workspace {
	t.Helper()
	root := t.TempDir()
	vdir := filepath.Join(root, ".vtree")
	os.MkdirAll(filepath.Join(vdir, "templates"), 0o755)
	os.WriteFile(filepath.Join(vdir, "templates", "CLAUDE.md"), []byte("# conventions\n"), 0o644)
	os.WriteFile(filepath.Join(vdir, "templates", "prepare-commit-msg"), []byte("#!/bin/sh\nexit 0\n"), 0o755)

	yaml := fmt.Sprintf(`
name: t
repos:
  - name: app
    git: %s
    base_ref: origin/staging
    git_hooks:
      - .vtree/templates/prepare-commit-msg
    env_files:
      - source: api/.env.example
        dest: api/.env
        delete: [DB_HOST, DB_DATABASE]
        set:
          APP_URL: "http://localhost:${VTREE_PORT_API}"
          WEB_URL: "http://localhost:${VTREE_PORT_WEB}"
ports:
  base: %d
  names: [api, web]
templates:
  - source: .vtree/templates/CLAUDE.md
    dest: CLAUDE.md
%s`, remote, testBase, extraYAML)
	os.WriteFile(filepath.Join(vdir, "vtree.yaml"), []byte(yaml), 0o644)

	os.MkdirAll(filepath.Join(root, "repos"), 0o755)
	git(t, root, "clone", "-q", remote, filepath.Join(root, "repos", "app"))

	ws, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestUpEndToEnd(t *testing.T) {
	ws := makeWS(t, makeRemote(t), "")
	if err := Up(ws, UpOptions{Name: "feat-x"}); err != nil {
		t.Fatal(err)
	}
	treeDir := filepath.Join(ws.TreesPath(), "feat-x")

	// Worktree on the right branch, cut from staging.
	branch := git(t, filepath.Join(treeDir, "app"), "rev-parse", "--abbrev-ref", "HEAD")
	if branch != "feature/feat-x" {
		t.Errorf("branch = %q", branch)
	}

	// Template copied to the TREE root, not into a repo.
	if _, err := os.Stat(filepath.Join(treeDir, "CLAUDE.md")); err != nil {
		t.Error("CLAUDE.md missing at tree root")
	}

	// Env rendered: ports in, sqlite-era keys handled, commented dupes gone.
	env, _ := os.ReadFile(filepath.Join(treeDir, "app", "api", ".env"))
	s := string(env)
	if !strings.Contains(s, fmt.Sprintf("APP_URL=http://localhost:%d\n", testBase)) {
		t.Errorf("APP_URL not rendered:\n%s", s)
	}
	if !strings.Contains(s, fmt.Sprintf("WEB_URL=http://localhost:%d\n", testBase+1)) {
		t.Errorf("WEB_URL not appended:\n%s", s)
	}
	if strings.Contains(s, "# DB_") {
		t.Errorf("commented DB keys survived:\n%s", s)
	}

	// Manifest claims the ports.
	m, err := manifest.Read(treeDir)
	if err != nil || m == nil {
		t.Fatalf("manifest: %v", err)
	}
	if m.Ports["api"] != testBase || m.Ports["web"] != testBase+1 {
		t.Errorf("manifest ports = %v", m.Ports)
	}

	// Hook landed in the SHARED hooks dir of the source clone.
	hook := filepath.Join(ws.RepoPath("app"), ".git", "hooks", "prepare-commit-msg")
	if _, err := os.Stat(hook); err != nil {
		t.Errorf("hook not installed in shared hooks dir: %v", err)
	}

	// Same name again refuses.
	if err := Up(ws, UpOptions{Name: "feat-x"}); err == nil {
		t.Error("second up with the same name should refuse")
	}

	// A second tree gets the next port block.
	if err := Up(ws, UpOptions{Name: "feat-y"}); err != nil {
		t.Fatal(err)
	}
	m2, _ := manifest.Read(filepath.Join(ws.TreesPath(), "feat-y"))
	if m2.Ports["api"] != testBase+2 {
		t.Errorf("second tree ports = %v, want api=%d", m2.Ports, testBase+2)
	}
}

func TestUpRollsBackOnFailedSetup(t *testing.T) {
	ws := makeWS(t, makeRemote(t), "setup: .vtree/scripts/setup.sh\n")
	os.MkdirAll(filepath.Join(ws.Root, ".vtree", "scripts"), 0o755)
	os.WriteFile(filepath.Join(ws.Root, ".vtree", "scripts", "setup.sh"),
		[]byte("#!/bin/bash\nexit 7\n"), 0o755)

	err := Up(ws, UpOptions{Name: "doomed"})
	if err == nil {
		t.Fatal("up should fail when setup fails")
	}
	if _, statErr := os.Stat(filepath.Join(ws.TreesPath(), "doomed")); !os.IsNotExist(statErr) {
		t.Error("failed tree dir should be rolled back")
	}
	// The branch must not survive rollback — a stale branch blocks retrying
	// the same name forever.
	if _, err := gitx.Run(ws.RepoPath("app"), "show-ref", "--verify", "-q", "refs/heads/feature/doomed"); err == nil {
		t.Error("branch survived rollback")
	}
}

func TestSetupReceivesEnv(t *testing.T) {
	ws := makeWS(t, makeRemote(t), "setup: .vtree/scripts/setup.sh\n")
	os.MkdirAll(filepath.Join(ws.Root, ".vtree", "scripts"), 0o755)
	os.WriteFile(filepath.Join(ws.Root, ".vtree", "scripts", "setup.sh"),
		[]byte("#!/bin/bash\necho \"$VTREE_TREE:$VTREE_PORT_API\" > \"$VTREE_TREE_DIR/marker\"\n"), 0o755)

	if err := Up(ws, UpOptions{Name: "envy"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(ws.TreesPath(), "envy", "marker"))
	if err != nil {
		t.Fatal("setup did not run in the tree dir with env:", err)
	}
	if want := fmt.Sprintf("envy:%d\n", testBase); string(got) != want {
		t.Errorf("marker = %q, want %q", got, want)
	}
}

func TestAllocationSeesManifestlessTrees(t *testing.T) {
	ws := makeWS(t, makeRemote(t), "")
	// A pre-vtree tree: no manifest, but a rendered .env claiming the first
	// port block. Allocation must skip it.
	legacy := filepath.Join(ws.TreesPath(), "legacy", "app", "api")
	os.MkdirAll(legacy, 0o755)
	os.WriteFile(filepath.Join(legacy, ".env"),
		[]byte(fmt.Sprintf("APP_URL=http://localhost:%d\n", testBase)), 0o644)

	if err := Up(ws, UpOptions{Name: "fresh"}); err != nil {
		t.Fatal(err)
	}
	m, _ := manifest.Read(filepath.Join(ws.TreesPath(), "fresh"))
	if m.Ports["api"] == testBase {
		t.Errorf("allocation reused a legacy tree's port %d", testBase)
	}
}

func TestBaseOverride(t *testing.T) {
	remote := makeRemote(t)
	// Advance main past staging so the two refs differ.
	os.WriteFile(filepath.Join(remote, "on-main-only"), []byte("x\n"), 0o644)
	git(t, remote, "add", ".")
	git(t, remote, "commit", "-q", "-m", "main moves ahead")

	ws := makeWS(t, remote, "")
	if err := Up(ws, UpOptions{Name: "from-main", BaseOverrides: map[string]string{"": "origin/main"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ws.TreesPath(), "from-main", "app", "on-main-only")); err != nil {
		t.Error("tree was not cut from origin/main")
	}
}

func TestMain(m *testing.M) {
	// Integration tests shell out to git; skip everything cleanly if absent.
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Println("git not found; skipping tree tests")
		os.Exit(0)
	}
	os.Exit(m.Run())
}
