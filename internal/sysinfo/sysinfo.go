// Package sysinfo provides lightweight runtime and OS system metrics with
// zero internal dependencies.
package sysinfo

import (
	"os"
	"runtime/metrics"
)

// DirTotalSize sums sizes of all regular files in dir without recursion.
// Returns 0 if dir is empty or unreadable.
func DirTotalSize(dir string) int64 {
	if dir == "" {
		return 0
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total int64
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err == nil {
			total += info.Size()
		}
	}
	return total
}

// DiskFreeBytes returns available disk space in bytes for path (defaulting to "." if empty), or 0 on error.
func DiskFreeBytes(path string) uint64 {
	if path == "" {
		path = "."
	}
	free, _ := DiskFreeSpace(path)
	return free
}

// ReadMemAlloc reads heapAlloc and sys memory metrics using runtime/metrics
// without triggering a Stop-The-World pause.
func ReadMemAlloc() (heapAlloc uint64, sys uint64) {
	const (
		heapAllocMetric = "/memory/classes/heap/objects:bytes"
		sysMetric       = "/memory/classes/total:bytes"
	)
	samples := []metrics.Sample{
		{Name: heapAllocMetric},
		{Name: sysMetric},
	}
	metrics.Read(samples)
	if samples[0].Value.Kind() == metrics.KindUint64 {
		heapAlloc = samples[0].Value.Uint64()
	}
	if samples[1].Value.Kind() == metrics.KindUint64 {
		sys = samples[1].Value.Uint64()
	}
	return heapAlloc, sys
}
