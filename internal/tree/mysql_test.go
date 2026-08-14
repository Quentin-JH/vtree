package tree

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Quentin-JH/vtree/internal/config"
	"github.com/Quentin-JH/vtree/internal/db"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

// testDB returns a reachable local MySQL or skips. Locally that is DBngin on
// 3306; in CI it is the workflow's mysql service container.
func testDB(t *testing.T) *config.LocalDatabase {
	t.Helper()
	ldb := &config.LocalDatabase{Host: "127.0.0.1", Port: 3306, User: "root", Password: ""}
	conn, err := db.Connect(ldb)
	if err != nil {
		t.Skipf("no local MySQL reachable: %v", err)
	}
	conn.Close()
	return ldb
}

func schemaExists(t *testing.T, ldb *config.LocalDatabase, name string) bool {
	t.Helper()
	conn, err := db.Connect(ldb)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var found string
	err = conn.QueryRow("SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME = ?", name).Scan(&found)
	return err == nil
}

func TestUpAndDownSchemaLifecycle(t *testing.T) {
	ldb := testDB(t)
	ws := makeWS(t, makeRemote(t), "database:\n  prefix: vtreetest_\n  schemas: [main, test]\n")
	os.WriteFile(filepath.Join(ws.Root, ".vtree", "local.yaml"),
		[]byte("database: { host: 127.0.0.1, port: 3306, user: root, password: \"\" }\n"), 0o644)
	ws, err := workspace.Open(ws.Root)
	if err != nil {
		t.Fatal(err)
	}

	// Cleanup regardless of outcome — orphan schemas from test runs are the
	// exact scar tissue the live server already carries.
	cleanup := func() {
		conn, err := db.Connect(ldb)
		if err != nil {
			return
		}
		defer conn.Close()
		db.DropSchema(conn, "vtreetest_lifecycle")
		db.DropSchema(conn, "vtreetest_lifecycle_test")
	}
	t.Cleanup(cleanup)

	if err := Up(ws, UpOptions{Name: "lifecycle"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"vtreetest_lifecycle", "vtreetest_lifecycle_test"} {
		if !schemaExists(t, ldb, s) {
			t.Errorf("up did not create schema %s", s)
		}
	}

	// The X / X-test collision must refuse before touching anything.
	if err := Up(ws, UpOptions{Name: "lifecycle-test"}); err == nil {
		t.Error("schema collision (lifecycle-test vs lifecycle) should refuse")
		Teardown(ws, "lifecycle-test")
	}

	for _, w := range Teardown(ws, "lifecycle") {
		t.Logf("teardown warning: %s", w)
	}
	for _, s := range []string{"vtreetest_lifecycle", "vtreetest_lifecycle_test"} {
		if schemaExists(t, ldb, s) {
			t.Errorf("down did not drop schema %s", s)
		}
	}
}
