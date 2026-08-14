package scaffold

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quentin-JH/vtree/internal/config"
)

func fullAnswers() Answers {
	return Answers{
		Name: "acme",
		Repos: []RepoAnswer{
			{Name: "app", Git: "https://github.com/acme/app.git", BaseRef: "origin/staging"},
			{Name: "docs", Git: "https://github.com/acme/docs.git", BaseRef: "origin/main"},
		},
		PortBase:  5000,
		PortNames: []string{"api", "web"},
		DBPrefix:  "acme_",
		DBSchemas: []string{"main", "test"},
		PRBase:    "staging",
		PRGuard:   []string{"main"},
	}
}

func TestWriteWorkspaceGeneratesLoadableConfig(t *testing.T) {
	dir := t.TempDir()
	created, err := WriteWorkspace(dir, fullAnswers())
	if err != nil {
		t.Fatal(err)
	}
	if len(created) < 2 {
		t.Errorf("created = %v, expected config + gitignore", created)
	}

	cfg, err := config.Load(filepath.Join(dir, ".vtree", "vtree.yaml"))
	if err != nil {
		t.Fatalf("generated config does not load: %v", err)
	}
	if len(cfg.Repos) != 2 || cfg.Repos[1].BaseRef != "origin/main" {
		t.Errorf("repos = %+v", cfg.Repos)
	}
	if cfg.Ports.Base != 5000 || cfg.Database.Prefix != "acme_" {
		t.Errorf("ports/db wrong: %+v %+v", cfg.Ports, cfg.Database)
	}
	if cfg.PR == nil || cfg.PR.GuardAgainst[0] != "main" {
		t.Errorf("pr wrong: %+v", cfg.PR)
	}

	gi, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	for _, want := range []string{"repos/", "trees/", ".vtree/local.yaml"} {
		if !strings.Contains(string(gi), want) {
			t.Errorf("gitignore missing %q", want)
		}
	}
}

func TestWriteWorkspaceMinimal(t *testing.T) {
	dir := t.TempDir()
	a := Answers{Name: "tiny", Repos: []RepoAnswer{{Name: "a", Git: "https://x/a.git", BaseRef: "origin/main"}}}
	if _, err := WriteWorkspace(dir, a); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(dir, ".vtree", "vtree.yaml"))
	if err != nil {
		t.Fatalf("minimal config does not load: %v", err)
	}
	if cfg.Ports != nil || cfg.Database != nil || cfg.PR != nil {
		t.Errorf("minimal scaffold grew optional blocks: %+v", cfg)
	}
}

func TestWriteWorkspaceRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteWorkspace(dir, fullAnswers()); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteWorkspace(dir, fullAnswers()); err == nil {
		t.Fatal("second scaffold should refuse")
	}
}

func TestWriteLocalRoundTrips(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".vtree"), 0o755)
	path, err := WriteLocal(dir, "127.0.0.1", 3307, "root", "s3cr3t\"quoted")
	if err != nil {
		t.Fatal(err)
	}
	loc, err := config.LoadLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Database.Port != 3307 || loc.Database.Password != "s3cr3t\"quoted" {
		t.Errorf("local = %+v", loc.Database)
	}
	if _, err := WriteLocal(dir, "x", 1, "y", ""); err == nil {
		t.Fatal("second local.yaml should refuse")
	}
}

func TestEnsureGitignoreAppendsWithoutDuplicating(t *testing.T) {
	dir := t.TempDir()
	gi := filepath.Join(dir, ".gitignore")
	os.WriteFile(gi, []byte("node_modules/\nrepos/\n"), 0o644)
	if _, err := ensureGitignore(gi); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(gi)
	if strings.Count(string(out), "repos/") != 1 {
		t.Errorf("repos/ duplicated:\n%s", out)
	}
	if !strings.Contains(string(out), "node_modules/") {
		t.Error("existing entries must survive")
	}
}
