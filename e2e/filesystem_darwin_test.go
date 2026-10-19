package e2e

import (
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func filesystemType(path string) (string, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return "", err
	}
	var name []byte
	for _, c := range stat.Fstypename {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	return string(name), nil
}

func privateUsage(t *testing.T, root string) float64 {
	t.Helper()
	output := success(t, root, "info", "--no-progress")
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 || fields[0] != "snapshot" {
			continue
		}
		value, err := strconv.ParseFloat(fields[len(fields)-2], 64)
		units := map[string]float64{"B": 1, "KiB": 1 << 10, "MiB": 1 << 20, "GiB": 1 << 30}
		scale, ok := units[fields[len(fields)-1]]
		if err != nil || !ok {
			t.Fatalf("invalid APFS private size: %s", line)
		}
		return value * scale
	}
	t.Fatalf("APFS private size was not available: %s", output)
	return 0
}

func verifyShared(t *testing.T, root, source, clone string, size int) {
	t.Helper()
	if private := privateUsage(t, root); private >= float64(size)/2 {
		t.Fatalf("fresh APFS clone has too much private data: %v bytes", private)
	}
}

func verifyPrivateGrowth(t *testing.T, root string, size int) {
	t.Helper()
	if private := privateUsage(t, root); private < float64(size)/2 {
		t.Fatalf("APFS clone did not retain the original's overwritten data: %v bytes", private)
	}
}
