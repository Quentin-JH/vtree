package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/tree"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "pr <tree> [-- gh-args...]",
		Short: "Push and open PRs against the configured base, with a wrong-base drift guard",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			ws, err := workspace.Open(cwd)
			if err != nil {
				return err
			}
			return tree.PR(ws, args[0], args[1:])
		},
	})
}
