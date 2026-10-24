package taxyear

import (
	"encoding/binary"
	"fmt"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/account"
	"github.com/nexteggapp/taxcalcs/taxyear/model/broker"
	"github.com/nexteggapp/taxcalcs/taxyear/model/business"
	"github.com/nexteggapp/taxcalcs/taxyear/model/itemised"
	"github.com/nexteggapp/taxcalcs/taxyear/model/otherincome"
	"github.com/nexteggapp/taxcalcs/taxyear/model/retirement"
)

// A model is expanded on its own unless it is a job, whose withholding and
// plan contributions depend on the household's retirement models, or tax
// paid, which the estimated-payment rules fill. So each other model's
// finished projection, its notes and its workings depend only on the model
// and the scenario's year, as_of and facts, and a [Cache] keeps them by a
// hash of those: an edit to one job re-expands no charity.

// modelEntry is one model's finished projection: expanded, its boxes
// rounded, its can_add filled, with its workings. Nothing in it is changed
// once it is made.
type modelEntry struct {
	used    uint64
	proj    proto.Message
	notes   *model.Notes
	working []*scenariosv1.Working
}

// modelSet finds and keeps one scenario's models' entries.
type modelSet struct {
	c   *Cache // nil: nothing is kept
	ctx *model.Context
	key key // the year, as_of and facts
	// working is each kept model's workings, by its projection path.
	working map[string][]*scenariosv1.Working
	// overrides are the scenario's overrides.
	overrides []*scenariosv1.Override
}

func newModels(c *Cache, ctx *model.Context, in *scenariosv1.ScenarioInputs) *modelSet {
	ms := &modelSet{c: c, ctx: ctx, working: map[string][]*scenariosv1.Working{}, overrides: in.GetOverrides()}
	prefix := binary.AppendVarint([]byte("taxyear.models.v1"), int64(ctx.Year()))
	prefix = append(prefix, hashBytes(ctx.AsOf().Proto())...)
	ms.key = ms.keyOf(string(prefix), in.GetFacts())
	return ms
}

// keyOf hashes a message after a prefix and the scenario's key. The cache
// holds resolved inputs that are never changed, and shares a message an
// edit leaves alone between a scenario's inputs before and after it, so a
// message it has hashed before is found by identity, not hashed again.
func (ms *modelSet) keyOf(prefix string, m proto.Message) key {
	k, _ := ms.keyOr(prefix, m, true)
	return k
}

// keyOr is keyOf, and with byContent false, a message not found by
// identity is not hashed: false says there is no key.
func (ms *modelSet) keyOr(prefix string, m proto.Message, byContent bool) (key, bool) {
	ref := keyRef{m: m, prefix: prefix, scenario: ms.key}
	if k, ok := ms.c.keyFor(ref); ok {
		return k, true
	}
	if !byContent {
		return key{}, false
	}
	k := hash(append([]byte(prefix+"\x00"), ms.key[:]...), m)
	ms.c.keepKey(ref, k)
	return k, true
}

// keyRef is a message hashed under a prefix and a scenario's key.
type keyRef struct {
	m        proto.Message
	prefix   string
	scenario key
}

// checked returns a model's own checks, running them with make when the
// cache doesn't hold them. A job is the model an edit most often changes,
// and the largest, so one the cache doesn't hold by identity is checked
// rather than hashed, and kept under a key of its own.
func (ms *modelSet) checked(s sourced, make func() *checkEntry) *checkEntry {
	if ms.c == nil {
		return make()
	}
	prefix := "check." + s.list
	k, ok := ms.keyOr(prefix, s.top.Interface(), s.list != "employers")
	if !ok {
		k = ms.c.fresh()
		ms.c.keepKey(keyRef{m: s.top.Interface(), prefix: prefix, scenario: ms.key}, k)
	}
	ms.c.mu.Lock()
	e := ms.c.checks[k]
	ms.c.mu.Unlock()
	if e == nil {
		e = make()
	}
	ms.c.mu.Lock()
	defer ms.c.mu.Unlock()
	ms.c.clock++
	e.used = ms.c.clock
	ms.c.checks[k] = e
	return e
}

// entry returns a model's entry, finishing it with make when the cache
// doesn't hold it.
func (ms *modelSet) entry(list, id string, m proto.Message, make func() (proto.Message, *model.Notes, error)) (*modelEntry, error) {
	at := fmt.Sprintf("projection.%s[%s]", list, id)
	overrides := overridesAt(ms.overrides, at)
	var k key
	if ms.c != nil {
		k = ms.keyOf(list, m)
		if len(overrides) > 0 {
			k = hash(append([]byte("overrides\x00"), k[:]...), &scenariosv1.ScenarioInputs{Overrides: overrides})
		}
		if e := ms.c.model(k); e != nil {
			ms.working[at] = e.working
			return e, nil
		}
	}
	p, notes, err := make()
	if err != nil {
		return nil, err
	}
	if notes == nil {
		notes = &model.Notes{}
	}
	working := modelWorkings(at, p)
	if len(overrides) > 0 {
		var ow []*scenariosv1.Working
		if p, ow, err = overridden(list, at, p, overrides); err != nil {
			return nil, err
		}
		working = withOverrideWorkings(modelWorkings(at, p), ow)
	}
	e := &modelEntry{proj: p, notes: notes, working: working}
	if ms.c != nil {
		ms.c.keepModel(k, e)
	}
	ms.working[at] = e.working
	return e, nil
}

