package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/tree"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

func init() {
	lsCmd := &cobra.Command{
		Use:     "ls",
		Aliases: []string{"list"},
		Short:   "Trees with ports, branches, and dirty/unpushed state",
		RunE: func(cmd *cobra.Command, args []string) error {
			infos, err := collect()
			if err != nil {
				return err
			}
			if len(infos) == 0 {
				fmt.Println(dim("no trees yet — vtree up <name>"))
				return nil
			}
			fmt.Printf("  %s\n", bold(fmt.Sprintf("%-26s %-12s %-34s %s", "TREE", "PORTS", "BRANCH", "STATE")))
			for _, t := range infos {
				state := stateCol(t)
				if t.DisplayName != "" {
					state += " " + dim("“"+t.DisplayName+"”")
				}
				fmt.Printf("  %-26s %-12s %-34s %s\n", t.Name, portsCol(t), branchCol(t), state)
			}
			return nil
		},
	}

	var asJSON bool
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Tree state for machines (--json) or humans",
		RunE: func(cmd *cobra.Command, args []string) error {
			infos, err := collect()
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(infos)
			}
			return lsCmd.RunE(cmd, args)
		},
	}
	statusCmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")

	rootCmd.AddCommand(lsCmd, statusCmd)
}

func collect() ([]tree.TreeInfo, error) {
	cwd, _ := os.Getwd()
	ws, err := workspace.Open(cwd)
	if err != nil {
		return nil, err
	}
	return tree.Collect(ws)
}

func portsCol(t tree.TreeInfo) string {
	if len(t.Ports) == 0 {
		return "?"
	}
	vals := make([]int, 0, len(t.Ports))
	for _, p := range t.Ports {
		vals = append(vals, p)
	}
	sort.Ints(vals)
	parts := make([]string, len(vals))
	for i, p := range vals {
		parts[i] = fmt.Sprint(p)
	}
	return strings.Join(parts, "/")
}

func branchCol(t tree.TreeInfo) string {
	var parts []string
	for _, r := range t.Repos {
		if r.Branch != "" {
			parts = append(parts, r.Branch)
		}
	}
	if len(parts) == 0 {
		return "?"
	}
	// Multi-repo trees usually share one branch name; collapse when they do.
	same := true
	for _, p := range parts[1:] {
		if p != parts[0] {
			same = false
		}
	}
	if same {
		return parts[0]
	}
	return strings.Join(parts, ",")
}

func stateCol(t tree.TreeInfo) string {
	var parts []string
	if d := t.DirtyCount(); d > 0 {
		parts = append(parts, yellow(fmt.Sprintf("%d dirty", d)))
	}
	if u := t.UnpushedCount(); u > 0 {
		parts = append(parts, red(fmt.Sprintf("%d unpushed", u)))
	}
	for _, r := range t.Repos {
		if r.Err != "" {
			parts = append(parts, red("unverifiable: "+r.Repo))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, green("clean"))
	}
	if !t.HasManifest {
		parts = append(parts, dim("(no manifest — vtree adopt)"))
	}
	if t.Legacy != "" {
		parts = append(parts, dim("(legacy: "+t.Legacy+")"))
	}
	if t.PortsDiverged {
		parts = append(parts, yellow("(ports diverged from .env)"))
	}
	return strings.Join(parts, " ")
}
