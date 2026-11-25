package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
)

const showCursor = "\x1b[?25h"

func interruptContext(writer io.Writer) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	display := &progressDisplay{}
	ctx = context.WithValue(ctx, progressDisplayKey{}, display)
	interrupts := make(chan os.Signal, 2)
	done, finished := make(chan struct{}), make(chan struct{})
	signal.Notify(interrupts, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer close(finished)
		select {
		case <-interrupts:
			display.Lock()
			// A live view shows the stopping state itself.
			if !display.active {
				fmt.Fprintln(writer, "\nStopping... waiting for filesystem work and cleanup.")
				fmt.Fprintln(writer, "Press Ctrl+C again to exit without waiting for cleanup.")
			}
			display.Unlock()
			cancel()
		case <-done:
			return
		}
		select {
		case <-interrupts:
			// Exit without waiting for cleanup. A live view hides the cursor,
			// so restore it before leaving the terminal to the shell.
			display.Lock()
			if display.active {
				fmt.Fprint(writer, "\r\x1b[2K"+showCursor)
			}
			fmt.Fprintln(writer, "\nExited without waiting for cleanup.")
			os.Exit(130)
		case <-done:
		}
	}()
	var once sync.Once
	return ctx, func() {
		once.Do(func() {
			signal.Stop(interrupts)
			close(done)
			cancel()
			<-finished
		})
	}
}
