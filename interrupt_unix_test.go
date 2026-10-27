//go:build darwin || linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestInterruptContextNormalShutdown(t *testing.T) {
	var output bytes.Buffer
	ctx, stop := interruptContext(&output)
	stop()
	stop()
	if ctx.Err() != context.Canceled || output.Len() != 0 {
		t.Fatalf("normal shutdown: %v, output %q", ctx.Err(), output.String())
	}
}

func TestInterruptHelperProcess(t *testing.T) {
	if os.Getenv("CLONETREE_TEST_INTERRUPT") != "1" {
		return
	}
	ctx, stop := interruptContext(os.Stdout)
	defer stop()
	fmt.Println("READY")
	<-ctx.Done()
	fmt.Println("CANCELLED")
	time.Sleep(time.Hour) // Stand in for a blocking filesystem operation/cleanup.
}

func TestInterruptAcknowledgesAndSecondInterruptExits(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestInterruptHelperProcess$", "-test.v")
	cmd.Env = append(os.Environ(), "CLONETREE_TEST_INTERRUPT=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 32)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	result, exited := make(chan error, 1), make(chan struct{})
	go func() {
		result <- cmd.Wait()
		close(exited)
	}()
	defer func() {
		cmd.Process.Kill()
		<-exited
	}()
	waitFor := func(want string) {
		t.Helper()
		timer := time.NewTimer(2 * time.Second)
		defer timer.Stop()
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("helper exited before %q", want)
				}
				if strings.Contains(line, want) {
					return
				}
			case <-timer.C:
				t.Fatalf("no prompt acknowledgement of %q", want)
			}
		}
	}
	waitFor("READY")
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	waitFor("Stopping...")
	waitFor("CANCELLED")
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("second interrupt did not force an unsuccessful exit")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second interrupt was swallowed during cleanup")
	}
}
