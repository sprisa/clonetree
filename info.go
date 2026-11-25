package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/urfave/cli/v3"
)

type snapshot struct {
	id   string
	path string
}

// listSnapshots returns the snapshots in directory, sorted by ID. It reads only
// the directory itself, never the snapshots' contents.
func listSnapshots(directory string) ([]snapshot, error) {
	entries, err := os.ReadDir(directory)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	var snapshots []snapshot
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), treeSuffix) {
			continue
		}
		snapshots = append(snapshots, snapshot{
			id:   strings.TrimSuffix(entry.Name(), treeSuffix),
			path: filepath.Join(directory, entry.Name()),
		})
	}
	return snapshots, nil
}

// projectSnapshots is the snapshot folder inside a project. --dir may name
// either the folder holding the <id>.clonetree folders or the project itself,
// as running from the project without --dir uses its ./clonetree.
const projectSnapshots = "clonetree"

// findSnapshots resolves --dir for commands that read existing snapshots. It
// returns the folder as the user wrote it (for display), its absolute path,
// and the snapshots in it. When --dir holds no snapshots itself but its
// clonetree/ folder does, that folder is used.
func findSnapshots(cmd *cli.Command) (dir, directory string, snapshots []snapshot, err error) {
	dir = cmd.String("dir")
	directory, err = snapshotDirectory(cmd)
	if err != nil {
		return "", "", nil, err
	}
	snapshots, err = listSnapshots(directory)
	if err != nil || len(snapshots) > 0 {
		return dir, directory, snapshots, err
	}
	nested := filepath.Join(directory, projectSnapshots)
	if found, err := listSnapshots(nested); err == nil && len(found) > 0 {
		return filepath.Join(dir, projectSnapshots), nested, found, nil
	}
	return dir, directory, nil, nil
}

func printNoSnapshots(w io.Writer, dir string) {
	printf(w, "%s\n", styleMuted.Render("No snapshots in "+safeText(dir)))
	printCommand(w, "clonetree new <id>")
}

func showList(ctx context.Context, cmd *cli.Command) error {
	if err := arguments(cmd, 0); err != nil {
		return err
	}
	dir, _, snapshots, err := findSnapshots(cmd)
	if err != nil {
		return err
	}
	w := cmd.Root().Writer
	if len(snapshots) == 0 {
		printNoSnapshots(w, dir)
		return nil
	}
	columns := []column{{title: "ID", width: idWidth(snapshots)}, {title: "PATH"}}
	printf(w, "%s\n", tableHeader(columns))
	for _, s := range snapshots {
		path := filepath.Join(dir, s.id+treeSuffix)
		printf(w, "%s\n", tableRow(columns, styleBold.Render(safeText(s.id)), styleMuted.Render(safeText(path))))
	}
	if isTerminalWriter(w) {
		printf(w, "\n%s\n", styleMuted.Render(snapshotCount(len(snapshots))+" · Run clonetree info for storage usage"))
	}
	return nil
}

func snapshotCount(n int) string {
	if n == 1 {
		return "1 snapshot"
	}
	return fmt.Sprintf("%d snapshots", n)
}

func idWidth(snapshots []snapshot) int {
	width := len("ID")
	for _, s := range snapshots {
		width = max(width, lipgloss.Width(safeText(s.id)))
	}
	return width
}

// Sizes are at most "1023.9 GiB".
const sizeWidth = 10

// Percentages are at most "100%" or "99.9%".
const percentWidth = 6

// infoColumns lays out the table. With disk usage, each row reads as "uses
// DISK USED, SAVED compared with a FULL COPY". Without it, only the logical
// SIZE is shown. The live view adds an unlabeled progress column after these,
// so values stay in place as each row finishes.
func infoColumns(snapshots []snapshot, wantDisk bool) []column {
	id := column{title: "ID", width: idWidth(snapshots)}
	if !wantDisk {
		return []column{id, {title: "SIZE", width: sizeWidth, right: true}}
	}
	return []column{
		id,
		{title: "DISK USED", width: sizeWidth, right: true},
		{title: "SAVED", width: percentWidth, right: true},
		{title: "FULL COPY", width: sizeWidth, right: true},
	}
}

