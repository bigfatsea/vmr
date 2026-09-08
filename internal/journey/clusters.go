// Ver 2026-09-08, by pi (coding)

package journey

import (
	"sort"
	"strings"
	"unicode"
)

// ClusterMember is one Journey candidate within a TaskCluster.
type ClusterMember struct {
	ID       string `json:"id"`
	Client   string `json:"client,omitempty"`
	Requests int    `json:"requests"`
	Steps    int    `json:"steps,omitempty"`
	Rendered string `json:"rendered,omitempty"`
}

// TaskCluster groups multiple Journey runs that share a substantially similar task instruction.
type TaskCluster struct {
	AnchorTitle string          `json:"anchor_title"`
	Size        int             `json:"size"`
	Members     []ClusterMember `json:"members"`
}

// ComputeTaskClusters groups JourneyIndexRow items by instruction similarity.
// Heartbeat and noise tasks are excluded. Minimum similarity threshold is 0.45.
func ComputeTaskClusters(rows []JourneyIndexRow) []TaskCluster {
	if len(rows) < 2 {
		return nil
	}

	type taskItem struct {
		row    JourneyIndexRow
		tokens map[string]struct{}
	}

	var candidates []taskItem
	for _, r := range rows {
		// exclude noise categories
		if IsNoiseCategory(r.Category) {
			continue
		}
		title := strings.TrimSpace(r.Title)
		if title == "" {
			continue
		}
		tokens := tokenizeTitle(title)
		if len(tokens) == 0 {
			continue
		}
		candidates = append(candidates, taskItem{row: r, tokens: tokens})
	}

	if len(candidates) < 2 {
		return nil
	}

	visited := make([]bool, len(candidates))
	var clusters []TaskCluster

	for i := 0; i < len(candidates); i++ {
		if visited[i] {
			continue
		}
		group := []taskItem{candidates[i]}
		visited[i] = true

		for j := i + 1; j < len(candidates); j++ {
			if visited[j] {
				continue
			}
			sim := jaccardSimilarity(candidates[i].tokens, candidates[j].tokens)
			if sim >= 0.45 {
				visited[j] = true
				group = append(group, candidates[j])
			}
		}

		// Only form a cluster if 2 or more journeys match
		if len(group) >= 2 {
			members := make([]ClusterMember, 0, len(group))
			for _, item := range group {
				members = append(members, ClusterMember{
					ID:       item.row.ID,
					Client:   item.row.Client,
					Requests: item.row.Requests,
					Steps:    item.row.Steps,
					Rendered: item.row.Rendered,
				})
			}
			clusters = append(clusters, TaskCluster{
				AnchorTitle: group[0].row.Title,
				Size:        len(members),
				Members:     members,
			})
		}
	}

	// Sort clusters by size desc
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Size > clusters[j].Size
	})

	return clusters
}

func tokenizeTitle(s string) map[string]struct{} {
	tokens := map[string]struct{}{}
	var cur strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(unicode.ToLower(r))
		} else {
			if cur.Len() > 0 {
				tokens[cur.String()] = struct{}{}
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		tokens[cur.String()] = struct{}{}
	}
	return tokens
}

func jaccardSimilarity(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for k := range a {
		if _, ok := b[k]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union <= 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
