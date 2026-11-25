package main

import (
	"fmt"
	"strconv"
	"time"
)

func bytesString(n uint64) string {
	if n < 1024 {
		return strconv.FormatUint(n, 10) + " B"
	}
	value := float64(n)
	for _, unit := range [...]string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"} {
		value /= 1024
		if value < 1024 || unit == "EiB" {
			return strconv.FormatFloat(value, 'f', 1, 64) + " " + unit
		}
	}
	panic("unreachable")
}

// countString abbreviates large counts: 950, 12.3k, 4.1M.
func countString(n float64) string {
	for _, suffix := range [...]string{"", "k", "M", "G", "T"} {
		if n < 1000 || suffix == "T" {
			precision := 1
			if suffix == "" {
				precision = 0
			}
			return strconv.FormatFloat(n, 'f', precision, 64) + suffix
		}
		n /= 1000
	}
	panic("unreachable")
}

// groupedCount formats n with thousands separators: 1,234,567.
func groupedCount(n uint64) string {
	digits := strconv.FormatUint(n, 10)
	out := make([]byte, 0, len(digits)+len(digits)/3)
	for i := range len(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, digits[i])
	}
	return string(out)
}

// stopwatch formats elapsed time with a fixed tenths digit, so a running timer
// reads 19.9s, 20.0s, 20.1s rather than dropping the decimal on whole seconds.
// Minutes and seconds are zero-padded for the same reason: 1m02.3s.
func stopwatch(d time.Duration) string {
	tenths := max(d, 0) / (100 * time.Millisecond)
	seconds, tenth := tenths/10, tenths%10
	switch {
	case seconds < 60:
		return fmt.Sprintf("%d.%ds", seconds, tenth)
	case seconds < 3600:
		return fmt.Sprintf("%dm%02d.%ds", seconds/60, seconds%60, tenth)
	default:
		return fmt.Sprintf("%dh%02dm%02d.%ds", seconds/3600, seconds/60%60, seconds%60, tenth)
	}
}

// etaString formats a remaining-time estimate in whole seconds.
func etaString(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	seconds := int64(d.Round(time.Second) / time.Second)
	switch {
	case seconds < 60:
		return fmt.Sprintf("%ds", seconds)
	case seconds < 3600:
		return fmt.Sprintf("%dm%02ds", seconds/60, seconds%60)
	default:
		return fmt.Sprintf("%dh%02dm", seconds/3600, seconds/60%60)
	}
}
