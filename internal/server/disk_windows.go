// Ver 2026-08-23 14:48, by Gemini
//go:build windows

package server

func diskFreeSpace(path string) (uint64, error) {
	return 0, nil
}
