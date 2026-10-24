package employer

import (
	"strings"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

// box14Benefits is how a W-2's box 14 names the other pre-tax benefits,
// which leave boxes 1, 3 and 5 as a cafeteria plan's do.
const box14Benefits = "other pre-tax benefits"

// fromW2 makes the job's items add up to its W-2s, which state the whole
// year: an ADJUSTMENT on December 31 for the W-2s' pay less the made
// items'. Pay dates with a deposit and no paystub read their gross from a
// template, and the W-2 says what they were.
func (j *Job) fromW2() {
	if len(j.w2s) == 0 {
		return
	}
	var want pay
	for _, w := range j.w2s {
		want = want.plus(w2Pay(w, model.Value(j.m.GetEmployerHsa())))
	}
	got := j.sumOf(j.made())
	diff := pay{pretax: map[factsv1.PretaxKind]line{}}
	pick := func(w, h line) line {
		if !w.set {
			return line{}
		}
		return setLine(w.v - h.v)
	}
	diff.gross, diff.federal = pick(want.gross, got.gross), pick(want.federal, got.federal)
	diff.ss, diff.medicare = pick(want.ss, got.ss), pick(want.medicare, got.medicare)
	diff.roth401k, diff.roth403b = pick(want.roth401k, got.roth401k), pick(want.roth403b, got.roth403b)
	for k, v := range want.pretax {
		diff.setPretax(k, pick(v, got.pretax[k]))
	}
	for _, s := range want.states {
		h, _ := got.state(s.state)
		i := diff.stateIndex(s.state)
		diff.states[i].wages = pick(s.wages, h.wages)
		diff.states[i].withheld = pick(s.withheld, h.withheld)
	}
	if diff = diff.nonzero(); diff.empty() {
		return
	}
	j.items = append(j.items, &draft{
		id: model.ItemID(j.itemPrefix(), j.ctx.YearEnd().String(), "w2", "adjustment"), date: j.ctx.YearEnd(),
		status: scenariosv1.ItemStatus_ITEM_STATUS_MADE, kind: scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT,
		origin: &scenariosv1.Origin{Basis: scenariosv1.Origin_BASIS_DOCUMENT, Part: j.w2Parts[0]},
		p:      diff,
	})
}

// w2Pay is a W-2 as the year's pay: gross is box 1 plus every pre-tax
// line, the HSA line is box 12 code W less the employer's own
// contribution, and the other benefits are box 14's.
func w2Pay(w *formw2.FormW2, employerHsa money.USCents) pay {
	p := pay{pretax: map[factsv1.PretaxKind]line{}}
	for _, c := range w.GetCodedAmounts() {
		v := model.Value(c.GetAmount())
		switch strings.ToUpper(c.GetCode()) {
		case "D":
			p.setPretax(factsv1.PretaxKind_PRETAX_KIND_401K, p.pretaxLine(factsv1.PretaxKind_PRETAX_KIND_401K).plus(setLine(v)))
		case "E":
			p.setPretax(factsv1.PretaxKind_PRETAX_KIND_403B, p.pretaxLine(factsv1.PretaxKind_PRETAX_KIND_403B).plus(setLine(v)))
		case "W":
			p.setPretax(factsv1.PretaxKind_PRETAX_KIND_HSA, p.pretaxLine(factsv1.PretaxKind_PRETAX_KIND_HSA).plus(setLine(v-employerHsa)))
		case "AA":
			p.roth401k = p.roth401k.plus(setLine(v))
		case "BB":
			p.roth403b = p.roth403b.plus(setLine(v))
		}
	}
	for _, o := range w.GetOtherItems() {
		if strings.EqualFold(strings.TrimSpace(o.GetDescription()), box14Benefits) {
			k := factsv1.PretaxKind_PRETAX_KIND_OTHER_BENEFITS
			p.setPretax(k, p.pretaxLine(k).plus(setLine(model.Value(o.GetAmount()))))
		}
	}
	p.gross = setLine(model.Value(w.GetWagesTipsOtherCompensation()) + p.pretaxTotal())
	p.federal = setLine(model.Value(w.GetFederalTaxWithheld()))
	p.ss = setLine(model.Value(w.GetSocialSecurityTaxWithheld()))
	p.medicare = setLine(model.Value(w.GetMedicareTaxWithheld()))
	for _, s := range w.GetStateTaxWithholding() {
		i := p.stateIndex(s.GetState())
		p.states[i].wages = p.states[i].wages.plus(setLine(model.Value(s.GetStateIncome())))
		p.states[i].withheld = p.states[i].withheld.plus(setLine(model.Value(s.GetTaxWithheld())))
	}
	return p
}
