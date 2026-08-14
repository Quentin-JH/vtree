// Package cmd wires the vtree CLI. Subcommands live one per file.
package cmd

import (
	"github.com/spf13/cobra"
)

// Version is stamped at build time via -ldflags "-X github.com/Quentin-JH/vtree/cmd.Version=v0.1.0".
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "vtree",
	Short: "Multi-repo git-worktree workspaces with per-tree MySQL schemas",
	Long: `vtree manages workspaces of git worktrees: one "tree" per feature, spanning
every configured repo, each with its own ports and MySQL schemas.

There is deliberately no bulk removal. Trees are removed by name, one at a
time, and 'down' refuses when uncommitted or unpushed work would be lost.`,
	SilenceUsage:  true,
	SilenceErrors: false,
}

func Execute() error {
	registerWorkspaceCommands()
	return rootCmd.Execute()
}
