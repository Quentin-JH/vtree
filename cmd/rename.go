package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/manifest"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "rename <tree> [display name...]",
		Short: "Set a tree's display name (its directory, branch, and schemas keep the tree name)",
		Long: `Gives a tree a human label, shown alongside the tree name in ls and the
app. With no display name, the label is cleared. Nothing on disk or in git
is renamed — the tree name stays the identifier.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			ws, err := workspace.Open(cwd)
			if err != nil {
				return err
			}
			name := args[0]
			display := strings.TrimSpace(strings.Join(args[1:], " "))
			treeDir := filepath.Join(ws.TreesPath(), name)
			if _, err := os.Stat(treeDir); err != nil {
				return fmt.Errorf("no tree named %q — try: vtree ls", name)
			}
			if err := manifest.SetDisplayName(treeDir, display); err != nil {
				return err
			}
			if display == "" {
				fmt.Printf("cleared display name of %s\n", name)
			} else {
				fmt.Printf("%s → %q\n", name, display)
			}
			return nil
		},
	})
}
