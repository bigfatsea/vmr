// Ver 2026-09-08, by pi (coding)

package journey

import (
	"testing"
)

func TestComputeTaskClusters(t *testing.T) {
	rows := []JourneyIndexRow{
		{
			ID:       "j-1",
			Client:   "lobster",
			Title:    "fix json parsing bug in parser.go",
			Category: CategoryTask,
			Requests: 5,
		},
		{
			ID:       "j-2",
			Client:   "lobster",
			Title:    "fix json parsing error in parser.go",
			Category: CategoryTask,
			Requests: 7,
		},
		{
			ID:       "j-3",
			Client:   "pimini",
			Title:    "heartbeat ping",
			Category: CategoryHeartbeat,
			Requests: 1,
		},
		{
			ID:       "j-4",
			Client:   "pimini",
			Title:    "implement authentication middleware in server.go",
			Category: CategoryTask,
			Requests: 12,
		},
	}

	clusters := ComputeTaskClusters(rows)
	if len(clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(clusters))
	}
	c := clusters[0]
	if c.Size != 2 {
		t.Errorf("cluster size = %d, want 2", c.Size)
	}
	if len(c.Members) != 2 {
		t.Errorf("members count = %d, want 2", len(c.Members))
	}
}

func TestComputeTaskClusters_CheapestFastest(t *testing.T) {
	rows := []JourneyIndexRow{
		{ID: "j-a", Title: "refresh the standard price table", Category: CategoryTask, Requests: 4,
			Cost: fp(0.12), NetWorkingMS: 90_000, Model: "gpt-5"},
		{ID: "j-b", Title: "refresh the standard pricing table", Category: CategoryTask, Requests: 6,
			Cost: fp(0.03), NetWorkingMS: 140_000, Model: "deepseek-v4"},
		{ID: "j-c", Title: "refresh standard price table again", Category: CategoryTask, Requests: 5,
			Cost: fp(0.07), NetWorkingMS: 45_000, Model: "deepseek-v4"},
	}
	cs := ComputeTaskClusters(rows)
	if len(cs) != 1 {
		t.Fatalf("want 1 cluster, got %d", len(cs))
	}
	if cs[0].Cheapest != "j-b" {
		t.Errorf("cheapest = %q, want j-b", cs[0].Cheapest)
	}
	if cs[0].Fastest != "j-c" {
		t.Errorf("fastest = %q, want j-c", cs[0].Fastest)
	}
	// members carry the projected metrics
	if cs[0].Members[0].Cost == nil || cs[0].Members[0].Model == "" {
		t.Errorf("member metrics not projected: %+v", cs[0].Members[0])
	}
	a, b := clusterComparePair(cs[0])
	if a != "j-b" || b != "j-a" { // cheapest vs priciest
		t.Errorf("compare pair = (%q,%q), want (j-b,j-a)", a, b)
	}

	// fewer than two members carry cost -> no cheapest mark
	rows[1].Cost, rows[2].Cost = nil, nil
	cs = ComputeTaskClusters(rows)
	if cs[0].Cheapest != "" {
		t.Errorf("cheapest = %q, want empty when <2 members priced", cs[0].Cheapest)
	}
}

func TestCleanAnchorTitle(t *testing.T) {
	cases := map[string]string{
		"[cron:7d9d8-11e Daily News Brief (08:00 Asia/Shanghai…": "Daily News Brief (08:00 Asia/Shanghai…",
		"[cron:abc Weekly Startup Idea Brief]":                   "Weekly Startup Idea Brief",
		"[subagent:x do a thing] and more":                       "do a thing and more",
		"plain task title":                                       "plain task title",
	}
	for in, want := range cases {
		if got := cleanAnchorTitle(in); got != want {
			t.Errorf("cleanAnchorTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
