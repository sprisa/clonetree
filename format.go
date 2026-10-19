package main

import (
	"strconv"
	"time"
)

func appendBytes(dst []byte, n uint64) []byte {
	if n < 1024 {
		dst = strconv.AppendUint(dst, n, 10)
		return append(dst, " B"...)
	}
	value := float64(n)
	for _, unit := range [...]string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB"} {
		value /= 1024
		if value < 1024 || unit == "EiB" {
			dst = strconv.AppendFloat(dst, value, 'f', 1, 64)
			dst = append(dst, ' ')
			return append(dst, unit...)
		}
	}
	panic("unreachable")
}

func bytesString(n uint64) string {
	var buffer [32]byte
	return string(appendBytes(buffer[:0], n))
}

func appendCount(dst []byte, n float64) []byte {
	for _, suffix := range [...]string{"", "k", "M", "G", "T"} {
		if n < 1000 || suffix == "T" {
			precision := 1
			if suffix == "" {
				precision = 0
			}
			dst = strconv.AppendFloat(dst, n, 'f', precision, 64)
			return append(dst, suffix...)
		}
		n /= 1000
	}
	panic("unreachable")
}

// Progress times are nonnegative and rounded to tenths of a second or seconds.
func appendDuration(dst []byte, d time.Duration) []byte {
	if d == 0 {
		return append(dst, "0s"...)
	}
	if d < time.Second {
		dst = strconv.AppendInt(dst, int64(d/time.Millisecond), 10)
		return append(dst, "ms"...)
	}
	if d >= time.Hour {
		dst = strconv.AppendUint(dst, uint64(d/time.Hour), 10)
		dst = append(dst, 'h')
	}
	if d >= time.Minute {
		dst = strconv.AppendUint(dst, uint64(d/time.Minute%60), 10)
		dst = append(dst, 'm')
	}
	dst = strconv.AppendFloat(dst, float64(d%time.Minute)/float64(time.Second), 'f', -1, 64)
	return append(dst, 's')
}
