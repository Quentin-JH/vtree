package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validYAML mirrors the velera-crm workspace config — the first real consumer.
const validYAML = `
name: velera-crm
repos:
  - name: velera-crm
    git: https://github.com/zbloom4/velera-crm.git
    base_ref: origin/staging
    branch_prefix: feature/
    git_hooks:
      - .vtree/templates/prepare-commit-msg
    env_files:
      - source: api/.env.example
        dest: api/.env
        delete: [DB_HOST, DB_PORT, DB_DATABASE, DB_USERNAME, DB_PASSWORD]
        set:
          APP_URL: "http://localhost:${VTREE_PORT_API}"
          DB_CONNECTION: mysql
      - source: web/.env.example
        dest: web/.env
        optional: true
        set:
          VITE_API_URL: "http://localhost:${VTREE_PORT_API}/api"
ports:
  base: 4000
  names: [api, web]
database:
  prefix: velera_
  schemas: [main, test]
setup: .vtree/scripts/setup.sh
templates:
  - source: .vtree/templates/CLAUDE.md
    dest: CLAUDE.md
commands:
  - name: dev
    command: .vtree/scripts/dev.sh
    scope: tree
  - name: test
    command: .vtree/scripts/test.sh
    scope: tree
ignore_dirty: ["package-lock\\.json", "graphify-out"]
pr:
  base: staging
  guard_against: [main]
`

func write(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vtree.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadValid(t *testing.T) {
	cfg, err := Load(write(t, validYAML))
	if err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if cfg.Name != "velera-crm" {
		t.Errorf("name = %q", cfg.Name)
	}
	if len(cfg.Repos) != 1 || cfg.Repos[0].BaseRef != "origin/staging" {
		t.Errorf("repos parsed wrong: %+v", cfg.Repos)
	}
	if cfg.Ports.Base != 4000 || len(cfg.Ports.Names) != 2 {
		t.Errorf("ports parsed wrong: %+v", cfg.Ports)
	}
	if cfg.PR == nil || len(cfg.PR.GuardAgainst) != 1 || cfg.PR.GuardAgainst[0] != "main" {
		t.Errorf("pr parsed wrong: %+v", cfg.PR)
	}
}

func TestBranchPrefixDefault(t *testing.T) {
	yaml := `
name: x
repos:
  - name: a
    git: https://example.com/a.git
    base_ref: origin/main
`
	cfg, err := Load(write(t, yaml))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repos[0].BranchPrefix != "feature/" {
		t.Errorf("branch_prefix default = %q, want feature/", cfg.Repos[0].BranchPrefix)
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(string) string
		wantErr string
	}{
		{"missing name", func(s string) string {
			return strings.Replace(s, "name: velera-crm\n", "", 1)
		}, `"name"`},
		{"missing base_ref", func(s string) string {
			return strings.Replace(s, "    base_ref: origin/staging\n", "", 1)
		}, "base_ref"},
		{"unknown field", func(s string) string {
			return s + "\nbranch_prfix: oops\n"
		}, "branch_prfix"},
		{"bad scope", func(s string) string {
			return strings.Replace(s, "scope: tree", "scope: feature", 1)
		}, "scope"},
		{"builtin shadowed", func(s string) string {
			return strings.Replace(s, "name: dev", "name: status", 1)
		}, "built-in"},
		{"duplicate command", func(s string) string {
			return strings.Replace(s, "name: dev", "name: test", 1)
		}, "duplicate command"},
		{"guard_against equals base", func(s string) string {
			return strings.Replace(s, "guard_against: [main]", "guard_against: [staging]", 1)
		}, "vacuous"},
		{"bad ignore_dirty regex", func(s string) string {
			return strings.Replace(s, `"graphify-out"`, `"["`, 1)
		}, "regular expression"},
		{"bad schema name", func(s string) string {
			return strings.Replace(s, "schemas: [main, test]", "schemas: [main, Test-DB]", 1)
		}, "schema name"},
		{"ports without names", func(s string) string {
			return strings.Replace(s, "  names: [api, web]\n", "", 1)
		}, "names"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(write(t, tc.mutate(validYAML)))
			if err == nil {
				t.Fatalf("expected an error containing %q, got none", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadLocal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local.yaml")
	os.WriteFile(path, []byte(`
database: { host: 127.0.0.1, port: 3306, user: root, password: "" }
hints:
  mysql_unreachable: "Start it in DBngin, not brew."
`), 0o644)
	loc, err := LoadLocal(path)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Database.Port != 3306 || loc.Hints["mysql_unreachable"] == "" {
		t.Errorf("local parsed wrong: %+v", loc)
	}
}

func TestLoadLocalIncomplete(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local.yaml")
	os.WriteFile(path, []byte(`database: { host: 127.0.0.1 }`), 0o644)
	if _, err := LoadLocal(path); err == nil {
		t.Fatal("incomplete database block accepted")
	}
}
