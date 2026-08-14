// Package ui renders long-running work: a spinner on a terminal, plain
// begin/end lines when piped (agents and CI read those). Subprocess output is
// buffered and replayed only on failure — success stays quiet, failure shows
// everything.
package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/briandowns/spinner"
)

var interactive = func() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	st, err := os.Stdout.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}()

// WithProgress runs fn under a message. On a terminal it spins; piped, it
// prints the message once and the outcome after.
func WithProgress(msg string, fn func() error) error {
	if !interactive {
		fmt.Println(msg + " ...")
		err := fn()
		if err == nil {
			fmt.Println(msg + " done")
		}
		return err
	}
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond)
	s.Suffix = " " + msg
	s.Start()
	err := fn()
	s.Stop()
	if err == nil {
		fmt.Println("✓ " + msg)
	} else {
		fmt.Println("✗ " + msg)
	}
	return err
}

// RunQuiet executes cmd with stdout/stderr buffered. On failure the buffer is
// replayed so nothing is lost; on success it stays out of the way.
func RunQuiet(cmd *exec.Cmd) error {
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	if err != nil {
		os.Stderr.Write(buf.Bytes())
	}
	return err
}