// savedPercent is the share of a full copy that the clone does not use,
// rounded down so a clone using any space never claims 100%.
func savedPercent(used, full uint64) string {
	switch {
	case full == 0:
		return "—"
	case used == 0:
		return "100%"
	case used >= full:
		return "0%"
	}
	saved := math.Floor(float64(full-used)/float64(full)*1000) / 10
	return strconv.FormatFloat(saved, 'f', 1, 64) + "%"
}

func showInfo(ctx context.Context, cmd *cli.Command) error {
	if err := arguments(cmd, 0); err != nil {
		return err
	}
	dir, directory, snapshots, err := findSnapshots(cmd)
	if err != nil {
		return err
	}
	w := cmd.Root().Writer
	if len(snapshots) == 0 {
		printNoSnapshots(w, dir)
		return nil
	}
	wantDisk := !cmd.Bool("size-only")
	roots := make([]string, len(snapshots))
	for i, s := range snapshots {
		roots[i] = s.path
	}
	started := time.Now()
	m, err := startSizes(ctx, roots, wantDisk, sizeWorkers())
	if err != nil {
		return err
	}
	columns := infoColumns(snapshots, wantDisk)

	// Live per-snapshot progress, estimated from the entry counts of the last
	// interactive run. Estimates only drive the display; sizes are always live.
	errWriter := cmd.Root().ErrWriter
	interactive := !cmd.Bool("no-progress") && isTerminalWriter(errWriter)
	var estimates map[string]uint64
	if interactive {
		estimates = loadEstimates()
	}
	view := startLive(ctx, errWriter, !interactive, func(now time.Time, _ int) liveFrame {
		return infoFrame(ctx, snapshots, columns, m, estimates, started, now)
	})
	err = m.wait()
	view.stop()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		job, _ := m.failed()
		for i, j := range m.jobs {
			if j == job {
				return fmt.Errorf("size of %s: %w", snapshots[i].id+treeSuffix, err)
			}
		}
		return err
	}
	if interactive {
		measured := make(map[string]uint64, len(m.jobs))
		for _, job := range m.jobs {
			measured[job.root] = job.entries.Load()
		}
		saveEstimates(directory, measured)
	}

	printf(w, "%s\n", tableHeader(columns))
	for i, job := range m.jobs {
		printf(w, "%s\n", tableRow(columns, measuredCells(snapshots[i], job.sizes(), wantDisk)...))
	}
	if isTerminalWriter(w) {
		printf(w, "\n%s\n", styleMuted.Render(infoSummary(m, wantDisk, time.Since(started))))
	}
	return nil
}

// infoSummary totals the table: how much the clones use and how much they save
// compared with full copies.
func infoSummary(m *sizeMeasurement, wantDisk bool, elapsed time.Duration) string {
	summary := snapshotCount(len(m.jobs))
	var used, full uint64
	known := wantDisk
	for _, job := range m.jobs {
		size := job.sizes()
		full += size.Logical
		if size.Private == nil {
			known = false
		} else {
			used += *size.Private
		}
	}
	if known {
		summary += " · " + bytesString(used) + " disk used"
		if full > used {
			summary += " · " + bytesString(full-used) + " saved (" + savedPercent(used, full) + ")"
		}
	} else {
		summary += " · " + bytesString(full) + " total"
	}
	return summary + " · measured in " + stopwatch(elapsed)
}

func measuredCells(s snapshot, size sizes, wantDisk bool) []string {
	name := styleBold.Render(safeText(s.id))
	if !wantDisk {
		return []string{name, bytesString(size.Logical)}
	}
	used, saved := styleMuted.Render("n/a"), styleMuted.Render("n/a")
	if size.Private != nil {
		used = bytesString(*size.Private)
		saved = styleAccent.Render(savedPercent(*size.Private, size.Logical))
	}
	return []string{name, used, saved, bytesString(size.Logical)}
}

const infoBarWidth = 16

