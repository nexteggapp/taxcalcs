package employer

import (
	"fmt"
	"sort"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// each calls f with every set line of the pay and a name for it, and
// writes back what f leaves in it.
func (p *pay) each(f func(name string, l *line)) {
	for _, x := range []struct {
		name string
		l    *line
	}{{"gross", &p.gross}, {"federal", &p.federal}, {"ss", &p.ss}, {"medicare", &p.medicare},
		{"roth401k", &p.roth401k}, {"roth403b", &p.roth403b}, {"net", &p.net}} {
		if x.l.set {
			f(x.name, x.l)
		}
	}
	kinds := make([]int, 0, len(p.pretax))
	for k := range p.pretax {
		kinds = append(kinds, int(k))
	}
	sort.Ints(kinds)
	for _, k := range kinds {
		l := p.pretax[factsv1.PretaxKind(k)]
		if l.set {
			f(fmt.Sprint("pretax", k), &l)
			p.pretax[factsv1.PretaxKind(k)] = l
		}
	}
	for i := range p.states {
		s := &p.states[i]
		if s.wages.set {
			f("wages."+s.state, &s.wages)
		}
		if s.withheld.set {
			f("withheld."+s.state, &s.withheld)
		}
	}
}

// roundItems rounds each item's lines to the dollar with a running total
// per line in each part's run (see [model.RoundItems]): each total then
// adds the displayed items, and a part's line is its own exact sum
// rounded.
func (j *Job) roundItems() {
	runs := model.Runs{}
	for _, d := range j.items {
		r := runs.For(d.status == scenariosv1.ItemStatus_ITEM_STATUS_MADE, d.origin.GetPart())
		d.p.each(func(name string, l *line) { l.v = r.Round(name, l.v) })
		if d.takeHome.set {
			d.takeHome.v = r.Round("take_home", d.takeHome.v)
		}
	}
}
