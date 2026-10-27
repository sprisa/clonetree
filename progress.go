package main

import (
	"context"
	"io"
	"os"
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

type progressActivity struct {
	directory string
	checking  bool
}

type progress struct {
	entries      atomic.Uint64
	bytes        atomic.Uint64
	total        atomic.Uint64
	cloneStarted atomic.Pointer[time.Time]
	activity     atomic.Pointer[progressActivity]
	bulk         atomic.Bool
	cleaning     atomic.Bool
	ctx          context.Context
	started      time.Time
	columns      int
	done         chan struct{}
	finished     chan struct{}
}

func startProgress(ctx context.Context, writer io.Writer, label string, disabled bool) *progress {
	if disabled {
		return nil
	}
	file, ok := writer.(*os.File)
	if !ok || !isTerminal(int(file.Fd())) {
		return nil
	}
	p := &progress{
		ctx:     ctx,
		started: time.Now(), columns: terminalWidth(int(file.Fd())),
		done: make(chan struct{}), finished: make(chan struct{}),
	}
	go p.render(file, label)
	return p
}

func (p *progress) setTotal(total uint64) {
	p.total.Store(total)
	started := time.Now()
	p.cloneStarted.Store(&started)
}

func (p *progress) add(size uint64) {
	p.entries.Add(1)
	p.bytes.Add(size)
}

func (p *progress) stop() {
	if p == nil || p.done == nil {
		return
	}
	close(p.done)
	<-p.finished
	p.done = nil
}

func (p *progress) render(writer *os.File, label string) {
	defer close(p.finished)
	defer writer.WriteString("\r\x1b[2K")
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
	var storage [512]byte
	buffer := storage[:0]
	for frame := 0; ; frame++ {
		buffer = append(buffer[:0], "\r\x1b[2K"...)
		line := len(buffer)
		buffer = utf8.AppendRune(buffer, frames[frame%len(frames)])
		buffer = append(buffer, ' ')
		buffer = p.appendStatus(buffer, label, time.Now())
		if p.columns > 1 {
			// Avoid wrapping and leaving old progress lines behind in narrow terminals.
			column := 0
			for i := line; i < len(buffer); {
				if column == p.columns-1 {
					buffer = buffer[:i]
					break
				}
				_, size := utf8.DecodeRune(buffer[i:])
				i += size
				column++
			}
		}
		writer.Write(buffer)
		select {
		case <-p.done:
			return
		case <-ticker.C:
		}
	}
}

func (p *progress) appendStatus(dst []byte, label string, now time.Time) []byte {
	start := len(dst)
	elapsed := now.Sub(p.started).Round(100 * time.Millisecond)
	if p.cleaning.Load() || p.ctx != nil && p.ctx.Err() != nil {
		if p.cleaning.Load() {
			dst = append(dst, "Removing partial clone"...)
		} else {
			dst = append(dst, "Stopping"...)
		}
		dst = append(dst, " · "...)
		return appendDuration(dst, elapsed)
	}
	started := p.cloneStarted.Load()
	if started == nil {
		dst = append(dst, label...)
		dst = append(dst, " · "...)
		return appendDuration(dst, elapsed)
	}
	activity := p.activity.Load()
	if activity != nil && !activity.checking {
		dst = append(dst, "Cloning directory "...)
		dst = append(dst, activity.directory...)
		dst = append(dst, " · "...)
		return appendDuration(dst, elapsed)
	}
	count, total := p.entries.Load(), p.total.Load()
	percent := 100
	if total != 0 && count < total {
		percent = int(float64(count) / float64(total) * 100)
	}
	filled := percent * 12 / 100
	if activity != nil {
		dst = append(dst, "Checking "...)
		dst = append(dst, activity.directory...)
	} else {
		dst = append(dst, "Cloning"...)
	}
	dst = append(dst, " ["...)
	for i := range 12 {
		if i < filled {
			dst = append(dst, "█"...)
		} else {
			dst = append(dst, "░"...)
		}
	}
	dst = append(dst, "] "...)
	dst = strconv.AppendInt(dst, int64(percent), 10)
	dst = append(dst, "% · "...)
	dst = strconv.AppendUint(dst, count, 10)
	dst = append(dst, '/')
	dst = strconv.AppendUint(dst, total, 10)
	dst = append(dst, " entries"...)
	if !p.bulk.Load() {
		rate := float64(0)
		if seconds := now.Sub(*started).Seconds(); seconds > 0 {
			rate = float64(count) / seconds
		}
		remaining := total - min(count, total)
		dst = append(dst, " · "...)
		dst = appendCount(dst, rate)
		dst = append(dst, "/s · ETA "...)
		switch {
		case remaining == 0:
			dst = append(dst, "0s"...)
		case rate <= 0:
			dst = append(dst, "--"...)
		default:
			duration := time.Duration(float64(remaining) / rate * float64(time.Second))
			if duration < time.Second {
				dst = append(dst, "<1s"...)
			} else {
				dst = appendDuration(dst, duration.Round(time.Second))
			}
		}
	}
	for i := range 2 {
		end := len(dst)
		dst = append(dst, " · "...)
		if i == 0 {
			dst = appendBytes(dst, p.bytes.Load())
		} else {
			dst = appendDuration(dst, elapsed)
		}
		// The status is valid UTF-8. Count rune starts without copying it to a string.
		columns := 2
		for _, c := range dst[start:] {
			if utf8.RuneStart(c) {
				columns++
			}
		}
		if columns >= p.columns {
			dst = dst[:end]
		}
	}
	return dst
}
