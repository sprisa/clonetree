// Command fixture creates the sample project used by the VHS tapes:
//
//	go run ./docs/demo/fixture <dir> [packages]
//
// The project is committed to a new Git repository, with node_modules and dist
// ignored, like a real checkout.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/sprisa/clonetree/internal/demotree"
)

func main() {
	if len(os.Args) < 2 || len(os.Args) > 3 {
		fmt.Fprintln(os.Stderr, "usage: fixture <dir> [packages]")
		os.Exit(2)
	}
	root, packages := os.Args[1], 400
	if len(os.Args) == 3 {
		n, err := strconv.Atoi(os.Args[2])
		if err != nil || n < 0 {
			fmt.Fprintln(os.Stderr, "packages must be a non-negative integer")
			os.Exit(2)
		}
		packages = n
	}
	if err := demotree.Create(root, demotree.Options{Packages: packages}); err != nil {
		fmt.Fprintln(os.Stderr, "fixture:", err)
		os.Exit(1)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"add", "-A"},
		{"-c", "user.name=Demo", "-c", "user.email=demo@example.com", "commit", "-qm", "Initial commit"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "fixture: git", args[0], err)
			os.Exit(1)
		}
	}
}
