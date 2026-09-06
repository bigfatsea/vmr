// Ver 2026-09-15, by pi

// Deprecated: legacy renderOverviewCard/renderToolTimeline/renderFindingsSection
// eating in-memory *Journey were deleted in Phase 3 in favor of viewmodel_spine.go (D11).
// Pure helpers (structuralTags, oneLineTruncate, padRight, joinInts) retained here.
package journey

import (
	"strconv"
	"strings"

	"vmr/internal/i18n"
)

const (
	toolIntensiveThreshold = 10
	retryHeavyThreshold    = 0.2
)

func structuralTags(m Metrics, t i18n.SpineText) []string {
	var tags []string
	if m.ToolCallCount >= toolIntensiveThreshold {
		tags = append(tags, t.TagToolIntensive)
	}
	if m.DuplicateActionRate >= retryHeavyThreshold {
		tags = append(tags, t.TagRetryHeavy)
	}
	if m.CompactionCount > 0 {
		tags = append(tags, t.TagContextCompacted)
	}
	return tags
}

func oneLineTruncate(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func padRight(s string, n int) string {
	r := []rune(s)
	if len(r) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(r))
}

func joinInts(ns []int) string {
	ss := make([]string, len(ns))
	for i, n := range ns {
		ss[i] = strconv.Itoa(n)
	}
	return strings.Join(ss, ", ")
}
