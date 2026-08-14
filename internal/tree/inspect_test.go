package tree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quentin-JH/vtree/internal/gitx"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

// upTree creates a workspace with one live tree.
func upTree(t *testing.T, name string) *workspace.Workspace {
	t.Helper()
	ws := makeWS(t, makeRemote(t), "ignore_dirty: [\"package-lock\\\\.json\"]\n")
	if err := Up(ws, UpOptions{Name: name}); err != nil {
		t.Fatal(err)
	}
	return ws
}

func TestGuardCleanTree(t *testing.T) {
	ws := upTree(t, "clean")
	for _, st := range Inspect(ws, "clean") {
		if st.AtRisk() {
			t.Errorf("fresh tree should not be at risk: %+v", st)
		}
	}
}

func TestGuardUntrackedFileCounts(t *testing.T) {
	// Untracked files are the incident class: the 2026-08-04 loss was 529
	// uncommitted lines of a NEXT packet — new files, never staged.
	ws := upTree(t, "untracked")
	os.WriteFile(filepath.Join(ws.TreesPath(), "untracked", "app", "new-packet.php"), []byte("<?php\n"), 0o644)

	states := Inspect(ws, "untracked")
	if !states[0].AtRisk() || len(states[0].Dirty) == 0 {
		t.Fatalf("untracked file must count as dirty: %+v", states[0])
	}
	if !strings.Contains(states[0].Dirty[0], "new-packet.php") {
		t.Errorf("refusal should name the file: %v", states[0].Dirty)
	}
}

func TestGuardIgnorePatterns(t *testing.T) {
	ws := upTree(t, "noisy")
	os.WriteFile(filepath.Join(ws.TreesPath(), "noisy", "app", "package-lock.json"), []byte("{}\n"), 0o644)

	states := Inspect(ws, "noisy")
	if states[0].AtRisk() {
		t.Errorf("ignore-listed noise must not put the tree at risk: %+v", states[0])
	}
}

func TestGuardUnpushedCommit(t *testing.T) {
	ws := upTree(t, "committed")
	wt := filepath.Join(ws.TreesPath(), "committed", "app")
	os.WriteFile(filepath.Join(wt, "work.txt"), []byte("x\n"), 0o644)
	git(t, wt, "add", ".")
	git(t, wt, "commit", "-q", "-m", "exists nowhere but this laptop")

	states := Inspect(ws, "committed")
	if len(states[0].Unpushed) != 1 {
		t.Fatalf("unpushed commit must be counted: %+v", states[0])
	}
	if !strings.Contains(states[0].Unpushed[0], "exists nowhere") {
		t.Errorf("refusal should show the commit: %v", states[0].Unpushed)
	}
}

func TestGuardFailsClosedOnBrokenRepo(t *testing.T) {
	// One git-broken repo must be a refusal, not a silent "clean" that lets
	// the whole tree be rm -rf'd.
	ws := upTree(t, "broken")
	gitFile := filepath.Join(ws.TreesPath(), "broken", "app", ".git")
	os.WriteFile(gitFile, []byte("gitdir: /nonexistent\n"), 0o644)

	states := Inspect(ws, "broken")
	if states[0].Err == "" {
		t.Fatalf("uninspectable repo must carry an error: %+v", states[0])
	}
	if !states[0].AtRisk() {
		t.Fatal("uninspectable repo must read as at-risk")
	}
}

func TestTeardownRemovesCleanTree(t *testing.T) {
	ws := upTree(t, "gone")
	warnings := Teardown(ws, "gone")
	for _, w := range warnings {
		t.Logf("warning: %s", w)
	}
	if _, err := os.Stat(filepath.Join(ws.TreesPath(), "gone")); !os.IsNotExist(err) {
		t.Error("tree dir should be removed")
	}
	if _, err := gitx.Run(ws.RepoPath("app"), "show-ref", "--verify", "-q", "refs/heads/feature/gone"); err == nil {
		t.Error("branch should be deleted")
	}
}

func TestCollectFlagsManifestlessTree(t *testing.T) {
	ws := upTree(t, "real")
	os.MkdirAll(filepath.Join(ws.TreesPath(), "prehistoric", "app"), 0o755)
	git(t, ws.RepoPath("app"), "worktree", "add", "-q", "-b", "feature/prehistoric",
		filepath.Join(ws.TreesPath(), "prehistoric", "app"), "origin/staging")

	infos, err := Collect(ws)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]TreeInfo{}
	for _, i := range infos {
		byName[i.Name] = i
	}
	if !byName["real"].HasManifest {
		t.Error("vtree-created tree should have a manifest")
	}
	if byName["prehistoric"].HasManifest {
		t.Error("pre-vtree tree should be flagged manifest-less")
	}
}
