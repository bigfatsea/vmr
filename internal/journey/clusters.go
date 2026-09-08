// Ver 2026-09-08, by pi (coding)

package journey

import (
	"sort"
	"strings"
	"unicode"
)

// ClusterMember is one Journey candidate within a TaskCluster.
type ClusterMember struct {
	ID           string   `json:"id"`
	Client       string   `json:"client,omitempty"`
	Requests     int      `json:"requests"`
	Steps        int      `json:"steps,omitempty"`
	Rendered     string   `json:"rendered,omitempty"`
	Cost         *float64 `json:"cost,omitempty"`
	NetWorkingMS int64    `json:"net_working_ms,omitempty"`
	Model        string   `json:"model,omitempty"`
}

// TaskCluster groups multiple Journey runs that share a substantially similar task instruction.
// Cheapest / Fastest are member IDs (empty when the metric isn't resolvable for
// at least two members) — the "which of these N runs to keep" answer.
type TaskCluster struct {
	AnchorTitle string          `json:"anchor_title"`
	Size        int             `json:"size"`
	Members     []ClusterMember `json:"members"`
	Cheapest    string          `json:"cheapest,omitempty"`
	Fastest     string          `json:"fastest,omitempty"`
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
					ID:           item.row.ID,
					Client:       item.row.Client,
					Requests:     item.row.Requests,
					Steps:        item.row.Steps,
					Rendered:     item.row.Rendered,
					Cost:         item.row.Cost,
					NetWorkingMS: item.row.NetWorkingMS,
					Model:        item.row.Model,
				})
			}
			cheapest, fastest := clusterExtremes(members)
			clusters = append(clusters, TaskCluster{
				AnchorTitle: cleanAnchorTitle(group[0].row.Title),
				Size:        len(members),
				Members:     members,
				Cheapest:    cheapest,
				Fastest:     fastest,
			})
		}
	}

	// Sort clusters by size desc
	sort.Slice(clusters, func(i, j int) bool {
		return clusters[i].Size > clusters[j].Size
	})

	return clusters
}

// clusterExtremes returns the member IDs with the lowest resolved cost and the
// lowest non-zero net working time. Either is "" when fewer than two members
// carry that metric — nothing to compare.
func clusterExtremes(members []ClusterMember) (cheapest, fastest string) {
	var bestCost *float64
	costN := 0
	var bestWall int64
	wallN := 0
	for _, m := range members {
		if m.Cost != nil {
			costN++
			if bestCost == nil || *m.Cost < *bestCost {
				bestCost = m.Cost
				cheapest = m.ID
			}
		}
		if m.NetWorkingMS > 0 {
			wallN++
			if bestWall == 0 || m.NetWorkingMS < bestWall {
				bestWall = m.NetWorkingMS
				fastest = m.ID
			}
		}
	}
	if costN < 2 {
		cheapest = ""
	}
	if wallN < 2 {
		fastest = ""
	}
	return cheapest, fastest
}

// cleanAnchorTitle strips a leading "[cron:<uuid> " / "[subagent:<id> "
// scaffolding bracket so a cluster of scheduled runs shows the human-readable
// task name, not the UUID that made them cluster in the first place. The title
// may be truncated mid-bracket (no closing "]"), so it also handles that.
func cleanAnchorTitle(s string) string {
	t := strings.TrimSpace(s)
	if !strings.HasPrefix(t, "[") {
		return t
	}
	// "[tag:id readable name] rest"  ->  "readable name rest"
	if close := strings.IndexByte(t, ']'); close >= 0 {
		inner := t[1:close]
		rest := strings.TrimRight(t[close+1:], " )")
		if sp := strings.IndexByte(inner, ' '); sp >= 0 {
			if tail := strings.TrimSpace(inner[sp+1:]); tail != "" {
				return tail + rest
			}
		}
		return strings.TrimSpace(rest)
	}
	// truncated before "]": "[cron:<uuid> Daily News Brief (08:00…"
	if sp := strings.IndexByte(t, ' '); sp >= 0 {
		if tail := strings.TrimSpace(t[sp+1:]); tail != "" {
			return tail
		}
	}
	return t
}

// DominantModel is the real model name that served the most steps of a journey —
// the label a task-cluster row shows next to cost and wall time.
func DominantModel(m Metrics) string {
	best := ""
	bestSteps := 0
	for _, u := range m.ModelUsage {
		if u.Steps > bestSteps {
			bestSteps = u.Steps
			best = u.Model
		}
	}
	return best
}

// clusterComparePair picks the two members worth diffing: the cheapest and the
// priciest when cost separates them, otherwise the first two in the cluster.
func clusterComparePair(c TaskCluster) (a, b string) {
	if len(c.Members) < 2 {
		return "", ""
	}
	if c.Cheapest != "" {
		var priciest string
		var hi *float64
		for _, m := range c.Members {
			if m.Cost != nil && (hi == nil || *m.Cost > *hi) {
				hi = m.Cost
				priciest = m.ID
			}
		}
		if priciest != "" && priciest != c.Cheapest {
			return c.Cheapest, priciest
		}
	}
	return c.Members[0].ID, c.Members[1].ID
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
