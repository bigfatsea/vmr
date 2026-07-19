// Ver 2026-07-07 01:55, by Fable 5

// Package strategy implements scheduling as filter + stable multi-key sort.
// Every scheduling behavior (priority, weight, round_robin, …) is just a
// Dimension; combining them is list concatenation in config, and the router's
// main loop never changes.
package strategy

import (
	"cmp"
	"fmt"
	"sort"
	"sync"

	"vmr/internal/core"
)

// Dimension is one comparable sort key. Stateful dimensions (round_robin)
// manage their own state inside the instance returned by their factory.
type Dimension interface {
	Name() string
	Compare(a, b *core.Endpoint) int // <0: a first; 0: tie, defer to next dimension
}

var (
	mu        sync.RWMutex
	factories = map[string]func() Dimension{}
)

func Register(name string, f func() Dimension) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := factories[name]; dup {
		panic(fmt.Sprintf("strategy: Register called twice for %q", name))
	}
	factories[name] = f
}

// Build instantiates the dimension chain for one virtual model.
func Build(names []string) ([]Dimension, error) {
	mu.RLock()
	defer mu.RUnlock()
	dims := make([]Dimension, 0, len(names))
	for _, n := range names {
		f, ok := factories[n]
		if !ok {
			return nil, fmt.Errorf("unknown strategy dimension %q", n)
		}
		dims = append(dims, f())
	}
	return dims, nil
}

// Sort orders endpoints by the dimension chain. The sort is stable, so full
// ties keep config-file order.
func Sort(eps []*core.Endpoint, dims []Dimension) {
	sort.SliceStable(eps, func(i, j int) bool {
		for _, d := range dims {
			if c := d.Compare(eps[i], eps[j]); c != 0 {
				return c < 0
			}
		}
		return false
	})
}

func init() {
	Register("priority", func() Dimension { return priority{} })
}

// priority: lower number wins; ties fall through to the next dimension.
type priority struct{}

func (priority) Name() string { return "priority" }
func (priority) Compare(a, b *core.Endpoint) int {
	return cmp.Compare(a.Priority, b.Priority)
}
