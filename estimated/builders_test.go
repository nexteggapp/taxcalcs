// Builder validation, and that nothing a caller holds can change a value after
// it is built.
//
// These are hand-written cases. Immutability has no published oracle; what
// is checked is that every slice an accessor returns is a copy, and that a
// builder reused after Build does not reach back into what it built.
package estimated_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

var d = estimated.MustDate

func figures(t *testing.T, tax money.USCents) estimated.Figures {
	t.Helper()
	f, err := estimated.NewFigures().Tax(tax).Build()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func withheld(t *testing.T, on estimated.Date, a money.USCents) estimated.Withheld {
	t.Helper()
	w, err := estimated.NewWithheld().On(on).Amount(a).Build()
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func payment(t *testing.T, on estimated.Date, a money.USCents) estimated.Payment {
	t.Helper()
	p, err := estimated.NewPayment().On(on).Amount(a).Build()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func base(t *testing.T) *estimated.FactsBuilder {
	return estimated.NewFacts(estimated.California, 2026).
		FilingStatus(filing.Single).
		ThisYear(figures(t, 10_000*money.Dollar))
}

func TestFactsBuildRefusesWhatEveryJurisdictionWouldRefuse(t *testing.T) {
	period := func(end estimated.Date) estimated.Period {
		p, err := estimated.NewPeriod().End(end).AnnualisedTax(100).Build()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	annualised, _ := estimated.NewElections().Annualised().Build()

	for name, b := range map[string]*estimated.FactsBuilder{
		"no jurisdiction":   estimated.NewFacts("", 2026).FilingStatus(filing.Single).ThisYear(figures(t, 1)),
		"no year":           estimated.NewFacts(estimated.Federal, 0).FilingStatus(filing.Single).ThisYear(figures(t, 1)),
		"no filing status":  estimated.NewFacts(estimated.Federal, 2026).ThisYear(figures(t, 1)),
		"no figures":        estimated.NewFacts(estimated.Federal, 2026).FilingStatus(filing.Single),
		"withheld in 2025":  base(t).Withheld(withheld(t, d(2025, time.December, 31), 100)),
		"withheld in 2027":  base(t).Withheld(withheld(t, d(2027, time.January, 1), 100)),
		"paid in 2025":      base(t).Paid(payment(t, d(2025, time.December, 31), 100)),
		"filed in 2026":     base(t).ReturnFiled(d(2026, time.December, 31)),
		"first met in 2027": base(t).RequirementFirstMet(d(2027, time.January, 2)),
		"shares over whole": base(t).FarmShare(6000).FishShare(4001),
		"negative share":    base(t).FarmShare(-1),
		"annualised, three periods": base(t).Elections(annualised).
			Periods(period(d(2026, 3, 31)), period(d(2026, 5, 31)), period(d(2026, 8, 31))),
		"periods out of order": base(t).
			Periods(period(d(2026, 5, 31)), period(d(2026, 3, 31))),
	} {
		if _, err := b.Build(); err == nil {
			t.Errorf("%s: built without error", name)
		}
	}

	// An overpayment applied forward has no date of its own and is allowed.
	forward, err := estimated.NewPayment().FromPriorYear().Amount(100).Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base(t).Paid(forward).Build(); err != nil {
		t.Errorf("an overpayment applied forward was refused: %v", err)
	}
}

func TestPartBuildersRefuseBadValues(t *testing.T) {
	check := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Errorf("%s: built without error", name)
		}
	}
	noTax, err := estimated.NewFigures().FederalAGI(50_000).Build()
	if err != nil {
		t.Errorf("this year's figures without the tax were refused: %v", err)
	}
	if _, known := noTax.Tax(); known {
		t.Error("figures built without a tax report one")
	}
	_, err = estimated.NewPriorReturn().Filed(true).Figures(noTax).Build()
	check("a prior return whose figures have no tax", err)
	_, err = estimated.NewFigures().Tax(-1).Build()
	check("negative tax", err)
	_, err = estimated.NewFigures().Tax(1).Withheld(-1).Build()
	check("negative withholding", err)

	_, err = estimated.NewPriorReturn().Months(0).Build()
	check("a return of 0 months", err)
	_, err = estimated.NewPriorReturn().Months(13).Build()
	check("a return of 13 months", err)
	_, err = estimated.NewPriorReturn().Filed(true).Build()
	check("filed without figures", err)
	_, err = estimated.NewPriorReturn().Filed(true).NotRequiredToFile().Figures(figures(t, 1)).Build()
	check("filed and not required", err)

	_, err = estimated.NewPayment().Amount(1).Build()
	check("a payment with no date", err)
	_, err = estimated.NewPayment().On(d(2026, 4, 15)).Amount(-1).Build()
	check("a negative payment", err)
	_, err = estimated.NewWithheld().Amount(1).Build()
	check("withholding with no date", err)

	_, err = estimated.NewResidencePeriod("CA").From(d(2026, 6, 1)).To(d(2026, 5, 31)).Build()
	check("residence ending before it starts", err)
	_, err = estimated.NewResidencePeriod("California").From(d(2026, 1, 1)).To(d(2026, 12, 31)).Build()
	check("a state that is not a postal code", err)

	_, err = estimated.NewPerson(50).RetiredAfter62().Build()
	check("retired after 62 at 50", err)
	_, err = estimated.NewPositions().Waiver(estimated.WaiverKind(9)).Build()
	check("an unknown waiver", err)

	_, err = estimated.NewDate(2026, time.February, 29)
	check("February 29, 2026", err)
}

func exception(t *testing.T) estimated.TargetOption {
	t.Helper()
	o, err := estimated.NewException("No tax due last year").Build()
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestOutputBuildersCheckConsistency(t *testing.T) {
	small, _ := estimated.NewTargetOption(estimated.BasisThisYearTax, 9000, 900).Build()
	large, _ := estimated.NewTargetOption(estimated.BasisPriorYearTax, 10_000, 1_000).Build()
	gone, _ := estimated.NewTargetOption(estimated.BasisPriorYearTax, 10_000, 0).Unavailable("no return").Build()

	if _, err := estimated.NewRequirement().Required(true, "x").Option(small).Option(large).Choose(estimated.BasisPriorYearTax).Build(); err == nil {
		t.Error("a requirement chose the larger of two available options")
	}
	if _, err := estimated.NewRequirement().Required(true, "x").Option(small).Option(gone).Choose(estimated.BasisPriorYearTax).Build(); err == nil {
		t.Error("a requirement chose an unavailable option")
	}
	if _, err := estimated.NewRequirement().Required(true, "x").Option(small).Exempt(exception(t)).Build(); err == nil {
		t.Error("an exempt requirement said payments were required")
	}
	if _, err := estimated.NewRequirement().Required(true, "").Option(small).Choose(estimated.BasisThisYearTax).Build(); err == nil {
		t.Error("a requirement without a reason was built")
	}
	r, err := estimated.NewRequirement().Required(true, "x").Option(small).Option(gone).Choose(estimated.BasisThisYearTax).Build()
	if err != nil {
		t.Fatal(err)
	}
	if r.Target() != 900 {
		t.Errorf("target is %v, want the chosen option's 900", r.Target())
	}
	if _, err := estimated.NewTargetOption(estimated.BasisThisYearTax, 9000, 900).Unavailable("").Build(); err == nil {
		t.Error("an unavailable option without a reason was built")
	}

	if _, err := estimated.NewInstallment(1, d(2026, 4, 15)).NeededBy(10).CreditedBy(10).Shortfall(5).Build(); err == nil {
		t.Error("an installment with a shortfall was built as met")
	}
	if _, err := estimated.NewInstallment(1, d(2026, 4, 15)).Shortfall(5).Build(); err == nil {
		t.Error("an installment with nothing due was built with a shortfall")
	}
	// An exception can excuse an installment that was short: Pennsylvania's
	// REV-1630 Exceptions 1 and 2 take the interest off a shortfall it still
	// reports.
	if i, err := estimated.NewInstallment(1, d(2026, 4, 15)).NeededBy(10).CreditedBy(5).Shortfall(5).Excused().Build(); err != nil || i.Status() != estimated.StatusExcused {
		t.Errorf("an excused installment with a shortfall: %v, %v", i.Status(), err)
	}

	part, _ := estimated.NewPenaltyPart(estimated.PartInterest, 200, 700).Span(d(2026, 4, 15), d(2026, 5, 15)).Days(30).Charge(1).Source("s").Build()
	if _, err := estimated.NewPenaltyLine(1, d(2026, 4, 15), 100).Part(part).Build(); err == nil {
		t.Error("a line charged interest on more than its shortfall")
	}
	if _, err := estimated.NewPenaltyPart(estimated.PartInterest, 1, 700).Span(d(2026, 4, 15), d(2026, 5, 15)).Build(); err == nil {
		t.Error("a part without a source was built")
	}
	line, _ := estimated.NewPenaltyLine(1, d(2026, 4, 15), 300).Part(part).Build()
	if _, err := estimated.NewPenalty(d(2027, 4, 15)).Line(line).Line(line).Because(chosen(t), nil).Build(); err == nil {
		t.Error("a penalty with two lines for one installment was built")
	}

	if _, err := estimated.NewRateSeries().
		Period(d(2026, 4, 1), d(2026, 6, 30), 600, "a").
		Period(d(2026, 7, 2), d(2026, 9, 30), 700, "b").Build(); err == nil {
		t.Error("a rate series with a gap was built")
	}
	if _, err := estimated.NewRateSeries().
		Period(d(2026, 4, 1), d(2026, 6, 30), 600, "a").
		Period(d(2026, 6, 30), d(2026, 9, 30), 700, "b").Build(); err == nil {
		t.Error("a rate series with an overlap was built")
	}
	if _, err := estimated.NewRateSeries().Period(d(2026, 4, 1), d(2026, 6, 30), 600, "").Build(); err == nil {
		t.Error("a rate without a source was built")
	}
	if _, err := estimated.NewCalendar("s").Holiday(d(2026, 7, 3), "a").Holiday(d(2026, 7, 3), "b").Build(); err == nil {
		t.Error("a calendar listing a day twice was built")
	}
	if _, err := estimated.NewExplanation("s").Source("c").Build(); err == nil {
		t.Error("an explanation without a body was built")
	}
	if _, err := working.NewDivergence("r").Form("f", "fs").Uses(working.ReadingForm).Affects(true).Build(); err == nil {
		t.Error("a divergence without the statute's reading was built")
	}
}

// Every slice an accessor returns is a copy, and a builder reused after Build
// does not change what it built.
// A requirement with no target says so, and Build refuses one that claims
// no target while an option is available.
func TestRequirementWithNoTarget(t *testing.T) {
	needs, err := estimated.NewTargetOption(estimated.BasisThisYearTax, 9000, 0).Unavailable(estimated.NeedsThisYearTax).Build()
	if err != nil {
		t.Fatal(err)
	}
	none, err := estimated.NewTargetOption(estimated.BasisPriorYearTax, 10_000, 0).Unavailable("no return").Build()
	if err != nil {
		t.Fatal(err)
	}
	some, err := estimated.NewTargetOption(estimated.BasisPriorYearTax, 10_000, 500).Build()
	if err != nil {
		t.Fatal(err)
	}
	r, err := estimated.NewRequirement().Required(false, "no target yet").Option(needs).Option(none).NoTarget().Build()
	if err != nil {
		t.Fatal(err)
	}
	if r.TargetKnown() || r.Exempt() || r.Target() != 0 {
		t.Errorf("known %v, exempt %v, target %v", r.TargetKnown(), r.Exempt(), r.Target())
	}
	if _, ok := r.Chosen(); ok {
		t.Error("an option was chosen")
	}
	for name, b := range map[string]*estimated.RequirementBuilder{
		"an option available": estimated.NewRequirement().Required(false, "x").Option(needs).Option(some).NoTarget(),
		"required":            estimated.NewRequirement().Required(true, "x").Option(needs).Option(none).NoTarget(),
		"also exempt":         estimated.NewRequirement().Required(false, "x").Option(needs).NoTarget().Exempt(exception(t)),
		"also chosen":         estimated.NewRequirement().Required(false, "x").Option(some).NoTarget().Choose(estimated.BasisPriorYearTax),
	} {
		if _, err := b.Build(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	exempt, err := estimated.NewRequirement().Required(false, "x").Option(some).Exempt(exception(t)).Build()
	if err != nil || !exempt.Exempt() || !exempt.TargetKnown() {
		t.Errorf("exempt %v, known %v, %v", exempt.Exempt(), exempt.TargetKnown(), err)
	}
	if c, ok := exempt.Chosen(); !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || exempt.Target() != 0 {
		t.Errorf("the exception is not the chosen $0 target: %v %v", c, ok)
	}
}

func TestPenaltyUpperBoundSurvivesToBuilder(t *testing.T) {
	p, err := estimated.NewPenalty(d(2027, 4, 15)).Waiver(estimated.WaiverCasualty).Because(chosen(t), nil).Build()
	if err != nil {
		t.Fatal(err)
	}
	if p.UpperBound() {
		t.Error("a penalty is an upper bound without being marked one")
	}
	b, err := p.ToBuilder().UpperBound().Build()
	if err != nil {
		t.Fatal(err)
	}
	if !b.UpperBound() || b.Waiver() != estimated.WaiverCasualty || b.AsOf() != p.AsOf() || b.Reason() != p.Reason() {
		t.Errorf("ToBuilder lost something: %+v", b)
	}
}

func TestAccessorsReturnCopies(t *testing.T) {
	b := base(t).
		Withheld(withheld(t, d(2026, 3, 1), 100)).
		Paid(payment(t, d(2026, 4, 15), 200))
	f, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}

	w := f.Withholding()
	w[0] = withheld(t, d(2026, 3, 1), 999)
	p := f.Payments()
	p[0] = payment(t, d(2026, 4, 15), 999)
	b.Withheld(withheld(t, d(2026, 5, 1), 5_000))
	if f.Withholding()[0].Amount() != 100 || len(f.Withholding()) != 1 || f.WithheldTotal() != 100 {
		t.Error("changing a returned slice, or the builder, changed the facts' withholding")
	}
	if f.Payments()[0].Amount() != 200 {
		t.Error("changing a returned slice changed the facts' payments")
	}

	opt, _ := estimated.NewTargetOption(estimated.BasisThisYearTax, 9000, 900).Build()
	r, _ := estimated.NewRequirement().Required(true, "x").Option(opt).Choose(estimated.BasisThisYearTax).Build()
	r.Options()[0] = estimated.TargetOption{}
	if r.Options()[0].Amount() != 900 {
		t.Error("changing a returned slice changed the requirement's options")
	}

	part, _ := estimated.NewPenaltyPart(estimated.PartInterest, 100, 700).Span(d(2026, 4, 15), d(2026, 5, 15)).Days(30).Charge(1).Source("s").Build()
	lb := estimated.NewPenaltyLine(1, d(2026, 4, 15), 100).Part(part)
	line, _ := lb.Build()
	lb.Part(part)
	line.Parts()[0] = estimated.PenaltyPart{}
	if len(line.Parts()) != 1 || line.Parts()[0].Charge() != 1 {
		t.Error("a line's parts changed after Build")
	}
	pen, _ := estimated.NewPenalty(d(2027, 4, 15)).Line(line).Because(chosen(t), nil).Build()
	pen.Lines()[0] = estimated.PenaltyLine{}
	if pen.Lines()[0].Total() != 1 || pen.Total() != 1 {
		t.Error("changing a returned slice changed the penalty's lines")
	}

	e, _ := estimated.NewExplanation("s").Paragraph("p").Source("c").Build()
	e.Body()[0], e.Sources()[0] = "x", "x"
	if e.Body()[0] != "p" || e.Sources()[0] != "c" {
		t.Error("changing a returned slice changed the explanation")
	}

	c, _ := estimated.NewCalendar("s").Holiday(d(2026, 7, 3), "Independence Day (observed)").Build()
	c.Holidays()[0] = estimated.Holiday{}
	if _, ok := c.IsHoliday(d(2026, 7, 3)); !ok {
		t.Error("changing a returned slice changed the calendar")
	}

	s, _ := estimated.NewRateSeries().Period(d(2026, 4, 1), d(2026, 6, 30), 600, "a").Build()
	s.Periods()[0] = estimated.RatePeriod{}
	if s.Ends() != d(2026, 6, 30) {
		t.Error("changing a returned slice changed the rate series")
	}

	applied, err := estimated.Apply([]estimated.Date{d(2026, 4, 15)}, []money.USCents{100}, nil)
	if err != nil {
		t.Fatal(err)
	}
	applied[0].Pieces()[0] = estimated.Piece{}
	if applied[0].Pieces()[0].Amount() != 100 {
		t.Error("changing a returned slice changed an installment's pieces")
	}
}

func TestDates(t *testing.T) {
	if got := d(2026, 1, 31).AddMonths(1); got != d(2026, 2, 28) {
		t.Errorf("January 31 plus a month is %v, want February 28", got)
	}
	if got := d(2026, 6, 10).DaysSince(d(2026, 4, 15)); got != 56 {
		t.Errorf("April 15 to June 10 is %d days, want 56", got)
	}
	if got := d(2026, 4, 15).Long(); got != "April 15, 2026" {
		t.Errorf("long form is %q", got)
	}
	if !strings.Contains(estimated.Date{}.String(), "no date") {
		t.Error("the zero date does not say it is no date")
	}
}

// chosen is a requirement with a $900 target.
func chosen(t *testing.T) estimated.Requirement {
	t.Helper()
	o, err := estimated.NewTargetOption(estimated.BasisThisYearTax, 9000, 900).Build()
	if err != nil {
		t.Fatal(err)
	}
	r, err := estimated.NewRequirement().Required(true, "x").Option(o).Choose(estimated.BasisThisYearTax).Build()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