// infoFrame renders the snapshot table. Every row keeps the same columns while it
// runs and after it finishes: sizes fill in place, and progress has a column of
// its own. Rows resolve as soon as their snapshot is measured.
func infoFrame(ctx context.Context, snapshots []snapshot, columns []column, m *sizeMeasurement,
	estimates map[string]uint64, started, now time.Time) liveFrame {
	bar := newBar(infoBarWidth)
	wantDisk := len(columns) == 4
	live := append(columns[:len(columns):len(columns)], column{})
	pending := styleMuted.Render("—")
	var b strings.Builder
	b.WriteString(tableHeader(columns))
	var scanned, expected uint64
	allEstimated, finished := true, 0
	for i, job := range m.jobs {
		b.WriteByte('\n')
		entries := job.entries.Load()
		scanned += entries
		begun, end := job.started.Load(), job.finished.Load()
		if end != nil {
			finished++
			expected += entries
			took := time.Duration(0)
			if begun != nil {
				took = end.Sub(*begun)
			}
			cells := append(measuredCells(snapshots[i], job.sizes(), wantDisk), markSuccess+" "+styleMuted.Render(stopwatch(took)))
			b.WriteString(tableRow(live, cells...))
			continue
		}
		estimate := estimates[job.root]
		expected += max(estimate, entries)
		allEstimated = allEstimated && estimate != 0
		cells := []string{styleBold.Render(safeText(snapshots[i].id)), pending}
		if wantDisk {
			cells = append(cells, pending, pending)
		}
		if begun == nil {
			b.WriteString(tableRow(live, append(cells, markPending+" "+styleMuted.Render("Waiting"))...))
			continue
		}
		// Running totals fill in muted; SAVED waits for the final numbers.
		logical := styleMuted.Render(bytesString(job.logical.Load()))
		if !wantDisk {
			cells[1] = logical
		} else {
			cells[3] = logical
			if job.privateKnown.Load() {
				cells[1] = styleMuted.Render(bytesString(job.private.Load()))
			}
		}
		elapsed := styleMuted.Render(stopwatch(now.Sub(*begun)))
		progress := spinnerFrame(started, now) + " " + styleMuted.Render(countString(float64(entries))+" entries") + dot + elapsed
		if estimate != 0 {
			// The tree may have grown since the estimate; never claim completion early.
			fraction := min(float64(entries)/float64(estimate), 0.99)
			progress = barView(bar, fraction) + " " + percentText(fraction) + dot + elapsed
		}
		b.WriteString(tableRow(live, append(cells, progress)...))
	}
	b.WriteString("\n\n")
	if ctx.Err() != nil {
		b.WriteString(spinnerFrame(started, now) + " " + styleBold.Render("Stopping") + dot +
			styleMuted.Render("Press Ctrl+C again to exit immediately"))
	} else {
		b.WriteString(styleMuted.Render(fmt.Sprintf("Measuring %d of %d", len(m.jobs)-finished, len(m.jobs))) + dot +
			styleMuted.Render(countString(float64(scanned))+" entries") + dot + styleMuted.Render(stopwatch(now.Sub(started))))
	}
	terminal := tea.NewProgressBar(tea.ProgressBarIndeterminate, 0)
	if allEstimated && expected > 0 {
		terminal = tea.NewProgressBar(tea.ProgressBarDefault, int(min(float64(scanned)/float64(expected), 0.99)*100))
	}
	return liveFrame{content: b.String(), progress: terminal}
}

// Entry-count estimates from earlier runs, keyed by snapshot path. The cache is
// a display hint only: unreadable or missing data just means no percentages.
func estimatesPath() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "clonetree", "entries.json"), nil
}

func loadEstimates() map[string]uint64 {
	estimates := map[string]uint64{}
	path, err := estimatesPath()
	if err != nil {
		return estimates
	}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &estimates)
	}
	return estimates
}

// saveEstimates records this run's counts and forgets snapshots that no longer
// exist in directory.
func saveEstimates(directory string, measured map[string]uint64) {
	path, err := estimatesPath()
	if err != nil {
		return
	}
	estimates := loadEstimates()
	for root := range estimates {
		if filepath.Dir(root) == directory {
			delete(estimates, root)
		}
	}
	for root, entries := range measured {
		estimates[root] = entries
	}
	data, err := json.Marshal(estimates)
	if err != nil || os.MkdirAll(filepath.Dir(path), 0700) != nil {
		return
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".entries-*.json")
	if err != nil {
		return
	}
	_, writeErr := temp.Write(data)
	if closeErr := temp.Close(); writeErr != nil || closeErr != nil || os.Rename(temp.Name(), path) != nil {
		os.Remove(temp.Name())
	}
}
