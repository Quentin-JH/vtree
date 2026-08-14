package envfile

import (
	"strings"
	"testing"

	"github.com/Quentin-JH/vtree/internal/config"
)

func set(pairs ...string) config.EnvSet {
	s := config.EnvSet{Values: map[string]string{}}
	for i := 0; i < len(pairs); i += 2 {
		s.Keys = append(s.Keys, pairs[i])
		s.Values[pairs[i]] = pairs[i+1]
	}
	return s
}

// Mirrors the velera-crm api/.env.example shape: sqlite live, MySQL commented.
const example = `APP_NAME=Velera
APP_URL=http://localhost:8000

DB_CONNECTION=sqlite
# DB_HOST=127.0.0.1
# DB_PORT=3306
# DB_DATABASE=velera
# DB_USERNAME=root
# DB_PASSWORD=
MICROSOFT_REDIRECT_URI=http://localhost:8000/api/user/auth/microsoft/callback
`

func TestRenderVeleraShape(t *testing.T) {
	ef := config.EnvFile{
		Delete: []string{"DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME", "DB_PASSWORD"},
		Set: set(
			"APP_URL", "http://localhost:${VTREE_PORT_API}",
			"MICROSOFT_REDIRECT_URI", "http://localhost:${VTREE_PORT_API}/api/user/auth/microsoft/callback",
			"DB_CONNECTION", "mysql",
			"DB_HOST", "${VTREE_DB_HOST}",
			"DB_PORT", "${VTREE_DB_PORT}",
			"DB_DATABASE", "${VTREE_DB_MAIN}",
			"DB_USERNAME", "${VTREE_DB_USER}",
			"DB_PASSWORD", "${VTREE_DB_PASS}",
		),
	}
	vars := map[string]string{
		"VTREE_PORT_API": "4100",
		"VTREE_DB_HOST":  "127.0.0.1",
		"VTREE_DB_PORT":  "3306",
		"VTREE_DB_MAIN":  "velera_demo",
		"VTREE_DB_USER":  "root",
		"VTREE_DB_PASS":  "",
	}
	out, err := Render(example, ef, vars)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"APP_URL=http://localhost:4100\n",
		"DB_CONNECTION=mysql\n",
		"DB_HOST=127.0.0.1\n",
		"DB_DATABASE=velera_demo\n",
		"DB_PASSWORD=\n",
		"MICROSOFT_REDIRECT_URI=http://localhost:4100/api/user/auth/microsoft/callback\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n---\n%s", want, out)
		}
	}
	// The commented-out MySQL keys must be GONE, not lurking.
	if strings.Contains(out, "# DB_") {
		t.Errorf("commented DB_ keys survived:\n%s", out)
	}
	// sqlite must not appear anywhere.
	if strings.Contains(out, "sqlite") {
		t.Errorf("sqlite survived:\n%s", out)
	}
	// Exactly one live line per DB key (a duplicate would win in dotenv loaders).
	for _, key := range []string{"DB_CONNECTION", "DB_HOST", "DB_DATABASE"} {
		if n := strings.Count(out, "\n"+key+"="); n+boolToInt(strings.HasPrefix(out, key+"=")) != 1 {
			t.Errorf("%s appears %d times", key, n)
		}
	}
}

func TestRenderDeterministic(t *testing.T) {
	ef := config.EnvFile{Set: set("B", "2", "A", "1", "C", "3")}
	first, _ := Render("X=0\n", ef, nil)
	for i := 0; i < 20; i++ {
		again, _ := Render("X=0\n", ef, nil)
		if again != first {
			t.Fatalf("nondeterministic output:\n%s\nvs\n%s", first, again)
		}
	}
	if !strings.HasSuffix(first, "B=2\nA=1\nC=3\n") {
		t.Errorf("append order should follow author order:\n%s", first)
	}
}

func TestExpandUnknownVarFails(t *testing.T) {
	ef := config.EnvFile{Set: set("DB_PASSWORD", "${VTREE_DB_PASSS}")}
	_, err := Render("", ef, map[string]string{"VTREE_DB_PASS": "secret"})
	if err == nil {
		t.Fatal("typo'd variable must be an error, not an empty string")
	}
	if !strings.Contains(err.Error(), "VTREE_DB_PASSS") {
		t.Errorf("error should name the unknown variable: %v", err)
	}
}

func TestReplaceInPlaceKeepsPosition(t *testing.T) {
	ef := config.EnvFile{Set: set("APP_URL", "y")}
	out, err := Render("APP_URL=x\nOTHER=1\n", ef, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out != "APP_URL=y\nOTHER=1\n" {
		t.Errorf("replace should keep line position:\n%s", out)
	}
}

func TestDuplicateLiveKeyCollapsed(t *testing.T) {
	ef := config.EnvFile{Set: set("K", "new")}
	out, err := Render("K=a\nK=b\n", ef, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "K=new") != 1 || strings.Contains(out, "K=b") {
		t.Errorf("second live occurrence should not survive:\n%s", out)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
