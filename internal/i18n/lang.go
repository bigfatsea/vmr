// Ver 2026-08-01, by Sonnet 5

// Package i18n holds the two-language (English/Chinese) text vmr report and
// vmr story render into their Markdown/CLI output. It is a zero-dependency
// leaf package, same tier as internal/core and internal/fmtutil: it
// declares no import on internal/config, internal/router, internal/server,
// internal/report, or internal/story, so report and story can depend on it
// without violating internal/archtest's import boundaries,
// and cmd/vmr can depend on it to parse -lang/report.yaml without either of
// them needing to know about configuration at all.
//
// Text lives one file per producing source file (report_workload.go pairs
// with internal/report/section_workload.go, story_render.go pairs with
// internal/story/render_md.go, ...) rather than in one shared catalog — see
// docs/VirtualModelRouter_Design_v4_Analytics.md's output-language section
// for why: a wording change stays in one small file next to the code that
// renders it, instead of a separate catalog file whose entries can silently
// drift out of sync with the section they back.
package i18n

import (
	"fmt"
	"strings"
)

// Lang selects vmr report/vmr story's output language. The zero value is
// EN, so any call site not yet updated during incremental migration keeps
// behaving as English by construction.
type Lang uint8

const (
	EN Lang = iota
	ZH
)

func (l Lang) String() string {
	if l == ZH {
		return "zh"
	}
	return "en"
}

// Parse accepts "en"/"english" and "zh"/"chinese"/"zh-cn" (case-
// insensitive); anything else is an error. Only two real values exist — no
// alias registry until a third language is a real requirement.
func Parse(s string) (Lang, error) {
	switch strings.ToLower(s) {
	case "en", "english":
		return EN, nil
	case "zh", "chinese", "zh-cn", "zh_cn":
		return ZH, nil
	default:
		return EN, fmt.Errorf("unsupported language %q (expected \"en\" or \"zh\")", s)
	}
}
