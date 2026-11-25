//go:build darwin || linux

package e2e

import (
	"strconv"
	"strings"
	"testing"
)

// privateUsage is the DISK USED value info reports for the snapshot: APFS
// private size on macOS, unshared extents on XFS and Btrfs.
func privateUsage(t *testing.T, root string) float64 {
	t.Helper()
	output := success(t, root, "info", "--no-progress")
	for _, line := range strings.Split(output, "\n") {
		// ID, DISK USED (APFS private size), SAVED, FULL COPY.
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] != "snapshot" {
			continue
		}
		value, err := strconv.ParseFloat(fields[1], 64)
		units := map[string]float64{"B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30}
		scale, ok := units[fields[2]]
		if err != nil || !ok {
			t.Fatalf("invalid disk used: %s", line)
		}
		return value * scale
	}
	t.Fatalf("disk used was not available: %s", output)
	return 0
}
