package main

import (
	"os"
	"strings"
	"testing"
)

func TestVersionOutput(t *testing.T) {
	data, err := os.ReadFile("version.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.TrimSpace(string(data))
	if want == "" {
		t.Fatal("version.txt is empty")
	}
	for _, flag := range []string{"--version", "-v"} {
		output, err := runCLI(t, flag)
		if err != nil || strings.TrimSpace(output) != "clonetree version "+want {
			t.Fatalf("%s: %q, %v; want version %s", flag, output, err, want)
		}
	}
	output, err := runCLI(t, "--help")
	if err != nil || !strings.Contains(output, "VERSION:") || !strings.Contains(output, want) {
		t.Fatalf("help omitted version %s: %q, %v", want, output, err)
	}
}
