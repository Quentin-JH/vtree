// Package gitx is a thin wrapper over the git CLI. vtree shells out rather
// than embedding a git library: worktrees, remote auth, and hook execution
// must behave exactly as the user's git does.
package gitx

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Run executes git with the given args in dir and returns trimmed stdout.
// Errors carry stderr, which is where git explains itself.
func Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s (in %s): %s", strings.Join(args, " "), dir, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// RunInteractive executes git with output streamed to the terminal — for
// clone and fetch, where progress matters.
func RunInteractive(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s failed", strings.Join(args, " "))
	}
	return nil
}
