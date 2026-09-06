// Ver 2026-09-06, by Gemini 3.8 Flash

package journey

import (
	"os"
	"path/filepath"
	"strings"
)

// CleanOrphanJourneys scans detailsDir (strictly limited to journeys/details/ per D20 / §3.4)
// and removes any expired j-<id>.* detail files whose ID is not present in activeIDs.
//
// Boundary invariants:
// 1. Only direct file entries within detailsDir are inspected (non-recursive).
// 2. Only files starting with "j-" and ending with recognized extensions (.json, .md) are subject to cleanup.
// 3. Subdirectories are never removed or traversed.
// 4. Never touches compares/ or requests/ (enforced by path scope and directory isolation).
func CleanOrphanJourneys(detailsDir string, activeIDs []string) (cleaned int, err error) {
	if detailsDir == "" {
		return 0, nil
	}
	cleanDir := filepath.Clean(detailsDir)
	entries, err := os.ReadDir(cleanDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	activeSet := make(map[string]bool, len(activeIDs)*2)
	for _, id := range activeIDs {
		if id == "" {
			continue
		}
		activeSet[id] = true
		if strings.HasPrefix(id, "j-") {
			activeSet[strings.TrimPrefix(id, "j-")] = true
		} else {
			activeSet["j-"+id] = true
		}
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Only files following the j-<id>.* naming convention are candidates for cleanup.
		if !strings.HasPrefix(name, "j-") {
			continue
		}

		ext := filepath.Ext(name)
		if ext != ".json" && ext != ".md" {
			continue
		}

		stem := strings.TrimSuffix(name, ext)
		if !activeSet[stem] {
			target := filepath.Join(cleanDir, name)
			if removeErr := os.Remove(target); removeErr != nil {
				if !os.IsNotExist(removeErr) {
					return cleaned, removeErr
				}
			} else {
				cleaned++
			}
		}
	}

	return cleaned, nil
}
