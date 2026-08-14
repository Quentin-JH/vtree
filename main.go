package main

import (
	"os"

	"github.com/Quentin-JH/vtree/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
