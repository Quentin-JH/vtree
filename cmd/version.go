package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the vtree version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("vtree", Version)
		},
	})
}
