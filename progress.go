package main

import (
	"context"
	"io"
	"sync/atomic"
	"time"

	progressbar "charm.land/bubbles/v2/progress"
	tea "charm.land/bubbletea/v2"
)

type progressActivity struct {
	directory string
	checking  bool
}

// progress is the shared state of a running clone or delete. Workers update it
// with atomics; the live view samples it on each frame.
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
	live         *live
}

const progressBarWidth = 24

func startProgress(ctx context.Context, writer io.Writer, label string, disabled bool) *progress {
	if disabled || !isTerminalWriter(writer) {
		return nil
	}
	p := &progress{ctx: ctx, started: time.Now()}
	bar := newBar(progressBarWidth)
	p.live = startLive(ctx, writer, false, func(now time.Time, _ int) liveFrame {
		return p.frame(label, now, bar)
	})
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
	if p != nil {
		p.live.stop()
	}
}

// frame renders two lines with a fixed layout: the phase on the first, and on
// the second a bar, percentage, and counters that never change position. During
// scanning and native directory clones the bar simply holds still.
func (p *progress) frame(label string, now time.Time, bar progressbar.Model) liveFrame {
	headline, details, fraction, known := p.status(label, now)
	content := spinnerFrame(p.started, now) + " " + headline + "\n  " +
		barView(bar, fraction) + " " + percentText(fraction) + dot + details
	terminal := tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
	if known {
		terminal = tea.NewProgressBar(tea.ProgressBarDefault, int(fraction*100))
	}
	return liveFrame{content: content, progress: terminal}
}

// status describes the current phase. fraction is the share of entries checked;
// known is false until the total is counted.
func (p *progress) status(label string, now time.Time) (headline, details string, fraction float64, known bool) {
	elapsed := styleMuted.Render(stopwatch(now.Sub(p.started)))
	count, total := p.entries.Load(), p.total.Load()
	started := p.cloneStarted.Load()
	if started != nil {
		known, fraction = true, 1
		if total != 0 && count < total {
			fraction = float64(count) / float64(total)
		}
	}
	if p.cleaning.Load() {
		return styleBold.Render("Removing partial clone"),
			styleMuted.Render("Press Ctrl+C again to exit without waiting") + dot + elapsed, fraction, known
	}
	if p.ctx != nil && p.ctx.Err() != nil {
		return styleBold.Render("Stopping"),
			styleMuted.Render("Press Ctrl+C again to exit without cleanup") + dot + elapsed, fraction, known
	}
	if started == nil {
		return styleBold.Render(safeText(label)), styleMuted.Render("Counting entries") + dot + elapsed, 0, false
	}
	switch activity := p.activity.Load(); {
	case activity == nil:
		headline = styleBold.Render("Cloning")
	case activity.checking:
		headline = styleBold.Render("Checking ") + styleMuted.Render(safeText(activity.directory))
	default:
		headline = styleBold.Render("Cloning ") + styleMuted.Render(safeText(activity.directory))
	}
	details = styleMuted.Render(groupedCount(min(count, max(total, count))) + "/" + groupedCount(total) + " entries")
	if !p.bulk.Load() {
		rate := float64(0)
		if seconds := now.Sub(*started).Seconds(); seconds > 0 {
			rate = float64(count) / seconds
		}
		eta := "--"
		switch remaining := total - min(count, total); {
		case remaining == 0:
			eta = "0s"
		case rate > 0:
			eta = etaString(time.Duration(float64(remaining) / rate * float64(time.Second)))
		}
		details += dot + styleMuted.Render(countString(rate)+"/s") + dot + styleMuted.Render("ETA "+eta)
	}
	details += dot + styleMuted.Render(bytesString(p.bytes.Load())) + dot + elapsed
	return headline, details, fraction, known
}
