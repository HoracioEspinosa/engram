//go:build !windows

package store

import "syscall"

// availableBytes reports how many bytes a non-privileged process can still
// write to the filesystem holding path.
func availableBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
}
