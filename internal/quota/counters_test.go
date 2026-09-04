// Ver 2026-09-04 00:00, by pi-agent

package quota

import "testing"

// TestTokenCountersSides drives the four shapes the exact-vs-degraded rule
// distinguishes: both sides sniffed (exact, est=0), either side alone
// degraded, and both degraded. The Out fallback is max(u.Out, outEst) — a
// real placeholder must never beat stronger emitted-text evidence.
func TestTokenCountersSides(t *testing.T) {
	cases := []struct {
		name    string
		u       TokenUsage
		inOK    bool
		outOK   bool
		inEst   int64
		outEst  int64
		want    Counters
		wantEst float64
	}{
		{
			name: "both sniffed: exact, est=0",
			u:    TokenUsage{Fresh: 900, CacheRead: 200, CacheWrite: 50, Out: 300},
			inOK: true, outOK: true,
			want:    Counters{Fresh: 900, CacheRead: 200, CacheWrite: 50, Out: 300},
			wantEst: 0,
		},
		{
			name: "input sniffed only: In exact, Out falls back to max(placeholder, est)",
			u:    TokenUsage{Fresh: 900, CacheRead: 200, CacheWrite: 50, Out: 1},
			inOK: true, outOK: false, outEst: 640,
			want:    Counters{Fresh: 900, CacheRead: 200, CacheWrite: 50, Out: 640},
			wantEst: 640,
		},
		{
			name: "output sniffed only: In fully degraded to Fresh",
			u:    TokenUsage{Out: 300},
			inOK: false, outOK: true, inEst: 700,
			want:    Counters{Fresh: 700, Out: 300},
			wantEst: 700,
		},
		{
			name: "both degraded: In to Fresh, Out max'd with placeholder",
			u:    TokenUsage{Out: 1},
			inOK: false, outOK: false, inEst: 700, outEst: 640,
			want:    Counters{Fresh: 700, Out: 640},
			wantEst: 700 + 640,
		},
		{
			name: "both degraded, placeholder beats the byte estimate",
			u:    TokenUsage{Out: 900},
			inOK: false, outOK: false, inEst: 700, outEst: 640,
			want:    Counters{Fresh: 700, Out: 900},
			wantEst: 700 + 900,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, est := TokenCountersSides(c.u, c.inOK, c.outOK, c.inEst, c.outEst)
			if got != c.want {
				t.Errorf("counters = %+v, want %+v", got, c.want)
			}
			if est != c.wantEst {
				t.Errorf("estimated = %v, want %v", est, c.wantEst)
			}
		})
	}
}
