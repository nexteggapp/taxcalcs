package taxyear

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
	"sync"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
)

// Cache keeps work between calls to its Compute, so an edit recomputes
// only what it changed. It holds the checks on each file's shared inputs,
// and each scenario's resolved inputs and result.
//
// A scenario is keyed by a hash of its parent's key and the scenario
// itself: its id, name, parent and changes. The chain's root is a hash of
// the file's year, as_of and shared inputs. Two scenarios with one key have
// the same resolved inputs and the same result. An edit to one scenario
// therefore recomputes that scenario and its descendants, and an edit to
// shared, the year or as_of recomputes every scenario.
//
// The cache keeps the entries used most recently, up to its size, and is
// safe for concurrent use. Everything in it is a copy, so a caller may
// change a file after computing it.
type Cache struct {
	mu        sync.Mutex
	size      int
	clock     uint64
	shared    map[key]*sharedEntry
	scenarios map[key]*scenarioEntry
	models    map[key]*modelEntry
	checks    map[key]*checkEntry
	keys      map[keyRef]*keyEntry
}

// key is a SHA-256 hash.
type key [sha256.Size]byte

// sharedEntry is the work on one file's shared inputs.
type sharedEntry struct {
	used        uint64
	inputs      *scenariosv1.ScenarioInputs // a copy of shared
	identifiers []*scenariosv1.Problem      // identifiers under shared
	ids         []*scenariosv1.Problem      // CheckIDs
	checks      sharedChecks
}

// scenarioEntry is one scenario's resolved inputs and result.
type scenarioEntry struct {
	used     uint64
	inputs   *scenariosv1.ScenarioInputs // nil when its changes don't apply
	problems []*scenariosv1.Problem      // its changes failing, or CheckScenarios
	result   *scenariosv1.ScenarioResult // nil until computed
}

// NewCache returns a cache that keeps up to size scenarios, and as many
// files' shared inputs. It refuses a size under 1.
func NewCache(size int) (*Cache, error) {
	if size < 1 {
		return nil, errors.New("taxyear: a cache must hold at least one scenario")
	}
	return &Cache{size: size, shared: map[key]*sharedEntry{}, scenarios: map[key]*scenarioEntry{}, models: map[key]*modelEntry{},
		checks: map[key]*checkEntry{}, keys: map[keyRef]*keyEntry{}}, nil
}

// Compute is [Compute], reusing the cache's work and adding to it. The
// result is the same as Compute's for the same file.
func (c *Cache) Compute(file *scenariosv1.TaxYearScenarios) (Result, error) {
	if file == nil {
		return Result{}, errors.New("taxyear: no file")
	}
	defer c.trim()
	return compute(file, c), nil
}

// hash is the SHA-256 of a message's deterministic encoding after a
// prefix. A message that can't be encoded hashes its error, so it matches
// nothing else and is recomputed each time.
func hash(prefix []byte, m proto.Message) key {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		b = []byte(err.Error())
	}
	h := sha256.New()
	h.Write(prefix)
	h.Write(b)
	var out key
	h.Sum(out[:0])
	return out
}

// rootKey keys a file's shared inputs, with its year and as_of.
func rootKey(file *scenariosv1.TaxYearScenarios) key {
	prefix := binary.AppendVarint([]byte("taxyear.shared.v1"), int64(file.GetTaxYear()))
	prefix = append(prefix, hashBytes(file.GetAsOf())...)
	return hash(prefix, file.GetShared())
}

// scenarioKey keys a scenario under its parent's key.
func scenarioKey(parent key, s *scenariosv1.Scenario) key {
	return hash(append([]byte("taxyear.scenario.v1"), parent[:]...), s)
}

func hashBytes(m proto.Message) []byte {
	k := hash(nil, m)
	return k[:]
}

// sharedFor returns the work on a file's shared inputs, doing it when the
// cache doesn't hold it.
func (c *Cache) sharedFor(file *scenariosv1.TaxYearScenarios) (key, *sharedEntry) {
	k := rootKey(file)
	c.mu.Lock()
	e, ok := c.shared[k]
	if ok {
		c.clock++
		e.used = c.clock
	}
	c.mu.Unlock()
	if ok {
		return k, e
	}
	in := proto.Clone(file.GetShared()).(*scenariosv1.ScenarioInputs)
	if in == nil {
		in = &scenariosv1.ScenarioInputs{}
	}
	e = &sharedEntry{inputs: in, ids: CheckIDs(in), checks: checkShared(in)}
	identifiers("shared", in.ProtoReflect(), &e.identifiers)
	c.mu.Lock()
	c.clock++
	e.used = c.clock
	c.shared[k] = e
	c.mu.Unlock()
	return k, e
}

// scenarioFor returns a scenario's entry, resolving and checking it when
// the cache doesn't hold it.
func (c *Cache) scenarioFor(k key, parent *scenariosv1.ScenarioInputs, s *scenariosv1.Scenario, shared *sharedEntry) *scenarioEntry {
	c.mu.Lock()
	e, ok := c.scenarios[k]
	c.clock++
	if ok {
		e.used = c.clock
	}
	c.mu.Unlock()
	if ok {
		return e
	}
	e = &scenarioEntry{}
	// apply may keep an added item rather than copy it, and the cache
	// keeps the inputs, so they take a copy of the caller's changes.
	in, err := ApplyChanges(parent, clone(s.GetChanges()))
	if err != nil {
		e.problems = []*scenariosv1.Problem{problem("scenarios["+s.GetId()+"]", changesFailed(err))}
	} else {
		e.inputs = in
		e.problems = shared.checks.scenario(in)
	}
	c.mu.Lock()
	c.clock++
	e.used = c.clock
	c.scenarios[k] = e
	c.mu.Unlock()
	return e
}

// computed returns a scenario's result, computing it once.
func (c *Cache) computed(file *scenariosv1.TaxYearScenarios, e *scenarioEntry, id string) *scenariosv1.ScenarioResult {
	c.mu.Lock()
	r := e.result
	c.mu.Unlock()
	if r != nil {
		return r
	}
	r = &scenariosv1.ScenarioResult{Scenario: id, Problems: append([]*scenariosv1.Problem(nil), e.problems...)}
	if e.inputs != nil && len(r.Problems) == 0 {
		computeScenario(c, file, e.inputs, r)
	}
	c.mu.Lock()
	if e.result == nil {
		e.result = r
	}
	r = e.result
	c.mu.Unlock()
	return r
}

// trim drops the least recently used entries past the cache's size.
func (c *Cache) trim() {
	c.mu.Lock()
	defer c.mu.Unlock()
	trimMap(c.scenarios, c.size, func(e *scenarioEntry) uint64 { return e.used })
	trimMap(c.shared, c.size, func(e *sharedEntry) uint64 { return e.used })
	trimMap(c.models, 16*c.size, func(e *modelEntry) uint64 { return e.used })
	trimMap(c.checks, 16*c.size, func(e *checkEntry) uint64 { return e.used })
	trimMap(c.keys, 64*c.size, func(e *keyEntry) uint64 { return e.used })
}

func trimMap[K comparable, E any](m map[K]E, size int, used func(E) uint64) {
	if len(m) <= size {
		return
	}
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return used(m[keys[i]]) < used(m[keys[j]]) })
	for _, k := range keys[:len(m)-size] {
		delete(m, k)
	}
}
