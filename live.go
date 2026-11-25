package main

import (
	"context"
	"io"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

type progressDisplayKey struct{}

// progressDisplay coordinates the interrupt notice with any live view on the
// same terminal. While a live view is active it renders the stopping state
// itself, so the notice is not printed over it.
type progressDisplay struct {
	sync.Mutex
	active bool
}

// liveFrame is what a live view draws: the frame content and an optional
// terminal-level progress value (shown in the tab or dock by terminals that
// support OSC 9;4).
type liveFrame struct {
	content  string
	progress *tea.ProgressBar
}

type liveView func(now time.Time, width int) liveFrame

// live runs an inline Bubble Tea program on a terminal. Input is disabled: the
// program never reads stdin or sends terminal queries, and Ctrl+C is left to
// interruptContext.
type live struct {
	program  *tea.Program
	display  *progressDisplay
	finished chan struct{}
	once     sync.Once
}

type liveTick time.Time
type liveStop struct{}

type liveModel struct {
	view    liveView
	width   int
	now     time.Time
	stopped bool
}

func (m liveModel) Init() tea.Cmd { return liveTickCmd() }

func liveTickCmd() tea.Cmd {
	return tea.Tick(frameInterval, func(t time.Time) tea.Msg { return liveTick(t) })
}

func (m liveModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case liveTick:
		m.now = time.Time(msg)
		return m, liveTickCmd()
	case liveStop:
		// Clear the frame: callers print the final, static result themselves.
		m.stopped = true
		return m, tea.Quit
	}
	return m, nil
}

func (m liveModel) View() tea.View {
	if m.stopped {
		return tea.NewView("")
	}
	now := m.now
	if now.IsZero() {
		now = time.Now()
	}
	frame := m.view(now, m.width)
	v := tea.NewView(fitWidth(frame.content, m.width))
	v.ProgressBar = frame.progress
	return v
}

// startLive shows view on w if it is a terminal. It returns nil otherwise; a nil
// *live is safe to stop.
func startLive(ctx context.Context, w io.Writer, disabled bool, view liveView) *live {
	if disabled || !isTerminalWriter(w) {
		return nil
	}
	l := &live{finished: make(chan struct{})}
	l.display, _ = ctx.Value(progressDisplayKey{}).(*progressDisplay)
	if l.display == nil {
		l.display = &progressDisplay{}
	}
	l.display.Lock()
	l.display.active = true
	l.display.Unlock()
	l.program = tea.NewProgram(liveModel{view: view},
		tea.WithOutput(w), tea.WithInput(nil),
		tea.WithoutSignalHandler(), tea.WithoutCatchPanics(), tea.WithFPS(30))
	go func() {
		defer close(l.finished)
		l.program.Run()
	}()
	return l
}

// stop clears the live frame and waits until the terminal is released.
func (l *live) stop() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.program.Send(liveStop{})
		<-l.finished
		l.display.Lock()
		l.display.active = false
		l.display.Unlock()
	})
}
