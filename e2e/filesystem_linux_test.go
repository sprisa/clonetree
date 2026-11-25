package e2e

import (
	"fmt"
	"os"
	"os/exec"
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
	switch uint64(stat.Type) & 0xffffffff {
	case unix.XFS_SUPER_MAGIC:
		return "xfs", nil
	case unix.BTRFS_SUPER_MAGIC:
		return "btrfs", nil
	default:
		return fmt.Sprintf("0x%x", stat.Type), nil
	}
}

func verifyShared(t *testing.T, root, source, clone string, size int) {
	t.Helper()
	for _, path := range []string{source, clone} {
		cmd := exec.Command("filefrag", "-v", path)
		cmd.Env = append(os.Environ(), "LC_ALL=C")
		output, err := cmd.CombinedOutput()
		shared := false
		for _, line := range strings.Split(string(output), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || !strings.HasSuffix(fields[0], ":") {
				continue
			}
			if _, err := strconv.Atoi(strings.TrimSuffix(fields[0], ":")); err != nil {
				continue
			}
			shared = shared || strings.Contains(","+fields[len(fields)-1]+",", ",shared,")
		}
		if err != nil || !shared {
			t.Fatalf("file extents are not shared for %s: %v\n%s", path, err, output)
		}
	}
	// info agrees: a fresh clone uses almost no storage of its own.
	if private := privateUsage(t, root); private >= float64(size)/2 {
		t.Fatalf("fresh clone reports %v bytes of disk used", private)
	}
}

func verifyPrivateGrowth(t *testing.T, root string, size int) {
	t.Helper()
	// Rewriting the original leaves the clone's extents unshared.
	if private := privateUsage(t, root); private < float64(size)/2 {
		t.Fatalf("clone did not retain the original's overwritten data: %v bytes of disk used", private)
	}
}
