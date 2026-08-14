package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/gitx"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Clone the configured repos into repos/",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			ws, err := workspace.Open(cwd)
			if err != nil {
				return err
			}
			for _, repo := range ws.Config.Repos {
				path := ws.RepoPath(repo.Name)
				if st, err := os.Stat(path); err == nil && st.IsDir() {
					fmt.Printf("%s already cloned\n", repo.Name)
					continue
				}
				fmt.Printf("cloning %s\n", repo.Git)
				if err := gitx.RunInteractive(ws.Root, "clone", repo.Git, path); err != nil {
					return err
				}
			}
			return nil
		},
	})
}
