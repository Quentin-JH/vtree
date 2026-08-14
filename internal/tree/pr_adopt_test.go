package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quentin-JH/vtree/internal/gitx"
	"github.com/Quentin-JH/vtree/internal/manifest"
)

const prYAML = `pr:
  base: staging
  guard_against: [main]
`

func TestStrayGuardFiresOnWrongBase(t *testing.T) {
	remote := makeRemote(t)
	// main moves ahead of staging — the drift that makes a main-cut branch
	// carry foreign commits into a staging PR.
	os.WriteFile(filepath.Join(remote, "main-only.txt"), []byte("x\n"), 0o644)
	git(t, remote, "add", ".")
	git(t, remote, "commit", "-q", "-m", "promotion merge on main")

	ws := makeWS(t, remote, prYAML)
	if err := Up(ws, UpOptions{Name: "wrong", BaseOverrides: map[string]string{"": "origin/main"}}); err != nil {
		t.Fatal(err)
	}

	strays, err := StrayCheck(ws, "wrong")
	if err != nil {
		t.Fatal(err)
	}
	if len(strays) != 1 {
		t.Fatalf("guard must fire for a main-cut branch: %+v", strays)
	}
	if strays[0].Guard != "main" || strays[0].Commits < 1 {
		t.Errorf("stray = %+v", strays[0])
	}
}

func TestStrayGuardPassesOnRightBase(t *testing.T) {
	remote := makeRemote(t)
	os.WriteFile(filepath.Join(remote, "main-only.txt"), []byte("x\n"), 0o644)
	git(t, remote, "add", ".")
	git(t, remote, "commit", "-q", "-m", "promotion merge on main")

	ws := makeWS(t, remote, prYAML)
	if err := Up(ws, UpOptions{Name: "right"}); err != nil { // default base: origin/staging
		t.Fatal(err)
	}
	strays, err := StrayCheck(ws, "right")
	if err != nil {
		t.Fatal(err)
	}
	if len(strays) != 0 {
		t.Errorf("guard must not fire for a staging-cut branch: %+v", strays)
	}
}

func TestAdoptWritesManifestAndFindsPorts(t *testing.T) {
	ws := makeWS(t, makeRemote(t), "")
	// A pre-vtree tree: real worktree, hand-rendered env, no manifest.
	treeDir := filepath.Join(ws.TreesPath(), "old")
	os.MkdirAll(treeDir, 0o755)
	git(t, ws.RepoPath("app"), "worktree", "add", "-q", "-b", "feature/old",
		filepath.Join(treeDir, "app"), "origin/staging")
	os.MkdirAll(filepath.Join(treeDir, "app", "api"), 0o755)
	os.WriteFile(filepath.Join(treeDir, "app", "api", ".env"),
		[]byte(fmt.Sprintf("APP_URL=http://localhost:%d\nWEB_URL=http://localhost:%d\n", testBase+8, testBase+9)), 0o644)

	warnings, err := Adopt(ws, "old")
	if err != nil {
		t.Fatal(err, warnings)
	}
	m, _ := manifest.Read(treeDir)
	if m == nil {
		t.Fatal("no manifest written")
	}
	if m.Ports["api"] != testBase+8 || m.Ports["web"] != testBase+9 {
		t.Errorf("adopted ports = %v", m.Ports)
	}
	if m.Branches["app"] != "feature/old" {
		t.Errorf("adopted branch = %v", m.Branches)
	}
	// Template refreshed at tree root.
	if _, err := os.Stat(filepath.Join(treeDir, "CLAUDE.md")); err != nil {
		t.Error("tree-root template not refreshed")
	}
	// The worktree itself must be untouched (adopt never writes in-repo).
	dirty, _ := gitStatus(ws, "old")
	if dirty != "" {
		t.Errorf("adopt dirtied the worktree:\n%s", dirty)
	}

	// Second adopt refuses; verify goes green.
	if _, err := Adopt(ws, "old"); err == nil {
		t.Error("re-adopt should refuse")
	}
	missing, _ := VerifyAdopted(ws)
	if len(missing) != 0 {
		t.Errorf("verify should be clean, missing = %v", missing)
	}
}

func TestAdoptMarksUnmanagedDBLegacy(t *testing.T) {
	ws := makeWS(t, makeRemote(t), `database:
  prefix: vtadopt_
  schemas: [main, test]
`)
	treeDir := filepath.Join(ws.TreesPath(), "sqlite-era")
	os.MkdirAll(treeDir, 0o755)
	git(t, ws.RepoPath("app"), "worktree", "add", "-q", "-b", "feature/sqlite-era",
		filepath.Join(treeDir, "app"), "origin/staging")
	os.MkdirAll(filepath.Join(treeDir, "app", "api"), 0o755)
	// An env that never mentions the derived schema: the sqlite era.
	os.WriteFile(filepath.Join(treeDir, "app", "api", ".env"),
		[]byte("APP_URL=http://localhost:31111\nDB_CONNECTION=sqlite\n"), 0o644)

	warnings, err := Adopt(ws, "sqlite-era")
	if err != nil {
		t.Fatal(err, warnings)
	}
	m, _ := manifest.Read(treeDir)
	if m.Legacy != LegacyUnmanagedDB {
		t.Errorf("legacy = %q, want %q", m.Legacy, LegacyUnmanagedDB)
	}
	// Legacy tree: db-dependent env injection is withheld by run (covered in
	// run.go); here we assert no schemas were recorded to drop later.
	if len(m.Schemas) != 0 {
		t.Errorf("legacy tree must not record schemas: %v", m.Schemas)
	}
}

func gitStatus(ws interface{ TreesPath() string }, name string) (string, error) {
	out, err := gitx.Run(filepath.Join(ws.TreesPath(), name, "app"), "status", "--porcelain")
	return strings.TrimSpace(out), err
}
