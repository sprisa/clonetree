package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestCompletionScripts(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		output, err := runCLI(t, "completion", shell)
		if err != nil || !strings.Contains(output, "clonetree") || !strings.Contains(output, "--generate-shell-completion") {
			t.Fatalf("%s script: %v\n%s", shell, err, output)
		}
	}
	// The bash script ends by replacing the helper that needs bash-completion.
	output, _ := runCLI(t, "completion", "bash")
	helper := "__clonetree_init_completion() {"
	if strings.Count(output, helper) != 2 || !strings.HasSuffix(strings.TrimSpace(output), "}") ||
		strings.LastIndex(output, helper) < strings.LastIndex(output, "complete -o bashdefault") {
		t.Fatalf("bash script lacks the fallback after the stock helper:\n%s", output)
	}
	help, err := runCLI(t, "completion", "--help")
	if err != nil || !strings.Contains(help, "Homebrew installs completions") || !strings.Contains(help, `eval "$(clonetree completion bash)"`) {
		t.Fatalf("completion help: %q, %v", help, err)
	}
	if strings.Contains(help, "pwsh") {
		t.Fatalf("completion help lists the untested PowerShell script:\n%s", help)
	}
	if overview, _ := runCLI(t); !strings.Contains(overview, "completion <shell>") {
		t.Fatalf("overview does not list completion:\n%s", overview)
	}
}

func TestDeleteCompletesSnapshotIDs(t *testing.T) {
	project := fixture(t)
	for _, id := range []string{"alpha", "beta"} {
		write(t, filepath.Join(project, "clonetree", id+".clonetree", "data"), "x")
	}
	t.Chdir(fixture(t))
	for _, dir := range []string{filepath.Join(project, "clonetree"), project} {
		output, err := runCLI(t, "--dir", dir, "delete", "--generate-shell-completion")
		if err != nil || output != "alpha\nbeta\n" {
			t.Fatalf("--dir %s: %q, %v", dir, output, err)
		}
	}
	output, err := runCLI(t, "--dir", project, "delete", "--no", "--generate-shell-completion")
	if err != nil || !strings.Contains(output, "--no-progress") || strings.Contains(output, "alpha") {
		t.Fatalf("flag completion: %q, %v", output, err)
	}
}