// expandOthers expands every model but the jobs and tax paid, each through
// the cache.
func (ms *modelSet) expandOthers(m *scenariosv1.TaxYearModel, proj *scenariosv1.YearProjection, notes *model.Notes) error {
	ctx := ms.ctx
	for _, b := range m.GetBusinesses() {
		e, err := ms.entry("businesses", b.GetId(), b, func() (proto.Message, *model.Notes, error) {
			p, n, err := business.Expand(ctx, b)
			if err == nil {
				periods(p.GetSoFar().GetBoxes(), p.GetRest().GetBoxes(), p.GetYear().GetBoxes())
				p.CanAdd = businessCanAdd(ctx, b, p)
			}
			return p, n, err
		})
		if err != nil {
			return err
		}
		proj.Businesses = append(proj.Businesses, e.proj.(*scenariosv1.BusinessProjection))
		notes.Merge(e.notes)
	}
	for _, a := range m.GetAccounts() {
		e, err := ms.entry("accounts", a.GetId(), a, func() (proto.Message, *model.Notes, error) {
			p, n, err := account.Expand(ctx, a)
			if err == nil {
				periods(p.GetSoFar().GetBoxes(), p.GetRest().GetBoxes(), p.GetYear().GetBoxes())
				p.CanAdd = accountCanAdd(ctx, a, p)
			}
			return p, n, err
		})
		if err != nil {
			return err
		}
		proj.Accounts = append(proj.Accounts, e.proj.(*scenariosv1.AccountProjection))
		notes.Merge(e.notes)
	}
	for _, b := range m.GetBrokers() {
		e, err := ms.entry("brokers", b.GetId(), b, func() (proto.Message, *model.Notes, error) {
			p, n, err := broker.Expand(ctx, b)
			if err == nil {
				p.GetYear().Boxes = brokerYear(p.GetSoFar().GetBoxes(), p.GetRest().GetBoxes())
				p.CanAdd = brokerCanAdd(ctx, b, p)
			}
			return p, n, err
		})
		if err != nil {
			return err
		}
		proj.Brokers = append(proj.Brokers, e.proj.(*scenariosv1.BrokerProjection))
		notes.Merge(e.notes)
	}
	return ms.expandRest(m, proj, notes)
}

func (ms *modelSet) expandRest(m *scenariosv1.TaxYearModel, proj *scenariosv1.YearProjection, notes *model.Notes) error {
	ctx := ms.ctx
	for _, it := range m.GetItemised() {
		e, err := ms.entry("itemised", it.GetId(), it, func() (proto.Message, *model.Notes, error) {
			p, n, err := itemised.Expand(ctx, it)
			if err == nil {
				periods(p.GetSoFar().GetBoxes(), p.GetRest().GetBoxes(), p.GetYear().GetBoxes())
				p.CanAdd = itemisedCanAdd(ctx, it, p)
			}
			return p, n, err
		})
		if err != nil {
			return err
		}
		proj.Itemised = append(proj.Itemised, e.proj.(*scenariosv1.ItemisedProjection))
		notes.Merge(e.notes)
	}
	for _, o := range m.GetOtherIncome() {
		e, err := ms.entry("other_income", o.GetId(), o, func() (proto.Message, *model.Notes, error) {
			p, n, err := otherincome.Expand(ctx, o)
			if err == nil {
				periods(p.GetSoFar().GetBoxes(), p.GetRest().GetBoxes(), p.GetYear().GetBoxes())
				p.CanAdd = otherIncomeCanAdd(ctx, o, p)
			}
			return p, n, err
		})
		if err != nil {
			return err
		}
		proj.OtherIncome = append(proj.OtherIncome, e.proj.(*scenariosv1.OtherIncomeProjection))
		notes.Merge(e.notes)
	}
	return nil
}

// retirement expands one retirement model through the cache.
func (ms *modelSet) retirement(r *scenariosv1.RetirementModel) (*scenariosv1.RetirementProjection, error) {
	e, err := ms.entry("retirement", r.GetId(), r, func() (proto.Message, *model.Notes, error) {
		p, err := retirement.Expand(ms.ctx, r)
		if err == nil {
			p.CanAdd = retirementCanAdd(ms.ctx, r)
		}
		return p, nil, err
	})
	if err != nil {
		return nil, err
	}
	return e.proj.(*scenariosv1.RetirementProjection), nil
}

// model returns a kept model's entry, or nil.
func (c *Cache) model(k key) *modelEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.models[k]
	if e != nil {
		c.clock++
		e.used = c.clock
	}
	return e
}

// keyFor returns the key a message was hashed to, by identity. A nil cache
// holds none.
func (c *Cache) keyFor(ref keyRef) (key, bool) {
	if c == nil {
		return key{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.keys[ref]
	if !ok {
		return key{}, false
	}
	c.clock++
	e.used = c.clock
	return e.key, true
}

// keepKey keeps the key a message was hashed to.
func (c *Cache) keepKey(ref keyRef, k key) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	c.keys[ref] = &keyEntry{used: c.clock, key: k}
}

// keyEntry is a kept key.
type keyEntry struct {
	used uint64
	key  key
}

// fresh returns a key no content hashes to.
func (c *Cache) fresh() key {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	return hash(binary.AppendUvarint([]byte("taxyear.fresh"), c.clock), nil)
}

// keepModel keeps a model's entry.
func (c *Cache) keepModel(k key, e *modelEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clock++
	e.used = c.clock
	c.models[k] = e
}
