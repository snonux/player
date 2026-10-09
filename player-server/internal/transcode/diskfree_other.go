//go:build !unix

package transcode

import "math"

// freeSpace cannot query the volume on this platform; it reports unlimited
// space so only the cache size bound applies. The server is deployed on
// Linux, where diskfree_unix.go provides the real check.
func freeSpace(string) (int64, error) {
	return math.MaxInt64, nil
}
