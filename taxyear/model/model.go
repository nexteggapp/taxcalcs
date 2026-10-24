// Package model holds what every model's expansion shares: the year being
// computed, money and date conversions, item ids, the state lived in on a
// date, and questions.
//
// Each model's own rules are in a package beneath this one, such as
// model/employer. See docs/models.md.
package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// Context is the year a scenario is computed for: its tax year, as_of and
// facts. It is read-only once made.
type Context struct {
	year  int
	asOf  civil.Date
	facts *factsv1.TaxYearFacts
}

// NewContext returns the context for a scenario. as_of must be a real day;
// facts may be nil, which reads as a household with no facts.
func NewContext(year int, asOf *commonv1.Date, facts *factsv1.TaxYearFacts) (*Context, error) {
	if year < 1900 || year > 9999 {
		return nil, fmt.Errorf("model: tax year %d is not a year", year)
	}
	d, err := civil.FromProto(asOf)
	if err != nil {
		return nil, fmt.Errorf("model: as_of: %w", err)
	}
	if d.IsZero() {
		return nil, errors.New("model: as_of is not set")
	}
	if facts == nil {
		facts = &factsv1.TaxYearFacts{}
	}
	return &Context{year: year, asOf: d, facts: facts}, nil
}

// Year is the tax year.
func (c *Context) Year() int { return c.year }

// AsOf is the day the year is computed as of. Anything dated after it is the
// rest of the year.
func (c *Context) AsOf() civil.Date { return c.asOf }

// Facts is the household's facts. Callers must not change it.
func (c *Context) Facts() *factsv1.TaxYearFacts { return c.facts }

// YearStart is January 1 of the tax year.
func (c *Context) YearStart() civil.Date { return civil.Must(c.year, time.January, 1) }

// YearEnd is December 31 of the tax year.
func (c *Context) YearEnd() civil.Date { return civil.Must(c.year, time.December, 31) }

// Finished reports whether as_of is after the tax year.
func (c *Context) Finished() bool { return c.asOf.After(c.YearEnd()) }

// Person returns the person with the id, or nil.
func (c *Context) Person(id string) *factsv1.Person {
	for _, p := range c.facts.GetPeople() {
		if p.GetId() == id {
			return p
		}
	}
	return nil
}

// AgeAtYearEnd returns a person's age on December 31 of the tax year, and
// false when their date of birth is not given.
func (c *Context) AgeAtYearEnd(person string) (int, bool) {
	p := c.Person(person)
	if p.GetDateOfBirth().GetYear() == 0 {
		return 0, false
	}
	return c.year - int(p.GetDateOfBirth().GetYear()), true
}

// LivedIn returns the state the household lived in on d: the period that
// holds it; in a gap, the period before; before every period, the first.
// It returns "" when the facts give no residence.
func (c *Context) LivedIn(d civil.Date) string {
	type span struct {
		state    string
		from, to civil.Date
	}
	var spans []span
	for _, r := range c.facts.GetResidence() {
		from, err1 := civil.FromProto(r.GetFrom())
		to, err2 := civil.FromProto(r.GetTo())
		if err1 != nil || err2 != nil {
			continue
		}
		if from.IsZero() {
			from = c.YearStart()
		}
		spans = append(spans, span{r.GetState(), from, to})
	}
	if len(spans) == 0 {
		return ""
	}
	sort.SliceStable(spans, func(i, j int) bool { return spans[i].from.Before(spans[j].from) })
	state := spans[0].state
	for _, s := range spans {
		if s.from.After(d) {
			break
		}
		state = s.state
		if s.to.IsZero() || !d.After(s.to) {
			return s.state
		}
	}
	return state
}

// Cents returns a Money's amount and whether it is set. A Money in any
// currency but USD, or none, is read as USD: every form here is filed in it.
func Cents(m *commonv1.Money) (money.USCents, bool) {
	if m == nil {
		return 0, false
	}
	return money.USCents(m.GetMinor()), true
}

// Value returns a Money's amount, zero when unset.
func Value(m *commonv1.Money) money.USCents {
	v, _ := Cents(m)
	return v
}

// Money returns the amount as a USD Money.
func Money(c money.USCents) *commonv1.Money {
	return &commonv1.Money{Minor: int64(c), Currency: "USD"}
}

// MoneyIfSet returns the amount as a Money when set, else nil.
func MoneyIfSet(c money.USCents, set bool) *commonv1.Money {
	if !set {
		return nil
	}
	return Money(c)
}

// ItemID joins the parts of an item's id: the model's list and id, then the
// date, kind and any evidence or part id, such as
// "employers/acme/2026-10-15/regular".
func ItemID(parts ...string) string { return strings.Join(parts, "/") }

// Share divides total into n parts that add back to it exactly, the first
// parts taking any remainder a cent each. n must be positive.
func Share(total money.USCents, n int) []money.USCents {
	out := make([]money.USCents, n)
	each := total / money.USCents(n)
	rem := total - each*money.USCents(n)
	step := money.USCents(1)
	if rem < 0 {
		step, rem = -1, -rem
	}
	for i := range out {
		out[i] = each
		if money.USCents(i) < rem {
			out[i] += step
		}
	}
	return out
}

// Scale returns v × num / den, rounded half away from zero, or v when den is
// zero.
func Scale(v, num, den money.USCents) money.USCents {
	if den == 0 {
		return v
	}
	p := int64(v) * int64(num)
	d := int64(den)
	if (p < 0) != (d < 0) {
		return money.USCents(-((abs(p) + abs(d)/2) / abs(d)))
	}
	return money.USCents((abs(p) + abs(d)/2) / abs(d))
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// Dollars rounds an amount to whole dollars, half away from zero, as a
// figure made from cents is rounded once.
func Dollars(c money.USCents) money.USCents {
	if c < 0 {
		return -Dollars(-c)
	}
	return (c + 50) / 100 * 100
}

// Usable reports whether a document counts: not removed by the household.
func Usable(ref *factsv1.DocumentRef) bool { return !ref.GetRemoved() }

// SortItems orders items by date, then id, as every projection lists them.
func SortItems(items []*scenariosv1.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].GetDate(), items[j].GetDate()
		if c := compareDates(a, b); c != 0 {
			return c < 0
		}
		return items[i].GetId() < items[j].GetId()
	})
}

func compareDates(a, b *commonv1.Date) int {
	for _, p := range [][2]int32{{a.GetYear(), b.GetYear()}, {a.GetMonth(), b.GetMonth()}, {a.GetDay(), b.GetDay()}} {
		if p[0] != p[1] {
			if p[0] < p[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}
