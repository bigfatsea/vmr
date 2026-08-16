// Ver 2026-08-16 18:30, by Gemini 3.7 Flash

package story

import (
	"testing"
)

func TestExtractActionablePlan(t *testing.T) {
	t.Parallel()

	t.Run("standard numbered list", func(t *testing.T) {
		text := "My plan is:\n1. Check database schema\n2. Run migration script\n3. Verify data integrity"
		items := ExtractActionablePlan(text)
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
		if items[0].Text != "Check database schema" || items[0].Kind != "numbered" {
			t.Errorf("item 0 mismatch: %+v", items[0])
		}
		if items[2].Text != "Verify data integrity" || items[2].Index != 3 {
			t.Errorf("item 2 mismatch: %+v", items[2])
		}
	})

	t.Run("markdown checklist", func(t *testing.T) {
		text := "Here is the checklist:\n- [ ] Read configuration\n- [x] Edit server settings\n- [ ] Restart server"
		items := ExtractActionablePlan(text)
		if len(items) != 3 {
			t.Fatalf("expected 3 items, got %d", len(items))
		}
		if items[0].Text != "Read configuration" || items[0].Kind != "checklist" {
			t.Errorf("checklist item 0 mismatch: %+v", items[0])
		}
	})

	t.Run("step and phase prefixes in english and chinese", func(t *testing.T) {
		textEN := "Plan:\nStep 1: Fetch remote branch\nStep 2: Rebase onto main\nStep 3: Push changes"
		itemsEN := ExtractActionablePlan(textEN)
		if len(itemsEN) != 3 {
			t.Fatalf("expected 3 items for Step prefix, got %d", len(itemsEN))
		}
		if itemsEN[0].Kind != "step" || itemsEN[0].Index != 1 {
			t.Errorf("step item 0 mismatch: %+v", itemsEN[0])
		}

		textZH := "执行步骤：\n步骤一：检查系统日志\n步骤二：定位死锁问题\n步骤三：释放锁资源"
		itemsZH := ExtractActionablePlan(textZH)
		if len(itemsZH) != 3 {
			t.Fatalf("expected 3 items for Chinese step prefix, got %d", len(itemsZH))
		}
		if itemsZH[1].Text != "定位死锁问题" || itemsZH[1].Index != 2 {
			t.Errorf("step item 1 mismatch: %+v", itemsZH[1])
		}
	})

	t.Run("multiple lists pick the latest one", func(t *testing.T) {
		text := "Initial analysis:\n1. Point A\n2. Point B\n\nFinal Action Plan:\n- [ ] Task 1\n- [ ] Task 2\n- [ ] Task 3"
		items := ExtractActionablePlan(text)
		if len(items) != 3 {
			t.Fatalf("expected 3 items from the latest checklist, got %d: %+v", len(items), items)
		}
		if items[0].Kind != "checklist" || items[0].Text != "Task 1" {
			t.Errorf("expected checklist items, got %+v", items)
		}
	})

	t.Run("two unrelated checklists: only the later, proximate one is the plan", func(t *testing.T) {
		// Checklists have no ordinal to detect a run break from (unlike
		// numbered/step items), so continuity is judged by line proximity.
		// An "already done" checklist followed by unrelated prose and then
		// a real "still to do" checklist must not merge into one plan — the
		// earlier items would never be referenced again (they're already
		// done) and would misread as an execution gap.
		text := "Already done:\n- [x] irrelevant item A\n- [x] irrelevant item B\n\n" +
			"Some unrelated narration about earlier parts of this session that has " +
			"nothing to do with the plan below.\n\n" +
			"Still to do:\n- [ ] real task 1\n- [ ] real task 2"
		items := ExtractActionablePlan(text)
		if len(items) != 2 {
			t.Fatalf("expected 2 items from only the later checklist, got %d: %+v", len(items), items)
		}
		if items[0].Text != "real task 1" || items[1].Text != "real task 2" {
			t.Errorf("expected only the later checklist's items, got %+v", items)
		}
	})

	t.Run("checklist split by a single blank line is still one run", func(t *testing.T) {
		text := "Plan:\n- [ ] task 1\n\n- [ ] task 2\n- [ ] task 3"
		items := ExtractActionablePlan(text)
		if len(items) != 3 {
			t.Fatalf("expected 3 items across the blank-line gap, got %d: %+v", len(items), items)
		}
	})
}
