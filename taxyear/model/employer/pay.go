package employer

import (
	"sort"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

// line is one pay line. Unset differs from zero, as on the proto.
type line struct {
	v   money.USCents
	set bool
}

func setLine(v money.USCents) line { return line{v: v, set: true} }

func lineOf(m *commonv1.Money) line {
	v, ok := model.Cents(m)
	return line{v: v, set: ok}
}

func (l line) plus(o line) line  { return line{v: l.v + o.v, set: l.set || o.set} }
func (l line) minus(o line) line { return line{v: l.v - o.v, set: l.set || o.set} }
func (l line) money() *commonv1.Money {
	return model.MoneyIfSet(l.v, l.set)
}

// stateLine is one state's wages and tax withheld.
type stateLine struct {
	state    string
	wages    line
	withheld line
}

// pretaxKinds is the order pre-tax lines are written in.
var pretaxKinds = []factsv1.PretaxKind{
	factsv1.PretaxKind_PRETAX_KIND_401K,
	factsv1.PretaxKind_PRETAX_KIND_403B,
	factsv1.PretaxKind_PRETAX_KIND_HSA,
	factsv1.PretaxKind_PRETAX_KIND_OTHER_BENEFITS,
}

// pay is one paycheck's figures, or a sum of them.
type pay struct {
	gross    line
	pretax   map[factsv1.PretaxKind]line
	federal  line
	ss       line
	medicare line
	roth401k line
	roth403b line
	net      line
	states   []stateLine
}

func payOf(f *factsv1.PayFigures) pay {
	p := pay{
		gross:    lineOf(f.GetGross()),
		federal:  lineOf(f.GetFederalWithheld()),
		ss:       lineOf(f.GetSocialSecurityWithheld()),
		medicare: lineOf(f.GetMedicareWithheld()),
		roth401k: lineOf(f.GetRoth_401K()),
		roth403b: lineOf(f.GetRoth_403B()),
		net:      lineOf(f.GetNet()),
	}
	for _, x := range f.GetPretax() {
		p.setPretax(x.GetKind(), p.pretaxLine(x.GetKind()).plus(lineOf(x.GetAmount())))
	}
	for _, s := range f.GetStates() {
		i := p.stateIndex(s.GetState())
		p.states[i].wages = p.states[i].wages.plus(lineOf(s.GetStateIncome()))
		p.states[i].withheld = p.states[i].withheld.plus(lineOf(s.GetTaxWithheld()))
	}
	return p
}

func (p pay) clone() pay {
	out := p
	out.pretax = map[factsv1.PretaxKind]line{}
	for k, v := range p.pretax {
		out.pretax[k] = v
	}
	out.states = append([]stateLine(nil), p.states...)
	return out
}

func (p pay) pretaxLine(k factsv1.PretaxKind) line { return p.pretax[k] }

func (p *pay) setPretax(k factsv1.PretaxKind, l line) {
	if p.pretax == nil {
		p.pretax = map[factsv1.PretaxKind]line{}
	}
	p.pretax[k] = l
}

// stateIndex returns the index of the state's line, adding an unset one.
func (p *pay) stateIndex(state string) int {
	for i, s := range p.states {
		if s.state == state {
			return i
		}
	}
	p.states = append(p.states, stateLine{state: state})
	return len(p.states) - 1
}

func (p pay) state(state string) (stateLine, bool) {
	for _, s := range p.states {
		if s.state == state {
			return s, true
		}
	}
	return stateLine{}, false
}

// pretaxTotal is every pre-tax line.
func (p pay) pretaxTotal() money.USCents {
	var t money.USCents
	for _, l := range p.pretax {
		t += l.v
	}
	return t
}

// taxable is federal taxable wages, W-2 box 1's terms: gross less every
// pre-tax line.
func (p pay) taxable() money.USCents { return p.gross.v - p.pretaxTotal() }

// fica is Social Security and Medicare wages before the cap: gross less HSA
// and other benefits.
func (p pay) fica() money.USCents {
	return p.gross.v - p.pretax[factsv1.PretaxKind_PRETAX_KIND_HSA].v - p.pretax[factsv1.PretaxKind_PRETAX_KIND_OTHER_BENEFITS].v
}

// deferrals is the workplace plan's pre-tax and Roth lines.
func (p pay) deferrals() money.USCents {
	return p.pretax[factsv1.PretaxKind_PRETAX_KIND_401K].v + p.pretax[factsv1.PretaxKind_PRETAX_KIND_403B].v + p.roth401k.v + p.roth403b.v
}

func (p pay) plus(o pay) pay {
	out := p.clone()
	out.gross = out.gross.plus(o.gross)
	for k, v := range o.pretax {
		out.setPretax(k, out.pretax[k].plus(v))
	}
	out.federal = out.federal.plus(o.federal)
	out.ss = out.ss.plus(o.ss)
	out.medicare = out.medicare.plus(o.medicare)
	out.roth401k = out.roth401k.plus(o.roth401k)
	out.roth403b = out.roth403b.plus(o.roth403b)
	out.net = out.net.plus(o.net)
	for _, s := range o.states {
		i := out.stateIndex(s.state)
		out.states[i].wages = out.states[i].wages.plus(s.wages)
		out.states[i].withheld = out.states[i].withheld.plus(s.withheld)
	}
	return out
}

func (p pay) negate() pay {
	out := pay{
		gross: line{-p.gross.v, p.gross.set}, federal: line{-p.federal.v, p.federal.set},
		ss: line{-p.ss.v, p.ss.set}, medicare: line{-p.medicare.v, p.medicare.set},
		roth401k: line{-p.roth401k.v, p.roth401k.set}, roth403b: line{-p.roth403b.v, p.roth403b.set},
		net: line{-p.net.v, p.net.set},
	}
	for k, v := range p.pretax {
		out.setPretax(k, line{-v.v, v.set})
	}
	for _, s := range p.states {
		out.states = append(out.states, stateLine{s.state, line{-s.wages.v, s.wages.set}, line{-s.withheld.v, s.withheld.set}})
	}
	return out
}

func (p pay) minus(o pay) pay { return p.plus(o.negate()) }

// nonzero keeps only the lines with a nonzero amount.
func (p pay) nonzero() pay {
	nz := func(l line) line {
		if l.v == 0 {
			return line{}
		}
		return l
	}
	out := pay{gross: nz(p.gross), federal: nz(p.federal), ss: nz(p.ss), medicare: nz(p.medicare),
		roth401k: nz(p.roth401k), roth403b: nz(p.roth403b), net: nz(p.net)}
	for k, v := range p.pretax {
		if v.v != 0 {
			out.setPretax(k, v)
		}
	}
	for _, s := range p.states {
		if s.wages.v != 0 || s.withheld.v != 0 {
			out.states = append(out.states, stateLine{s.state, nz(s.wages), nz(s.withheld)})
		}
	}
	return out
}

// empty reports whether no line is set.
func (p pay) empty() bool {
	n := p.nonzero()
	return !n.gross.set && !n.federal.set && !n.ss.set && !n.medicare.set && !n.roth401k.set &&
		!n.roth403b.set && !n.net.set && len(n.pretax) == 0 && len(n.states) == 0
}

// proto writes the figures, unset lines left unset, pre-tax lines in kind
// order and states by postal code.
func (p pay) proto() *factsv1.PayFigures {
	out := &factsv1.PayFigures{
		Gross:                  p.gross.money(),
		FederalWithheld:        p.federal.money(),
		SocialSecurityWithheld: p.ss.money(),
		MedicareWithheld:       p.medicare.money(),
		Roth_401K:              p.roth401k.money(),
		Roth_403B:              p.roth403b.money(),
		Net:                    p.net.money(),
	}
	for _, k := range pretaxKinds {
		if l, ok := p.pretax[k]; ok && l.set {
			out.Pretax = append(out.Pretax, &factsv1.PayFigures_Pretax{Kind: k, Amount: l.money()})
		}
	}
	states := append([]stateLine(nil), p.states...)
	sort.Slice(states, func(i, j int) bool { return states[i].state < states[j].state })
	for _, s := range states {
		if !s.wages.set && !s.withheld.set {
			continue
		}
		out.States = append(out.States, &commonv1.StateTaxWithholding{State: s.state, StateIncome: s.wages.money(), TaxWithheld: s.withheld.money()})
	}
	return out
}

// lineSum adds every line, for figures that hold one.
func (p pay) lineSum() money.USCents {
	t := p.gross.v + p.federal.v + p.ss.v + p.medicare.v + p.roth401k.v + p.roth403b.v + p.net.v + p.pretaxTotal()
	for _, s := range p.states {
		t += s.wages.v + s.withheld.v
	}
	return t
}

// sumPay adds figures.
func sumPay(ps []pay) pay {
	var t pay
	for _, p := range ps {
		t = t.plus(p)
	}
	return t
}
