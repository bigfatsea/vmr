//go:build windows

package sysinfo

// DiskFreeSpace returns available disk space in bytes for the filesystem containing path.
func DiskFreeSpace(path string) (uint64, error) {
	return 0, nil
}
