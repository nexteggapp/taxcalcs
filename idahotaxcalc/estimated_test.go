// Idaho's estimated tax rules for 2026, checked against the Tax Commission's
// and the Legislature's own documents.
//
// There is no oracle to run, because there is no computation: Idaho requires
// no estimated payments of individuals. The tests check that every answer says
// so for any household, that each figure the Explanation states is in its
// extract in testdata/, and that a changed figure changes the Explanation.
package idahotaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/idahotaxcalc"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

const idaho = estimated.Jurisdiction("ID")

func extract(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return strings.Join(strings.Fields(string(raw)), " ")
}

func mustContain(t *testing.T, name string, phrases ...string) {
	t.Helper()
	text := extract(t, name)
	for _, p := range phrases {
		if !strings.Contains(text, p) {
			t.Errorf("%q is not in %s", p, name)
		}
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func rules(t *testing.T) estimated.EstimatedTax {
	t.Helper()
	return must(idahotaxcalc.EstimatedTax(2026))
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

// households are facts that would need estimated payments federally, and
// some that would not. Idaho's answer is the same for every one.
func households() map[string]estimated.Facts {
	big := must(estimated.NewFigures().Tax(40_000 * money.Dollar).StateAGI(500_000 * money.Dollar).Build())
	last := must(estimated.NewPriorReturn().Filed(true).
		Figures(must(estimated.NewFigures().Tax(30_000 * money.Dollar).Build())).FullYearResident(true).Build())
	return map[string]estimated.Facts{
		"large tax, nothing withheld": must(estimated.NewFacts(idaho, 2026).FilingStatus(filing.Single).ThisYear(big).PriorYear(last).Build()),
		"this year's tax not given": must(estimated.NewFacts(idaho, 2026).FilingStatus(filing.MarriedFilingJointly).
			ThisYear(must(estimated.NewFigures().Build())).PriorYear(last).Build()),
		"no prior return, some paid": must(estimated.NewFacts(idaho, 2026).FilingStatus(filing.HeadOfHousehold).ThisYear(big).
			Paid(pay(day(2026, 4, 15), 1_000*money.Dollar)).
			Withheld(must(estimated.NewWithheld().On(day(2026, 6, 30)).Amount(5_000 * money.Dollar).Build())).Build()),
		"a farmer": must(estimated.NewFacts(idaho, 2026).FilingStatus(filing.Single).ThisYear(big).FarmShare(9_000).Build()),
	}
}

// Every figure the rules and the Explanation state, found in the document
// that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "form-40-instructions-2025.txt",
		"Idaho doesn’t require estimated tax payments. You can prepay at any time.",
		"You automatically get an extension of up to six months to file your return if you’ve paid at least one of these: • 100% of what you paid for state income taxes the year before • 80% of your current year’s tax liability",
		"until paid at the rate of 6% per year (rate effective for 2026).",
	)
	mustContain(t, "form-51.txt",
		"Make estimated payments for tax year 2026",
		"• 100% of the income tax on your 2024 return (if you filed). You can’t use this method if you’ve never filed in Idaho. • 80% of the estimated tax due on your 2025 return.",
		"If it’s $50 or less, you don’t have to pay it now to get the extension.",
		"Total tax on your 2024 return, Form 40, line 33",
		"Enter the lesser of line 1 or line 6. If you didn’t file a 2024 tax return, then enter the amount from line 6",
	)
	mustContain(t, "idaho-code-63-3036a.txt",
		"(a) Any corporation subject to this chapter which is required to make a payment of estimated taxes to the internal revenue service and will have an Idaho income tax liability of five hundred dollars ($500) or more shall pay to the state tax commission estimated taxes due under this chapter.",
	)
	mustContain(t, "idaho-code-63-3033.txt",
		"has paid at least eighty percent (80%) of the total tax due on the income tax return when it is filed, or the total tax due on the income tax return for the prior year if a return was filed for the prior year.",
		"is fifty dollars ($50.00) or less, such payment shall not be required in order to qualify for the extension.",
		"two percent (2%) per month from the original due date to the date of payment.",
		"the penalty provided in section 63-3046 (c), Idaho Code, from the original due date.",
		"am. 2014, ch. 9, sec. 3, p. 12.]",
	)
	mustContain(t, "istc-interest-rates.txt", "Year Annual Monthly Daily 2027 6% 0.0050 0.00016438 2026 6%")
}

// No household is required to pay, and none can owe a penalty: Idaho's
// estimated tax law reaches corporations only.
func TestEstimatedNoIndividualIsRequiredToPay(t *testing.T) {
	for name, f := range households() {
		res := must(rules(t).Requirement(f))
		r := res.Value()
		if r.Required() || !r.Exempt() || r.Target() != 0 || !r.TargetKnown() || len(r.Options()) != 1 {
			t.Errorf("%s: required %v, exempt %v, target %v, known %v, %d options; want an exemption as the one option",
				name, r.Required(), r.Exempt(), r.Target(), r.TargetKnown(), len(r.Options()))
		}
		// The one option is the exception, and it is the target.
		if c, chosen := r.Chosen(); !chosen || c.Basis() != estimated.BasisException || c.Amount() != 0 ||
			c.Label() != "Idaho doesn't require estimated payments" {
			t.Errorf("%s: chosen %v %v %q at %v", name, chosen, c.Basis(), c.Label(), c.Amount())
		}
		if !strings.Contains(r.Why(), "Idaho does not require individuals to make estimated tax payments") {
			t.Errorf("%s: why is %q", name, r.Why())
		}
		// Nothing is asked: no answer the caller could give changes this.
		if len(res.Required()) != 0 {
			t.Errorf("%s: asks %v", name, res.Required())
		}
		if len(res.Applied()) == 0 || !strings.Contains(res.Applied()[0].Cite(), "63-3036A") {
			t.Errorf("%s: the rule does not cite § 63-3036A: %v", name, res.Applied())
		}
	}
}

// There are no installments, so no due dates to move.
func TestEstimatedScheduleIsEmpty(t *testing.T) {
	for name, f := range households() {
		res := must(rules(t).Schedule(f))
		if len(res.Value()) != 0 {
			t.Errorf("%s: %d installments, want none", name, len(res.Value()))
		}
		if len(res.Required()) != 0 {
			t.Errorf("%s: asks %v", name, res.Required())
		}
	}
}

// The penalty is an exact zero, figured to asOf or to the day the return was
// filed, whichever is earlier.
func TestEstimatedPenaltyIsZero(t *testing.T) {
	for name, f := range households() {
		res := must(rules(t).Penalty(f, day(2027, 4, 15)))
		p := res.Value()
		if p.Total() != 0 || len(p.Lines()) != 0 || p.UpperBound() || p.AsOf() != day(2027, 4, 15) ||
			p.Reason() != "Idaho doesn't require estimated payments." {
			t.Errorf("%s: penalty %v over %d lines, upper bound %v, to %v; want an exact zero to April 15, 2027",
				name, p.Total(), len(p.Lines()), p.UpperBound(), p.AsOf())
		}
	}

	this := must(estimated.NewFigures().Tax(40_000 * money.Dollar).Build())
	filed := must(estimated.NewFacts(idaho, 2026).FilingStatus(filing.Single).ThisYear(this).ReturnFiled(day(2027, 2, 10)).
		Positions(must(estimated.NewPositions().Waiver(estimated.WaiverCasualty).Build())).Build())
	res := must(rules(t).Penalty(filed, day(2027, 4, 15)))
	if res.Value().AsOf() != day(2027, 2, 10) || res.Value().Waiver() != estimated.WaiverCasualty {
		t.Errorf("figured to %v with waiver %v; want the filing day and the waiver asked for", res.Value().AsOf(), res.Value().Waiver())
	}
	if last := res.Applied()[len(res.Applied())-1]; !strings.Contains(last.Applied(), "nothing to waive") {
		t.Errorf("a waiver request is not answered: %q", last.Applied())
	}
	if _, err := rules(t).Penalty(filed, estimated.Date{}); err == nil {
		t.Error("a penalty with no date was figured")
	}
}

// The Form 40 instructions word the extension's prior-year test as what was
// paid; the statute and Form 51 say the tax on last year's return.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	f := households()["large tax, nothing withheld"]
	for _, ds := range [][]working.Divergence{
		must(rules(t).Requirement(f)).Divergences(),
		must(rules(t).Schedule(f)).Divergences(),
		must(rules(t).Penalty(f, day(2027, 4, 15))).Divergences(),
	} {
		if len(ds) != 1 {
			t.Fatalf("%d divergences, want one", len(ds))
		}
		d := ds[0]
		if d.Rule() != "What the extension's prior-year test measures" ||
			!strings.Contains(d.Form(), "100% of state income taxes paid the year before") ||
			!strings.Contains(d.FormSource(), "Form 40 instructions (2025") ||
			!strings.Contains(d.Statute(), "total tax due on last year's return") ||
			d.StatuteSource() != "Idaho Code § 63-3033(a)" ||
			d.Uses() != working.ReadingStatute || d.AffectsHousehold() {
			t.Errorf("the divergence does not pin both readings: %q, %q (%s), %q (%s)", d.Rule(), d.Form(), d.FormSource(), d.Statute(), d.StatuteSource())
		}
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	for _, year := range []int{2025, 2027} {
		if _, err := idahotaxcalc.EstimatedTax(year); !errors.Is(err, estimated.ErrNoRules) {
			t.Errorf("%d: %v, want ErrNoRules", year, err)
		}
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(idaho, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"extras":             must(estimated.NewFacts(idaho, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Idaho's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: Requirement accepted", name)
		}
		if _, err := rules(t).Schedule(f); err == nil {
			t.Errorf("%s: Schedule accepted", name)
		}
		if _, err := rules(t).Penalty(f, day(2027, 4, 15)); err == nil {
			t.Errorf("%s: Penalty accepted", name)
		}
	}
}

// The Explanation states the extension test and the interest with the 2026
// figures, so changing any of them fails here.
func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	all := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{
		"does not require individuals to make estimated tax payments",
		"applies only to corporations",
		"must be at least one of these",
		"One is 80% of this year's tax",
		"The other is 100% of the tax on last year's return",
		"open only if you filed an Idaho return last year",
		"$50 or less",
		"A penalty of 2% a month",
		"6% a year for 2027",
		"no 110% tier",
		"$1,000",
	} {
		if !strings.Contains(all, s) {
			t.Errorf("the explanation does not say %q", s)
		}
	}
	if len(e.Sources()) == 0 {
		t.Error("the explanation cites nothing")
	}
}
