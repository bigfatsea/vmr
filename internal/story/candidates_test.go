// Ver 2026-08-20 16:10, by Sonnet 5

package story

import "testing"

// TestClassifyJourney locks the three literal markers verified against the
// full local logs/ corpus (see classifyJourney's own doc comment) — real
// title shapes, not the architecture doc's paraphrased examples.
func TestClassifyJourney(t *testing.T) {
	cases := []struct {
		name  string
		title string
		want  JourneyCategory
	}{
		{"cron prefix", "[cron:defe7721-2b0e-436c-a419-7b9b8c1c797a snap-24h-hourly] [snap-24h ...]", CategoryCron},
		{"heartbeat after timestamp bracket", "[Tue 2026-07-14 11:53 GMT+8] [OpenClaw heartbeat poll]", CategoryHeartbeat},
		{"subagent after timestamp bracket", "[Thu 2026-07-16 15:44 GMT+8] [Subagent Context] You are running as a subagent", CategorySubagent},
		{"plain real task, no marker", "我要写第一个Release了，那么请你阅读我们当前这个项目的README文档", CategoryTask},
		{"short but genuine interaction is still a task, not noise", "hi back", CategoryTask},
		{"timestamp bracket alone (not heartbeat/subagent) is still a task", "[Tue 2026-07-14 18:06 GMT+8] do something real", CategoryTask},
		{"empty title", "", CategoryTask},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyJourney(c.title); got != c.want {
				t.Errorf("classifyJourney(%q) = %q, want %q", c.title, got, c.want)
			}
		})
	}
}

// TestClassifyJourney_CronTakesPriority documents the fixed priority order
// (cron, then heartbeat, then subagent) for the hypothetical case where a
// future client combines markers — real-corpus verification found zero
// overlap, but the switch statement's order must still be deterministic,
// not accidentally dependent on branch reordering by a future edit.
func TestClassifyJourney_CronTakesPriority(t *testing.T) {
	combined := "[cron:xyz job] [OpenClaw heartbeat poll] [Subagent Context] hypothetical combined marker"
	if got := classifyJourney(combined); got != CategoryCron {
		t.Errorf("classifyJourney with all three markers present = %q, want %q (cron checked first)", got, CategoryCron)
	}
}
