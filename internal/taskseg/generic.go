// Ver 2026-08-14, by Sonnet 5

package taskseg

import (
	"strings"

	"vmr/internal/chatmsg"
)

// Generic is the template-free fallback: any non-empty user message counts
// as a real instruction (nothing stripped), and NoReply only fires on a
// literally empty reassembled response — no agent-specific skip-marker
// convention to check for. Used for any request not otherwise recognized;
// see the design doc's profile-selection notes for why this isn't a Detect-based dispatch yet.
var Generic Profile = generic{}

type generic struct{}

func (generic) Name() string { return "generic" }

func (generic) RealUserText(m chatmsg.Message, rawMsgs []any, rawIdx int) (string, bool) {
	if m.Role != "user" {
		return "", false
	}
	if strings.TrimSpace(m.Text) == "" {
		return "", false
	}
	return m.Text, true
}

func (p generic) IsRealUser(m chatmsg.Message, rawMsgs []any, rawIdx int) bool {
	_, ok := p.RealUserText(m, rawMsgs, rawIdx)
	return ok
}

func (generic) NoReply(finish, content string) bool {
	return (finish == "stop" || finish == "end_turn") && strings.TrimSpace(content) == ""
}

// ChatID always returns "": Generic has no framework-specific session-id
// convention to look for.
func (generic) ChatID(msgs []chatmsg.Message) string { return "" }
