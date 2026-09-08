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
