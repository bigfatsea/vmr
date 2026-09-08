// Ver 2026-09-16, by pi

package journey

import (
	"testing"

	"vmr/internal/chatmsg"
	"vmr/internal/ctxgraph"
)

func TestComputeCacheBreak_NilCases(t *testing.T) {
	t.Parallel()
	m := &ctxgraph.Manifest{}
	if got := ComputeCacheBreak(nil, m, nil, nil); got != CacheBreakNone {
		t.Errorf("got %q, want %q", got, CacheBreakNone)
	}
	if got := ComputeCacheBreak(m, nil, nil, nil); got != CacheBreakNone {
		t.Errorf("got %q, want %q", got, CacheBreakNone)
	}
	if got := ComputeCacheBreak(nil, nil, nil, nil); got != CacheBreakNone {
		t.Errorf("got %q, want %q", got, CacheBreakNone)
	}
}

func TestComputeCacheBreak_StitchBoundary(t *testing.T) {
	t.Parallel()
	prev := &ctxgraph.Manifest{
		ServedEndpoint: "openai:prov:m1",
		HasSys:         true,
		SysHash:        ctxgraph.Hash{1},
	}
	cur := &ctxgraph.Manifest{
		ServedEndpoint: "openai:prov:m2", // even with different endpoint / sys, stitch takes priority
		HasSys:         true,
		SysHash:        ctxgraph.Hash{2},
	}
	stitch := &ctxgraph.StitchEdge{Kind: ctxgraph.StitchCompaction, Score: 0.9}
	if got := ComputeCacheBreak(prev, cur, nil, stitch); got != CacheBreakHistoryStitch {
		t.Errorf("got %q, want %q", got, CacheBreakHistoryStitch)
	}
}

func TestComputeCacheBreak_SystemPrompt(t *testing.T) {
	t.Parallel()
	// Text changed
	prev := &ctxgraph.Manifest{
		HasSys:         true,
		SysHash:        ctxgraph.Hash{1},
		ServedEndpoint: "openai:prov:m1",
	}
	cur := &ctxgraph.Manifest{
		HasSys:         true,
		SysHash:        ctxgraph.Hash{2},
		ServedEndpoint: "openai:prov:m1",
	}
	edge := &ctxgraph.Edit{Kind: ctxgraph.Append}
	if got := ComputeCacheBreak(prev, cur, edge, nil); got != CacheBreakSystem {
		t.Errorf("sys text changed: got %q, want %q", got, CacheBreakSystem)
	}

	// Sys added
	prevNoSys := &ctxgraph.Manifest{HasSys: false, ServedEndpoint: "openai:prov:m1"}
	if got := ComputeCacheBreak(prevNoSys, cur, edge, nil); got != CacheBreakSystem {
		t.Errorf("sys added: got %q, want %q", got, CacheBreakSystem)
	}

	// Sys removed
	curNoSys := &ctxgraph.Manifest{HasSys: false, ServedEndpoint: "openai:prov:m1"}
	if got := ComputeCacheBreak(prev, curNoSys, edge, nil); got != CacheBreakSystem {
		t.Errorf("sys removed: got %q, want %q", got, CacheBreakSystem)
	}
}

func TestComputeCacheBreak_ToolsChurn(t *testing.T) {
	t.Parallel()
	sysHash := ctxgraph.Hash{1}
	prev := &ctxgraph.Manifest{
		HasSys:         true,
		SysHash:        sysHash,
		HasTools:       true,
		ToolsHash:      ctxgraph.Hash{10},
		ServedEndpoint: "openai:prov:m1",
	}
	cur := &ctxgraph.Manifest{
		HasSys:         true,
		SysHash:        sysHash,
		HasTools:       true,
		ToolsHash:      ctxgraph.Hash{20},
		ServedEndpoint: "openai:prov:m1",
	}
	edge := &ctxgraph.Edit{Kind: ctxgraph.Append}

	// Both sides declare tools and hash changed -> tools
	if got := ComputeCacheBreak(prev, cur, edge, nil); got != CacheBreakTools {
		t.Errorf("tools churn: got %q, want %q", got, CacheBreakTools)
	}

	// One side has_tools=false -> should NOT attribute to tools
	prevNoTools := &ctxgraph.Manifest{
		HasSys:         true,
		SysHash:        sysHash,
		HasTools:       false,
		ServedEndpoint: "openai:prov:m1",
	}
	if got := ComputeCacheBreak(prevNoTools, cur, edge, nil); got == CacheBreakTools {
		t.Errorf("single side tools should not attribute to tools, got %q", got)
	}
}

