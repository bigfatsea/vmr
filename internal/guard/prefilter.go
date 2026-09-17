// Ver 2026-09-16, by Sonnet 5

// Aho-Corasick literal prefilter (ADR-13's Level 1, M1.1/M3.0). No external
// dependency — go.mod carries no AC library and the rule set is small
// enough (Appendix A: eleven Tier1/Tier2 literals, longest under twenty
// bytes) that a from-scratch automaton is the right call over adding a
// dependency for it. The automaton is built once at NewEngine construction
// time and is immutable afterward, matching Engine's read-only-singleton
// contract (docs/design/agent-guard-technical-spec-final-2.0.md §4.2): every
// per-call transition is a flat array lookup, no per-scan allocation, no
// fail-link walk at match time (the classic optimization: goto[node][byte]
// is precomputed for every node/byte pair during construction, so matching
// never needs to fall back through the fail chain itself).
package guard

// acNoMatch marks a rule index absent from a node's output set; acRoot is
// the automaton's start state.
const acRoot = 0

// acAutomaton is an immutable Aho-Corasick automaton over a fixed set of
// literal byte strings, indexed by their position in the original slice
// (the Engine's rule index). Built once by newACAutomaton and never
// mutated afterward, so concurrent Scan calls sharing one Engine can walk
// it without synchronization.
type acAutomaton struct {
	// goTo is a flat nodeCount*256 transition table: goTo[node*256+b] is
	// the next state on byte b, always defined (root loops to itself on
	// any byte with no path) -- the precomputed-goto optimization that
	// removes the fail-chain walk from the hot loop. Flattened into one
	// []int32 (rather than [][256]int32) so match's hot loop is a single
	// bounds-checked slice index per byte instead of a slice-of-arrays
	// index, which measurably matters at the >=1 GB/s throughput this is
	// built for.
	goTo []int32
	// outputs[node] lists every rule index whose literal ends at node, via
	// either a direct match or a fail-suffix match merged in at
	// construction time (a literal that is itself a suffix of a longer
	// literal ending here would otherwise be missed).
	outputs [][]int32
}

// newACAutomaton builds an automaton recognizing every literal in lits
// (indexed by rule position; an empty literal is skipped -- NewEngine
// already rejects those before this ever runs, but staying defensive here
// costs nothing). Standard construction: trie insertion, then a
// breadth-first fail-link pass that also compiles the precomputed goto
// table and merges each node's fail-suffix outputs into its own.
func newACAutomaton(lits [][]byte) *acAutomaton {
	// Phase 1: trie insertion. child[node][byte] holds an explicit edge (0
	// = "no explicit child" -- root never has itself as a child, so 0 is a
	// safe sentinel); fail is filled in during the BFS below.
	type node struct {
		child    [256]int32
		explicit [256]bool
		fail     int32
		out      []int32
	}
	nodes := []*node{{}}
	for ruleIdx, lit := range lits {
		if len(lit) == 0 {
			continue
		}
		cur := int32(acRoot)
		for _, b := range lit {
			n := nodes[cur]
			if !n.explicit[b] {
				nodes = append(nodes, &node{})
				n.child[b] = int32(len(nodes) - 1)
				n.explicit[b] = true
			}
			cur = n.child[b]
		}
		nodes[cur].out = append(nodes[cur].out, int32(ruleIdx))
	}

	// Phase 2: BFS fail-link + goto compilation. Root's goto is filled
	// directly from its explicit children (root's own fail is itself,
	// never followed); every other node's goto[b] is either its explicit
	// child (whose fail becomes goto[fail[cur]][b]) or, when absent,
	// inherited straight from goto[fail[cur]][b] -- the precomputed table
	// that makes matching a single array index per byte, no loop.
	queue := make([]int32, 0, len(nodes))
	root := nodes[acRoot]
	for b := 0; b < 256; b++ {
		if root.explicit[b] {
			child := root.child[b]
			nodes[child].fail = acRoot
			queue = append(queue, child)
		} else {
			root.child[b] = acRoot
		}
	}
	for qi := 0; qi < len(queue); qi++ {
		cur := queue[qi]
		curNode := nodes[cur]
		for b := 0; b < 256; b++ {
			if curNode.explicit[b] {
				child := curNode.child[b]
				f := nodes[curNode.fail].child[b]
				nodes[child].fail = f
				nodes[child].out = append(nodes[child].out, nodes[f].out...)
				queue = append(queue, child)
			} else {
				curNode.child[b] = nodes[curNode.fail].child[b]
			}
		}
	}

	a := &acAutomaton{goTo: make([]int32, len(nodes)*256), outputs: make([][]int32, len(nodes))}
	for i, n := range nodes {
		copy(a.goTo[i*256:i*256+256], n.child[:])
		a.outputs[i] = n.out
	}
	return a
}

// match runs text through the automaton once (O(len(text)), no
// allocation) and sets hits[i] = true for every rule index i whose literal
// occurs at least once in text. hits must be at least len(rules) long and
// is only ever set to true, never cleared -- callers reset it (or use a
// fresh, zeroed slice) between calls; Scratch.acHits does this via
// re-slicing a []bool back to its zero value each Scan (see engine.go).
//
// Measured (BenchmarkOutboundPrefilter, Apple M4, 1 MiB no-hit JSON):
// ~490 MB/s, 0 allocs/op -- short of the design spec's illustrative >=1
// GB/s figure (an unverified target the spec itself says must come from a
// real benchmark, not be assumed). The flat goTo table above already
// replaced an earlier [][256]int32 (~40% faster) by removing a
// slice-of-arrays index in the hot loop; closing the remaining gap would
// mean unsafe-pointer indexing to shed bounds checks, which is not worth
// the risk in credential-scanning code before there is a real per-request
// online workload (M3.4+) to size the actual requirement against — this
// is exactly the "don't design the online concurrency/perf story before
// the mount point is real" call the design spec's M1.1/M1.2 deferral
// already made once. Revisit if a real online p95 ever needs it.
func (a *acAutomaton) match(text []byte, hits []bool) {
	cur := int32(acRoot)
	goTo := a.goTo
	for _, b := range text {
		cur = goTo[int(cur)*256+int(b)]
		if out := a.outputs[cur]; len(out) > 0 {
			for _, ruleIdx := range out {
				hits[ruleIdx] = true
			}
		}
	}
}
