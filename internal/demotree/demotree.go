// Package demotree creates a sample web project for recordings and UI tests.
//
// The tree resembles a JavaScript monorepo with installed dependencies and build
// output, so clonetree new and clonetree info run long enough to show live
// progress. Contents are deterministic for a given package count.
package demotree

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// Options size the sample project.
type Options struct {
	// Packages is the number of dependency directories in node_modules; each
	// holds roughly 150–600 files.
	Packages int
	// SmallFiles caps file contents at 64 bytes, keeping the entry count while
	// fitting small test filesystems.
	SmallFiles bool
}

// Create writes the sample project under root.
func Create(root string, options Options) error {
	random := rand.New(rand.NewPCG(7, 7))
	between := func(low, high int) int {
		n := low + random.IntN(high-low+1)
		if options.SmallFiles {
			return min(n, 64)
		}
		return n
	}
	files := map[string]int{"package.json": between(600, 600), "README.md": between(2400, 2400)}
	for _, app := range []string{"web", "api", "worker"} {
		for i := range 60 {
			files[fmt.Sprintf("apps/%s/src/module-%d.ts", app, i)] = between(800, 9000)
		}
	}
	for p := range options.Packages {
		base := fmt.Sprintf("node_modules/pkg-%04d", p)
		files[base+"/package.json"] = between(700, 700)
		for d := range between(4, 10) {
			for n := range between(20, 60) {
				files[fmt.Sprintf("%s/lib/part-%d/file-%d.js", base, d, n)] = between(200, 24000)
			}
		}
	}
	for n := range 40 {
		files[fmt.Sprintf("dist/chunk-%d.js", n)] = between(200_000, 4_000_000)
	}
	files[".gitignore"] = 0
	content := make([]byte, 4_000_000)
	for i := range content {
		content[i] = 'x'
	}
	for name, size := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		data := content[:size]
		if name == ".gitignore" {
			data = []byte("node_modules/\ndist/\n")
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
