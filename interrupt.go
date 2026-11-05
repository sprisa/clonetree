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
			// Restore normal handling now, not after potentially slow cleanup.
			// A second interrupt can therefore terminate the process immediately.
			signal.Stop(interrupts)
			display.Lock()
			var buffer [32]byte
			writer.Write(display.appendClear(buffer[:0]))
			fmt.Fprintln(writer, "\nStopping... waiting for filesystem work and cleanup.")
			fmt.Fprintln(writer, "Press Ctrl+C again to exit without waiting for cleanup.")
			display.Unlock()
			cancel()
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
