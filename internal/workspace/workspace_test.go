package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const minimalYAML = `
name: t
repos:
  - name: a
    git: https://example.com/a.git
    base_ref: origin/main
`

func makeWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ConfigDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ConfigFile), []byte(minimalYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestFindRootFromNestedDir(t *testing.T) {
	root := makeWorkspace(t)
	nested := filepath.Join(root, "trees", "x", "a", "deep")
	os.MkdirAll(nested, 0o755)

	got, err := FindRoot(nested)
	if err != nil {
		t.Fatal(err)
	}
	// t.TempDir may sit behind a symlink (/tmp on macOS); compare resolved paths.
	want, _ := filepath.EvalSymlinks(root)
	gotR, _ := filepath.EvalSymlinks(got)
	if gotR != want {
		t.Errorf("FindRoot = %q, want %q", gotR, want)
	}
}

func TestFindRootNotFound(t *testing.T) {
	_, err := FindRoot(t.TempDir())
	if err == nil {
		t.Fatal("expected an error outside any workspace")
	}
	if !strings.Contains(err.Error(), "vtree init") {
		t.Errorf("error should point at `vtree init`: %v", err)
	}
}

func TestOpenWithoutLocal(t *testing.T) {
	ws, err := Open(makeWorkspace(t))
	if err != nil {
		t.Fatal(err)
	}
	if ws.Local != nil {
		t.Error("Local should be nil when local.yaml is absent")
	}
	if ws.Config.Name != "t" {
		t.Errorf("config not loaded: %+v", ws.Config)
	}
}

func TestOpenWithLocal(t *testing.T) {
	root := makeWorkspace(t)
	os.WriteFile(filepath.Join(root, ConfigDir, LocalFile),
		[]byte(`database: { host: 127.0.0.1, port: 3306, user: root }`), 0o644)
	ws, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if ws.Local == nil || ws.Local.Database.Host != "127.0.0.1" {
		t.Errorf("local not loaded: %+v", ws.Local)
	}
}
