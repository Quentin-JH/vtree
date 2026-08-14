package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/tree"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

func init() {
	runCmd := &cobra.Command{
		Use:   "run <command> [tree] [args...]",
		Short: "Run a workspace-defined command",
		// Raw pass-through — everything after the command name belongs to
		// the script, flags included.
		DisableFlagParsing: true,
		Args:               cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			ws, err := workspace.Open(cwd)
			if err != nil {
				return err
			}
			name := args[0]
			rest := args[1:]
			treeName := ""
			if len(rest) > 0 {
				treeName = rest[0]
				rest = rest[1:]
			}
			return tree.Run(ws, name, treeName, rest)
		},
	}
	rootCmd.AddCommand(runCmd)
}

// registerWorkspaceCommands adds the current workspace's custom commands as
// first-class subcommands, so `vtree dev <tree>` works without the `run`
// prefix — matching how the bash vtree was used. Config validation guarantees
// they cannot shadow builtins.
func registerWorkspaceCommands() {
	cwd, err := os.Getwd()
	if err != nil {
		return
	}
	ws, err := workspace.Open(cwd)
	if err != nil {
		return // not in a workspace; builtins only
	}
	for _, cc := range ws.Config.Commands {
		cc := cc
		use := cc.Name
		if cc.Scope == "tree" {
			use += " <tree>"
		}
		rootCmd.AddCommand(&cobra.Command{
			Use:   use,
			Short: "(workspace) " + cc.Command,
			// Raw pass-through: `vtree test <tree> --filter=X` must hand
			// --filter to the script, not die on an unknown cobra flag.
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				treeName := ""
				if cc.Scope == "tree" && len(args) > 0 {
					treeName = args[0]
					args = args[1:]
				}
				return tree.Run(ws, cc.Name, treeName, args)
			},
		})
	}
}
