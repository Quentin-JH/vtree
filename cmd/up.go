package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/tree"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

func init() {
	var bases []string
	upCmd := &cobra.Command{
		Use:   "up <name>",
		Short: "Create a tree: worktrees, branches, ports, schemas, env files, setup",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			ws, err := workspace.Open(cwd)
			if err != nil {
				return err
			}
			overrides := map[string]string{}
			for _, b := range bases {
				repo, ref, found := strings.Cut(b, "=")
				if found {
					overrides[repo] = ref
				} else if len(ws.Config.Repos) == 1 {
					overrides[""] = b
				} else {
					return fmt.Errorf("--base %q: with multiple repos, say which one: --base <repo>=<ref>", b)
				}
			}
			return tree.Up(ws, tree.UpOptions{Name: args[0], BaseOverrides: overrides})
		},
	}
	upCmd.Flags().StringArrayVar(&bases, "base", nil,
		"override a repo's base ref for this tree (<ref> for single-repo workspaces, <repo>=<ref> otherwise; repeatable)")
	rootCmd.AddCommand(upCmd)
}
