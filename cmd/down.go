package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/tree"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

func init() {
	var force bool
	downCmd := &cobra.Command{
		Use:   "down <name>",
		Short: "Remove a tree; refuses if work would be lost",
		Long: `Removes a tree's worktrees, branches, ports, and MySQL schemas.

Removing a worktree deletes its branch, so commits that exist on no remote
die with it. down therefore refuses when any repo in the tree has uncommitted
changes (untracked files included) or unpushed commits — and also when a
repo's state cannot be verified at all, because an uninspectable repo must
never read as clean. --force overrides all of it and has to be typed.

There is deliberately no bulk removal. Remove trees by name.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cwd, _ := os.Getwd()
			ws, err := workspace.Open(cwd)
			if err != nil {
				return err
			}
			if _, err := os.Stat(filepath.Join(ws.TreesPath(), name)); err != nil {
				return fmt.Errorf("no tree named %q — try: vtree ls", name)
			}

			if !force {
				states := tree.Inspect(ws, name)
				refused := false
				for _, st := range states {
					if !st.AtRisk() {
						continue
					}
					refused = true
					fmt.Fprintf(os.Stderr, "%s\n", red(fmt.Sprintf("Refusing to remove %q — %s:", name, st.Repo)))
					if st.Err != "" {
						fmt.Fprintf(os.Stderr, "  cannot verify state: %s\n  fix the repo, or re-run with --force\n", st.Err)
					}
					if len(st.Dirty) > 0 {
						fmt.Fprintf(os.Stderr, "  %d uncommitted change(s):\n", len(st.Dirty))
						for _, l := range st.Dirty {
							fmt.Fprintf(os.Stderr, "    %s\n", l)
						}
					}
					if len(st.Unpushed) > 0 {
						fmt.Fprintf(os.Stderr, "  %d commit(s) on no remote — deleted with the branch:\n", len(st.Unpushed))
						for _, l := range st.Unpushed {
							fmt.Fprintf(os.Stderr, "    %s\n", l)
						}
					}
				}
				if refused {
					fmt.Fprintln(os.Stderr, "\nPush the work, or re-run with --force to discard it.")
					return fmt.Errorf("refused")
				}
			}

			for _, w := range tree.Teardown(ws, name) {
				fmt.Fprintln(os.Stderr, "warning:", w)
			}
			fmt.Printf("removed %s\n", name)
			return nil
		},
	}
	downCmd.Flags().BoolVar(&force, "force", false, "discard uncommitted and unpushed work")
	rootCmd.AddCommand(downCmd)
}
