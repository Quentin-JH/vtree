package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/workspace"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Check prerequisites and workspace health",
		RunE:  runDoctor,
	})
}

type report struct {
	failed bool
}

func (r *report) ok(what, detail string) {
	fmt.Printf("  %s   %-10s %s\n", green("ok"), what, detail)
}

func (r *report) miss(what, detail string) {
	r.failed = true
	fmt.Printf("  %s %-10s %s\n", red("MISS"), what, detail)
}

func (r *report) warn(what, detail string) {
	fmt.Printf("  %s %-10s %s\n", yellow("warn"), what, detail)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	r := &report{}

	for _, tool := range []string{"git", "gh"} {
		if path, err := exec.LookPath(tool); err == nil {
			r.ok(tool, firstLine(tool, "--version")+" ("+path+")")
		} else {
			r.miss(tool, "not on PATH")
		}
	}

	cwd, _ := os.Getwd()
	ws, err := workspace.Open(cwd)
	if err != nil {
		r.miss("workspace", err.Error())
		finish(r)
		return errFailed(r)
	}
	fmt.Printf("\nworkspace %s (%s)\n", ws.Config.Name, ws.Root)
	r.ok("config", ws.ConfigPath())

	if ws.Local == nil {
		if ws.Config.Database != nil {
			r.miss("local.yaml", ws.LocalPath()+" not found — required before any database operation; vtree has no built-in connection defaults")
		} else {
			r.warn("local.yaml", ws.LocalPath()+" not found")
		}
	} else {
		r.ok("local.yaml", ws.LocalPath())
		if db := ws.Local.Database; db != nil {
			if v, err := mysqlVersion(db.User, db.Password, db.Host, db.Port); err == nil {
				r.ok("mysql", fmt.Sprintf("%s at %s:%d", v, db.Host, db.Port))
			} else {
				hint := ws.Local.Hints["mysql_unreachable"]
				if hint != "" {
					hint = " — " + hint
				}
				r.miss("mysql", fmt.Sprintf("unreachable at %s:%d as %s%s", db.Host, db.Port, db.User, hint))
			}
		} else if ws.Config.Database != nil {
			r.miss("mysql", "workspace defines per-tree schemas but local.yaml has no database block")
		}
	}

	for _, repo := range ws.Config.Repos {
		path := ws.RepoPath(repo.Name)
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			r.ok("repo", path)
		} else {
			r.miss("repo", path+" — clone it with `vtree install`")
		}
	}

	finish(r)
	return errFailed(r)
}

func finish(r *report) {
	if r.failed {
		fmt.Println("\nsome checks failed")
	}
}

func errFailed(r *report) error {
	if r.failed {
		return fmt.Errorf("doctor found problems")
	}
	return nil
}

// mysqlVersion connects with a short timeout and returns SELECT VERSION().
// The Go driver is used rather than a mysql client binary so teammates'
// machines need nothing beyond the vtree binary itself.
func mysqlVersion(user, pass, host string, port int) (string, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/?timeout=3s", user, pass, host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return "", err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var v string
	if err := db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&v); err != nil {
		return "", err
	}
	return v, nil
}

func firstLine(name string, args ...string) string {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(out), "\n")
	if len(line) > 60 {
		line = line[:60]
	}
	return line
}
