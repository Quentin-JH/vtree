package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Quentin-JH/vtree/internal/dashboard"
	"github.com/Quentin-JH/vtree/internal/workspace"
)

const launchAgentLabel = "com.vtree.ui"

func init() {
	var port int
	var noOpen, install, uninstall bool
	uiCmd := &cobra.Command{
		Use:   "ui",
		Short: "The workspace app: trees, actions, and live progress in your browser",
		Long: `Serves the workspace app on 127.0.0.1: every tree with its state, branch,
and ports, plus the actions — new tree, remove (same guard as the CLI),
run commands — with live output. Ctrl-C stops it.

--install registers it as a login service (macOS LaunchAgent), so the page
is always alive without a terminal — open http://127.0.0.1:<port> anytime,
or add it to your Dock via the browser's "install page as app".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, _ := os.Getwd()
			root, err := workspace.FindRoot(cwd)
			if err != nil {
				return err
			}
			if install {
				return installAgent(root, port)
			}
			if uninstall {
				return uninstallAgent()
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
	uiCmd.Flags().BoolVar(&install, "install", false, "run at login as a background service (macOS)")
	uiCmd.Flags().BoolVar(&uninstall, "uninstall", false, "remove the login service")
	rootCmd.AddCommand(uiCmd)
}

func agentPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist"), nil
}

func installAgent(root string, port int) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("--install is macOS-only for now")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	path, err := agentPlistPath()
	if err != nil {
		return err
	}
	// launchd gives agents a bare-bones PATH — no homebrew, no nvm, no Herd —
	// so setup and dev scripts spawned through the app would miss node, php,
	// and composer. Capture the PATH of the shell running --install (the
	// user's real one) into the service definition, the way ramp's app
	// sources the login shell before spawning its backend.
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>%s</string>
  <key>ProgramArguments</key><array>
    <string>%s</string><string>ui</string><string>--no-open</string>
    <string>--port</string><string>%d</string>
  </array>
  <key>WorkingDirectory</key><string>%s</string>
  <key>EnvironmentVariables</key><dict>
    <key>PATH</key><string>%s</string>
  </dict>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
</dict></plist>
`, launchAgentLabel, self, port, root, xmlEscape(os.Getenv("PATH")))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return err
	}
	// Reload if already registered, then start.
	exec.Command("launchctl", "unload", path).Run()
	if out, err := exec.Command("launchctl", "load", path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %s", out)
	}
	fmt.Printf("installed: the vtree app now runs at login → http://127.0.0.1:%d\n", port)
	fmt.Println("tip: in your browser, use \"Add to Dock\" / \"Install page as app\" for a Dock icon")
	return nil
}

func xmlEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return r.Replace(s)
}

func uninstallAgent() error {
	path, err := agentPlistPath()
	if err != nil {
		return err
	}
	exec.Command("launchctl", "unload", path).Run()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Println("login service removed")
	return nil
}
