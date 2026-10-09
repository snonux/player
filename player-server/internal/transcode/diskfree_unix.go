//go:build unix

package transcode

import (
	"fmt"
	"math"
	"syscall"
)

// freeSpace returns the bytes available to unprivileged processes on the
// volume holding dir (the server runs as UID 65534).
func freeSpace(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}
	free := uint64(st.Bavail) * uint64(st.Bsize)
	if free > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(free), nil
}
