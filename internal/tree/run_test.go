package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quentin-JH/vtree/internal/workspace"
)

func wsWithCommands(t *testing.T) *workspace.Workspace {
	t.Helper()
	extra := `commands:
  - name: mark
    command: .vtree/scripts/mark.sh
    scope: tree
  - name: hello
    command: echo hi > hello.txt
    scope: workspace
`
	ws := makeWS(t, makeRemote(t), extra)
	scripts := filepath.Join(ws.Root, ".vtree", "scripts")
	os.MkdirAll(scripts, 0o755)
	os.WriteFile(filepath.Join(scripts, "mark.sh"),
		[]byte("#!/bin/bash\necho \"$VTREE_TREE:$VTREE_PORT_API:$VTREE_ARGS:$1\" > ran.txt\n"), 0o755)
	return ws
}

func TestRunTreeScoped(t *testing.T) {
	h := wsWithCommands(t)
	if err := Up(h, UpOptions{Name: "r1"}); err != nil {
		t.Fatal(err)
	}
	if err := Run(h, "mark", "r1", []string{"--flag"}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(h.TreesPath(), "r1", "ran.txt"))
	if err != nil {
		t.Fatal("command did not run in the tree dir:", err)
	}
	want := fmt.Sprintf("r1:%d:--flag:--flag\n", testBase)
	if string(got) != want {
		t.Errorf("marker = %q, want %q", got, want)
	}
}

func TestRunInlineShellWorkspaceScoped(t *testing.T) {
	h := wsWithCommands(t)
	if err := Run(h, "hello", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(h.Root, "hello.txt")); err != nil {
		t.Error("inline shell command did not run at workspace root")
	}
}

func TestRunUnknownCommandListsAvailable(t *testing.T) {
	h := wsWithCommands(t)
	err := Run(h, "nope", "", nil)
	if err == nil || !strings.Contains(err.Error(), "mark") {
		t.Errorf("error should list defined commands: %v", err)
	}
}

func TestRunRefusesManifestlessTree(t *testing.T) {
	h := wsWithCommands(t)
	os.MkdirAll(filepath.Join(h.TreesPath(), "old", "app"), 0o755)
	err := Run(h, "mark", "old", nil)
	if err == nil || !strings.Contains(err.Error(), "adopt") {
		t.Errorf("manifest-less tree should be refused with the adopt remedy: %v", err)
	}
}

func TestRunTreeScopedNeedsTree(t *testing.T) {
	h := wsWithCommands(t)
	if err := Run(h, "mark", "", nil); err == nil {
		t.Error("tree-scoped command without a tree should error")
	}
}
