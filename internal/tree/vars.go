package tree

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Quentin-JH/vtree/internal/workspace"
)

// BuildVars assembles the VTREE_* variable set for a tree — used both to
// expand ${...} references in env templates and as process environment for
// setup scripts and custom commands.
func BuildVars(ws *workspace.Workspace, name string, ports map[string]int, schemas map[string]string) map[string]string {
	vars := map[string]string{
		"VTREE_TREE":     name,
		"VTREE_TREE_DIR": filepath.Join(ws.TreesPath(), name),
		"VTREE_ROOT":     ws.Root,
	}
	for pname, p := range ports {
		vars["VTREE_PORT_"+strings.ToUpper(pname)] = fmt.Sprint(p)
	}
	if db := localDB(ws); db != nil {
		vars["VTREE_DB_HOST"] = db.Host
		vars["VTREE_DB_PORT"] = fmt.Sprint(db.Port)
		vars["VTREE_DB_USER"] = db.User
		vars["VTREE_DB_PASS"] = db.Password
	}
	for sname, full := range schemas {
		vars["VTREE_DB_"+strings.ToUpper(sname)] = full
	}
	return vars
}

// Environ returns os.Environ() extended with vars.
func Environ(vars map[string]string) []string {
	env := os.Environ()
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	return env
}
