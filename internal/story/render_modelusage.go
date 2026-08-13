// Ver 2026-08-12 23:40, by Opus 5

// This Journey's upstream model usage & switches — see modelusage.go's
// package doc comment for why the upstream identity comes from the
// endpoint, never Manifest.Model. Deliberately its own file, not folded
// into render_spine.go (which has essentially no line budget left — see
// internal/archtest's TestArchitecture_CoreFileSizes).
package story

import (
	"vmr/internal/i18n"
)

func renderModelUsage(w func(string, ...any), m Metrics, lang i18n.Lang) {
	if len(m.ModelUsage) == 0 {
		return
	}
	t := i18n.ModelUsage(lang)
	w("%s\n\n", t.Title)
	w("%s", t.UsageHeader)
	for _, u := range m.ModelUsage {
		w("| %s (%s) | %d | %s | %s | %s |\n",
			u.Model, u.Provider, u.Steps, fmtTokens(u.TokensIn), fmtTokens(u.TokensInCached), fmtTokens(u.TokensOut))
	}
	w("\n")

	if len(m.ModelSwitches) == 0 {
		w("%s", t.NoSwitches)
		return
	}
	w("%s", t.SwitchTitle)
	for _, sw := range m.ModelSwitches {
		w("%s", t.SwitchLine(sw.StepSeq, sw.From, sw.To))
		if sw.OnFailoverStep {
			w("%s", t.OnFailoverNote)
		}
		w("\n")
	}
	w("\n")
}
