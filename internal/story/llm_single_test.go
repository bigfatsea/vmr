// Ver 2026-09-04, by Worker 3

package story

import (
	"strings"
	"testing"

	"vmr/internal/chatmsg"
)

// TestExtractRootUserIntent_UsesInitialInstructionNotFirstUserEvent pins
// the P-D6-8 fix: extractRootUserIntent used to scan
// Tasks[*].Steps[*].NewEvents for the first user-role message, bypassing
// taskseg.Profile's agent-dialect filter — an OpenClaw-class client's
// system scaffolding masquerading as user role would pose as the root
// intent. The Journey's InitialInstruction (set by buildFrom through
// firstRealInstruction, dialect-filtered) is the only legitimate source.
func TestExtractRootUserIntent_UsesInitialInstructionNotFirstUserEvent(t *testing.T) {
	scaffold := "<system-reminder>You are OpenClaw, an autonomous agent.</system-reminder>"
	j := &Journey{
		InitialInstruction: "fix the failing router test",
		Tasks: []*Task{{
			Title: "t",
			Steps: []*Step{{
				Seq: 1,
				NewEvents: []*Event{
					// First user-role event: dialect-filtered-out scaffold.
					{Msg: chatmsg.Message{Role: "user", Text: scaffold}},
					{Msg: chatmsg.Message{Role: "assistant", Text: "on it"}},
					{Msg: chatmsg.Message{Role: "user", Text: "later follow-up"}},
				},
			}},
		}},
	}

	if got := extractRootUserIntent(j); got != j.InitialInstruction {
		t.Errorf("extractRootUserIntent = %q, want InitialInstruction %q (the scaffold user event must not leak in)", got, j.InitialInstruction)
	}
}

// TestExtractRootUserIntent_EmptyWithoutInitialInstruction covers the
// inverse: when buildFrom found no real instruction (InitialInstruction
// empty), a scaffold user event in NewEvents must NOT stand in for one —
// the old NewEvents scan returned exactly that.
func TestExtractRootUserIntent_EmptyWithoutInitialInstruction(t *testing.T) {
	j := &Journey{
		Tasks: []*Task{{
			Title: "t",
			Steps: []*Step{{
				Seq: 1,
				NewEvents: []*Event{
					{Msg: chatmsg.Message{Role: "user", Text: "<system-reminder>scaffold</system-reminder>"}},
				},
			}},
		}},
	}

	if got := extractRootUserIntent(j); got != "" {
		t.Errorf("extractRootUserIntent = %q, want \"\" (no dialect-filtered instruction exists)", got)
	}
}

// TestExtractRootUserIntent_TruncatesAt2000 covers the retained
// truncateText bound: a very long InitialInstruction comes back capped at
// 2000 runes.
func TestExtractRootUserIntent_TruncatesAt2000(t *testing.T) {
	j := &Journey{InitialInstruction: strings.Repeat("x", 3000)}

	got := extractRootUserIntent(j)
	if n := len([]rune(got)); n != 2000 {
		t.Errorf("len = %d, want 2000 (truncateText bound)", n)
	}
}