func TestComputeCacheBreak_ProviderSwitch(t *testing.T) {
	t.Parallel()
	sysHash := ctxgraph.Hash{1}
	toolsHash := ctxgraph.Hash{10}
	prev := &ctxgraph.Manifest{
		HasSys:         true,
		SysHash:        sysHash,
		HasTools:       true,
		ToolsHash:      toolsHash,
		ServedEndpoint: "openai:prov-a:m1",
	}
	cur := &ctxgraph.Manifest{
		HasSys:         true,
		SysHash:        sysHash,
		HasTools:       true,
		ToolsHash:      toolsHash,
		ServedEndpoint: "openai:prov-b:m1",
	}
	edge := &ctxgraph.Edit{Kind: ctxgraph.Append}

	if got := ComputeCacheBreak(prev, cur, edge, nil); got != CacheBreakProviderSwitch {
		t.Errorf("provider switch: got %q, want %q", got, CacheBreakProviderSwitch)
	}

	// Empty endpoint on one side does not trigger switch
	prevEmptyEp := &ctxgraph.Manifest{
		HasSys:         true,
		SysHash:        sysHash,
		HasTools:       true,
		ToolsHash:      toolsHash,
		ServedEndpoint: "",
	}
	if got := ComputeCacheBreak(prevEmptyEp, cur, edge, nil); got == CacheBreakProviderSwitch {
		t.Errorf("empty endpoint should not trigger provider_switch, got %q", got)
	}
}

func TestComputeCacheBreak_HistoryEdits(t *testing.T) {
	t.Parallel()
	sysHash := ctxgraph.Hash{1}
	toolsHash := ctxgraph.Hash{10}
	prev := &ctxgraph.Manifest{
		HasSys:         true,
		SysHash:        sysHash,
		HasTools:       true,
		ToolsHash:      toolsHash,
		ServedEndpoint: "openai:prov:m1",
	}
	cur := &ctxgraph.Manifest{
		HasSys:         true,
		SysHash:        sysHash,
		HasTools:       true,
		ToolsHash:      toolsHash,
		ServedEndpoint: "openai:prov:m1",
	}

	cases := []struct {
		kind ctxgraph.EditKind
		want CacheBreakKind
	}{
		{ctxgraph.ReplaceTail, "history:replace_tail"},
		{ctxgraph.Splice, "history:splice"},
		{ctxgraph.Contract, "history:contract"},
		{ctxgraph.Fork, "history:fork"},
	}

	for _, tc := range cases {
		edge := &ctxgraph.Edit{Kind: tc.kind}
		if got := ComputeCacheBreak(prev, cur, edge, nil); got != tc.want {
			t.Errorf("edit kind %v: got %q, want %q", tc.kind, got, tc.want)
		}
	}
}

func TestComputeCacheBreak_UnexplainedDrop(t *testing.T) {
	t.Parallel()
	sysHash := ctxgraph.Hash{1}
	toolsHash := ctxgraph.Hash{10}
	edge := &ctxgraph.Edit{Kind: ctxgraph.Append}

	mkManifest := func(in, cached int64, inOK bool) *ctxgraph.Manifest {
		return &ctxgraph.Manifest{
			HasSys:         true,
			SysHash:        sysHash,
			HasTools:       true,
			ToolsHash:      toolsHash,
			ServedEndpoint: "openai:prov:m1",
			Usage:          chatmsg.Usage{In: in, CacheRead: cached},
			UsageInOK:      inOK,
		}
	}

	// 1. Drop: 90% -> 40% (drop 50%, prev >= 50%) -> unexplained
	prev := mkManifest(1000, 900, true)
	cur := mkManifest(2000, 800, true) // 800/2000 = 40%
	if got := ComputeCacheBreak(prev, cur, edge, nil); got != CacheBreakUnexplained {
		t.Errorf("unexplained drop: got %q, want %q", got, CacheBreakUnexplained)
	}

	// 2. Normal append: 80% -> 75% (drop 5% < 25%) -> none
	curNormal := mkManifest(1200, 900, true) // 900/1200 = 75%
	if got := ComputeCacheBreak(prev, curNormal, edge, nil); got != CacheBreakNone {
		t.Errorf("normal append: got %q, want %q", got, CacheBreakNone)
	}

	// 3. Prev had low ratio: 40% -> 10% (prev < 50%) -> none
	prevLow := mkManifest(1000, 400, true)
	curLow := mkManifest(2000, 200, true)
	if got := ComputeCacheBreak(prevLow, curLow, edge, nil); got != CacheBreakNone {
		t.Errorf("prev < 50%%: got %q, want %q", got, CacheBreakNone)
	}

	// 4. UsageInOK false on either side -> none
	prevNoOK := mkManifest(1000, 900, false)
	if got := ComputeCacheBreak(prevNoOK, cur, edge, nil); got != CacheBreakNone {
		t.Errorf("prev UsageInOK=false: got %q, want %q", got, CacheBreakNone)
	}
	curNoOK := mkManifest(2000, 800, false)
	if got := ComputeCacheBreak(prev, curNoOK, edge, nil); got != CacheBreakNone {
		t.Errorf("cur UsageInOK=false: got %q, want %q", got, CacheBreakNone)
	}
}

func TestShouldDisplayCacheBreak(t *testing.T) {
	t.Parallel()
	displays := []string{
		"unexplained",
		"provider_switch",
		"system",
		"tools",
		"history:stitch",
		"history:contract",
		"history:fork",
	}
	for _, d := range displays {
		if !ShouldDisplayCacheBreak(d) {
			t.Errorf("ShouldDisplayCacheBreak(%q) = false, want true", d)
		}
	}

	suppressed := []string{
		"",
		"history:append",
		"history:replace_tail",
		"history:splice",
		"unknown",
	}
	for _, s := range suppressed {
		if ShouldDisplayCacheBreak(s) {
			t.Errorf("ShouldDisplayCacheBreak(%q) = true, want false", s)
		}
	}
}
