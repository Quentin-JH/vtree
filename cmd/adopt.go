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
	var verify bool
	adoptCmd := &cobra.Command{
		Use:   "adopt <tree> | adopt --verify",
		Short: "Bring a pre-vtree tree under management (writes only its manifest)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			ws, err := workspace.Open(cwd)
			if err != nil {
				return err
			}
			if verify {
				missing, err := tree.VerifyAdopted(ws)
				if err != nil {
					return err
				}
				if len(missing) > 0 {
					return fmt.Errorf("%d tree(s) lack a manifest: %s", len(missing), strings.Join(missing, ", "))
				}
				fmt.Println(green("every tree has a manifest"))
				return nil
			}
			if len(args) != 1 {
				return fmt.Errorf("usage: vtree adopt <tree>, or vtree adopt --verify")
			}
			warnings, err := tree.Adopt(ws, args[0])
			for _, w := range warnings {
				fmt.Fprintln(os.Stderr, "warning:", w)
			}
			if err != nil {
				return err
			}
			fmt.Printf("adopted %s\n", args[0])
			return nil
		},
	}
	adoptCmd.Flags().BoolVar(&verify, "verify", false, "fail if any tree lacks a manifest")
	rootCmd.AddCommand(adoptCmd)
}
