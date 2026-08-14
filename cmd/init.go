package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/scaffold"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "init",
		Short: "Set up a workspace — full scaffold, or just your machine's local.yaml",
		Long: `In an empty directory: interactively scaffold a new workspace
(.vtree/vtree.yaml, .gitignore, scripts dir).

In an existing workspace missing .vtree/local.yaml — a fresh clone on a new
machine — it prompts only for your machine's database settings and writes
local.yaml. That is the whole teammate first-run flow: clone, vtree init,
vtree install, vtree up.`,
		RunE: runInit,
	})
}

func runInit(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	if root, err := workspace.FindRoot(cwd); err == nil {
		return initLocal(root)
	}
	return initWorkspace(cwd)
}

// initLocal is the teammate path: the workspace exists, this machine's
// local.yaml does not.
func initLocal(root string) error {
	ws, err := workspace.Open(root)
	if err != nil {
		return err
	}
	if ws.Local != nil {
		fmt.Printf("workspace %s is already set up here (%s exists) — nothing to do\n", ws.Config.Name, ws.LocalPath())
		return nil
	}
	if ws.Config.Database == nil {
		fmt.Printf("workspace %s needs no local.yaml (no database block) — you're set. Next: vtree install\n", ws.Config.Name)
		return nil
	}

	fmt.Printf("workspace %s found at %s — setting up this machine\n", ws.Config.Name, root)
	host, portStr, user, pass := "127.0.0.1", "3306", "root", ""
	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("MySQL host").Value(&host),
		huh.NewInput().Title("MySQL port").Value(&portStr).Validate(validatePort),
		huh.NewInput().Title("MySQL user").Value(&user),
		huh.NewInput().Title("MySQL password (empty is fine for local)").Value(&pass).EchoMode(huh.EchoModePassword),
	))
	if err := form.Run(); err != nil {
		return err
	}
	port, _ := strconv.Atoi(strings.TrimSpace(portStr))
	path, err := scaffold.WriteLocal(root, strings.TrimSpace(host), port, strings.TrimSpace(user), pass)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s\nNext: vtree install, then vtree doctor, then vtree up <name>\n", path)
	return nil
}

// initWorkspace scaffolds a brand-new workspace in dir.
func initWorkspace(dir string) error {
	a := scaffold.Answers{Name: filepath.Base(dir)}

	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Workspace name").Value(&a.Name).Validate(required("name")),
	)).Run(); err != nil {
		return err
	}

	for {
		r := scaffold.RepoAnswer{BaseRef: "origin/main"}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Repository git URL").Value(&r.Git).Validate(required("git URL")),
			huh.NewInput().Title("Base ref to cut trees from").
				Description("The branch PRs target — e.g. origin/staging when main is deploy-only").
				Value(&r.BaseRef).Validate(required("base ref")),
		)).Run(); err != nil {
			return err
		}
		r.Name = repoNameFromURL(r.Git)
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Repository directory name").Value(&r.Name).Validate(required("name")),
		)).Run(); err != nil {
			return err
		}
		a.Repos = append(a.Repos, r)

		more := false
		if err := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().Title("Add another repository?").Value(&more),
		)).Run(); err != nil {
			return err
		}
		if !more {
			break
		}
	}

	usePorts, useDB := true, false
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Allocate ports per tree?").Value(&usePorts),
		huh.NewConfirm().Title("Provision MySQL schemas per tree?").
			Description("Each tree gets <prefix><tree> and <prefix><tree>_test on your local MySQL").
			Value(&useDB),
	)).Run(); err != nil {
		return err
	}

	if usePorts {
		base, names := "4000", "api, web"
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Base port").Value(&base).Validate(validatePort),
			huh.NewInput().Title("Port names (comma-separated)").
				Description("Each becomes ${VTREE_PORT_<NAME>} in env templates and scripts").
				Value(&names).Validate(required("port names")),
		)).Run(); err != nil {
			return err
		}
		a.PortBase, _ = strconv.Atoi(strings.TrimSpace(base))
		a.PortNames = splitList(names)
	}

	if useDB {
		prefix := strings.ToLower(strings.ReplaceAll(a.Name, "-", "_")) + "_"
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Schema prefix").Value(&prefix).Validate(required("prefix")),
		)).Run(); err != nil {
			return err
		}
		a.DBPrefix = strings.TrimSpace(prefix)
		a.DBSchemas = []string{"main", "test"}
	}

	prBase := ""
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("PR base branch (empty to skip vtree pr)").
			Description("With a guard branch, vtree pr refuses branches cut from the wrong base").
			Value(&prBase),
	)).Run(); err != nil {
		return err
	}
	if s := strings.TrimSpace(prBase); s != "" {
		a.PRBase = s
		guard := "main"
		if s == "main" {
			guard = ""
		}
		if guard != "" {
			a.PRGuard = []string{guard}
		}
	}

	created, err := scaffold.WriteWorkspace(dir, a)
	if err != nil {
		return err
	}
	for _, p := range created {
		fmt.Println("wrote", p)
	}

	if useDB {
		if err := initLocal(dir); err != nil {
			return err
		}
	} else {
		fmt.Println("Next: vtree install, then vtree up <name>")
	}
	return nil
}

func required(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%s is required", what)
		}
		return nil
	}
}

func validatePort(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1024 || n > 65535 {
		return fmt.Errorf("must be a port between 1024 and 65535")
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func repoNameFromURL(url string) string {
	base := filepath.Base(strings.TrimSuffix(strings.TrimSpace(url), "/"))
	return strings.TrimSuffix(base, ".git")
}
