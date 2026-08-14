package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/dashboard"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

func init() {
	var port int
	var noOpen bool
	uiCmd := &cobra.Command{
		Use:   "ui",
		Short: "Serve the workspace dashboard in your browser",
		Long: `Serves a local page showing every tree — state, branch, ports, and
whether its dev server is running — refreshing itself every few seconds.
Read-only, bound to 127.0.0.1. Ctrl-C stops it.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			root, err := workspace.FindRoot(cwd)
			if err != nil {
				return err
			}
			if !noOpen && runtime.GOOS == "darwin" {
				url := fmt.Sprintf("http://127.0.0.1:%d", port)
				go func() {
					time.Sleep(300 * time.Millisecond)
					exec.Command("open", url).Run()
				}()
			}
			return dashboard.Serve(root, Version, port)
		},
	}
	uiCmd.Flags().IntVar(&port, "port", 7333, "port to serve the dashboard on")
	uiCmd.Flags().BoolVar(&noOpen, "no-open", false, "don't open the browser automatically")
	rootCmd.AddCommand(uiCmd)
}
