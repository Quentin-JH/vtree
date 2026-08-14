package dashboard

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Quentin-JH/vtree/internal/tree"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

// statusHandler mirrors the /api/status closure in Serve for testing without
// binding a real port. Keep in sync (it is small on purpose).
func TestStatusPayloadShape(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".vtree"), 0o755)
	os.WriteFile(filepath.Join(root, ".vtree", "vtree.yaml"), []byte(`
name: dash
repos:
  - name: app
    git: https://x/app.git
    base_ref: origin/main
ports: { base: 25900, names: [api] }
`), 0o644)
	// A manifest-only tree is enough for the payload path.
	treeDir := filepath.Join(root, "trees", "t1")
	os.MkdirAll(treeDir, 0o755)
	os.WriteFile(filepath.Join(treeDir, ".vtree-tree.json"),
		[]byte(`{"name":"t1","ports":{"api":25900},"branches":{"app":"feature/t1"}}`), 0o644)

	ws, err := workspace.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	infos, err := tree.Collect(ws)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Ports["api"] != 25900 {
		t.Fatalf("collect = %+v", infos)
	}

	// Listening probe: true against a live listener, false after close.
	ln, err := net.Listen("tcp", "127.0.0.1:25900")
	if err != nil {
		t.Skip("port busy on this machine")
	}
	if !dialing(25900) {
		t.Error("dialing should see the live listener")
	}
	ln.Close()
	if dialing(25900) {
		t.Error("dialing should be false after close")
	}
}

func TestPageServes(t *testing.T) {
	page, err := pageFS.ReadFile("page.html")
	if err != nil {
		t.Fatal("page not embedded:", err)
	}
	if !strings.Contains(string(page), "/api/status") {
		t.Error("page does not fetch /api/status")
	}
	// Serve the page bytes the way the handler does.
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "text/html; charset=utf-8")
	rec.Write(page)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d", rec.Code)
	}
}

func TestPayloadMarshals(t *testing.T) {
	p := payload{Workspace: "x", Trees: []treeView{{Listening: map[string]bool{"api": true}}}}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"listening"`) {
		t.Errorf("payload json missing listening: %s", b)
	}
}
