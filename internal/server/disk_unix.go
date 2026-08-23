// Ver 2026-08-23 14:48, by Gemini
//go:build !windows

package server

import (
	"syscall"
)

func diskFreeSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bavail) * uint64(stat.Bsize), nil
}
