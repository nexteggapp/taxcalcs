// The answers' invariants: exceptions as options, one status rule, and a
// penalty reason that says only what the figures show.
package estimated_test

import (
	"strings"
	"testing"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/money"
)

// An exception that compares an amount with a threshold can only be made
// when the comparison holds, so its label is true of the figures.
func TestExceptionLabelsAreTrueOfTheirFigures(t *testing.T) {
	o, err := estimated.ExceptionBelow("Tax less withholding", 800*money.Dollar, 1_000*money.Dollar)
	if err != nil {
		t.Fatal(err)
	}
	if o.Label() != "Tax less withholding under $1,000" || o.Basis() != estimated.BasisException || o.Amount() != 0 || !o.Available() {
		t.Errorf("%q, %v, %v, %v", o.Label(), o.Basis(), o.Amount(), o.Available())
	}
	// $5,742 is not under $1,000, and $1,000 is not under itself.
	for _, a := range []money.USCents{5_742 * money.Dollar, 1_000 * money.Dollar} {
		if _, err := estimated.ExceptionBelow("Tax less withholding", a, 1_000*money.Dollar); err == nil {
			t.Errorf("%v was called under $1,000", a)
		}
	}
	if o, err := estimated.ExceptionAtMost("Tax less withholding", 500*money.Dollar, 500*money.Dollar); err != nil || o.Label() != "Tax less withholding of $500 or less" {
		t.Errorf("%q, %v", o.Label(), err)
	}
	if _, err := estimated.ExceptionAtMost("Tax less withholding", 50_001, 500*money.Dollar); err == nil {
		t.Error("$500.01 was called $500 or less")
	}
	// A hand-written label names no figure.
	if _, err := estimated.NewException("Tax under $1,000").Build(); err == nil {
		t.Error("a hand-written exception named a figure")
	}
}

func TestOptionsAreLabelledByBasis(t *testing.T) {
	for _, tc := range []struct {
		b    estimated.Basis
		pct  money.Rate
		want string
	}{
		{estimated.BasisThisYearTax, 9000, "90% of this year's tax"},
		{estimated.BasisPriorYearTax, 11_000, "110% of last year's tax"},
		{estimated.BasisThisYearTax, 6667, "66.67% of this year's tax"},
	} {
		o, err := estimated.NewTargetOption(tc.b, tc.pct, 100).Build()
		if err != nil {
			t.Fatal(err)
		}
		if o.Label() != tc.want {
			t.Errorf("%v at %v: %q, want %q", tc.b, tc.pct, o.Label(), tc.want)
		}
	}
	if _, err := estimated.NewTargetOption(estimated.BasisException, 0, 0).Build(); err == nil {
		t.Error("an exception with no label was built")
	}
	// An option that isn't open carries no amount.
	if _, err := estimated.NewTargetOption(estimated.BasisPriorYearTax, 10_000, 500).Unavailable("You didn't file a 2025 return").Build(); err == nil {
		t.Error("an unavailable option carried an amount")
	}
}

// An exception is the chosen $0 target, and an exception listed beside the
// options without being chosen is refused.
func TestExceptionIsTheOnlyChosenOption(t *testing.T) {
	ninety, _ := estimated.NewTargetOption(estimated.BasisThisYearTax, 9000, 5_827*money.Dollar).Build()
	last, _ := estimated.NewTargetOption(estimated.BasisPriorYearTax, 10_000, 6_100*money.Dollar).Build()
	under, _ := estimated.ExceptionBelow("Tax less withholding", 600*money.Dollar, 1_000*money.Dollar)
	r, err := estimated.NewRequirement().Required(false, "x").Option(ninety).Option(last).Exempt(under).Build()
	if err != nil {
		t.Fatal(err)
	}
	c, ok := r.Chosen()
	if !ok || c.Label() != under.Label() || r.Target() != 0 || len(r.Options()) != 3 {
		t.Errorf("chosen %q, target %v, %d options", c.Label(), r.Target(), len(r.Options()))
	}
	if _, err := estimated.NewRequirement().Required(false, "x").Option(ninety).Option(under).Choose(estimated.BasisThisYearTax).Build(); err == nil {
		t.Error("an exception was listed without being chosen")
	}
}

func TestStatusOf(t *testing.T) {
	for _, tc := range []struct {
		needed, credited money.USCents
		excused          bool
		want             estimated.Status
	}{
		{0, 0, false, estimated.StatusNothingDue},
		{0, 500, true, estimated.StatusNothingDue},
		{1_000, 1_000, false, estimated.StatusMet},
		{1_000, 1_200, true, estimated.StatusMet},
		{1_000, 999, false, estimated.StatusShort},
		{1_000, 999, true, estimated.StatusExcused},
	} {
		if got := estimated.StatusOf(tc.needed, tc.credited, tc.excused); got != tc.want {
			t.Errorf("needed %v, credited %v, excused %v: %v, want %v", tc.needed, tc.credited, tc.excused, got, tc.want)
		}
	}
	// California's September 15 needs 70% by then. It is met or short, not
	// "nothing due", because something is needed by it.
	i, err := estimated.NewInstallment(3, d(2026, 9, 15)).NeededBy(7_000).CreditedBy(7_500).Build()
	if err != nil || i.Status() != estimated.StatusMet {
		t.Errorf("September 15: %v, %v", i.Status(), err)
	}
}

func installment(t *testing.T, n int, needed, credited, short money.USCents, excused bool) estimated.Installment {
	t.Helper()
	b := estimated.NewInstallment(n, d(2026, 4, 15).AddMonths(2*(n-1))).NeededBy(needed).CreditedBy(credited).Shortfall(short)
	if excused {
		b.Excused()
	}
	i, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return i
}

// The reason states the test that decided the penalty, from the figures it
// was decided on.
func TestPenaltyReasonStatesWhatDecidedIt(t *testing.T) {
	under, _ := estimated.ExceptionBelow("Tax less withholding", 600*money.Dollar, 1_000*money.Dollar)
	exempt, err := estimated.NewRequirement().Required(false, "x").Exempt(under).Build()
	if err != nil {
		t.Fatal(err)
	}
	p, err := estimated.NewPenalty(d(2027, 4, 15)).Because(exempt, nil).Build()
	if err != nil || p.Reason() != "Tax less withholding under $1,000." {
		t.Errorf("%q, %v", p.Reason(), err)
	}

	met := []estimated.Installment{installment(t, 1, 100, 100, 0, false), installment(t, 2, 200, 250, 0, false)}
	p, err = estimated.NewPenalty(d(2027, 4, 15)).Because(chosen(t), met).Build()
	if err != nil || p.Reason() != "Your payments meet every due date." {
		t.Errorf("%q, %v", p.Reason(), err)
	}

	part, _ := estimated.NewPenaltyPart(estimated.PartInterest, 100, 700).Span(d(2026, 4, 15), d(2026, 5, 15)).Days(30).Charge(1).Source("s").Build()
	line1, _ := estimated.NewPenaltyLine(1, d(2026, 4, 15), 100).Part(part).Build()
	line2, _ := estimated.NewPenaltyLine(2, d(2026, 6, 15), 100).Part(part).Build()
	short := []estimated.Installment{installment(t, 1, 200, 100, 100, false), installment(t, 2, 400, 200, 100, false)}
	p, err = estimated.NewPenalty(d(2027, 4, 15)).Line(line1).Line(line2).Because(chosen(t), short).Build()
	if err != nil || p.Reason() != "Payments fall short on two due dates." {
		t.Errorf("%q, %v", p.Reason(), err)
	}

	excused := []estimated.Installment{installment(t, 1, 100, 100, 0, false), installment(t, 2, 200, 100, 100, true)}
	p, err = estimated.NewPenalty(d(2027, 4, 15)).Because(chosen(t), excused).Build()
	if err != nil || !strings.Contains(p.Reason(), "excused") {
		t.Errorf("%q, %v", p.Reason(), err)
	}

	// A jurisdiction can add a sentence, with no figure in it.
	p, err = estimated.NewPenalty(d(2027, 4, 15)).Because(chosen(t), met).Context("The January payment still counts after a move.").Build()
	if err != nil || p.Reason() != "Your payments meet every due date. The January payment still counts after a move." {
		t.Errorf("%q, %v", p.Reason(), err)
	}
	if b, err := p.ToBuilder().UpperBound().Build(); err != nil || b.Reason() != p.Reason() {
		t.Errorf("ToBuilder lost the context: %q, %v", b.Reason(), err)
	}
	if _, err := estimated.NewPenalty(d(2027, 4, 15)).Because(chosen(t), met).Context("A $50 charge.").Build(); err == nil {
		t.Error("a context naming a figure was accepted")
	}
	if _, err := estimated.NewPenalty(d(2027, 4, 15)).Because(chosen(t), met).Context("a clause").Build(); err == nil {
		t.Error("a context that is not a sentence was accepted")
	}

	// An exception and a penalty cannot both be true.
	if _, err := estimated.NewPenalty(d(2027, 4, 15)).Line(line1).Because(exempt, nil).Build(); err == nil {
		t.Error("a penalty was charged under an exception")
	}
	if _, err := estimated.NewPenalty(d(2027, 4, 15)).Build(); err == nil {
		t.Error("a penalty with no requirement to explain it was built")
	}
}

// A penalty that stops at the last published rate is no maximum, and its
// summary says how far it runs.
func TestPenaltyCutShortIsNoMaximum(t *testing.T) {
	part, _ := estimated.NewPenaltyPart(estimated.PartInterest, 10_000, 700).Span(d(2026, 4, 15), d(2026, 12, 31)).Days(260).Charge(10_900).Source("s").Build()
	line, _ := estimated.NewPenaltyLine(1, d(2026, 4, 15), 10_000).Part(part).Build()
	short := []estimated.Installment{installment(t, 1, 20_000, 10_000, 10_000, false)}
	b := estimated.NewPenalty(d(2027, 4, 15)).Line(line).Because(chosen(t), short).UpperBound()
	p, err := b.Build()
	if err != nil || !p.UpperBound() || p.Summary() != "At most $109, and less if this year's tax turns out lower." {
		t.Errorf("%q, %v, %v", p.Summary(), p.UpperBound(), err)
	}
	p, err = p.ToBuilder().RatedThrough(d(2026, 12, 31)).Build()
	if err != nil || p.UpperBound() || p.RatedThrough() != d(2026, 12, 31) ||
		p.Summary() != "About $109 through Dec 31, 2026, then more at a rate not yet published." {
		t.Errorf("%q, %v, %v", p.Summary(), p.UpperBound(), err)
	}
	if again, _ := p.ToBuilder().Build(); again.RatedThrough() != p.RatedThrough() {
		t.Error("ToBuilder lost the last rated day")
	}
}

func TestLeavesWordsARefundWithoutAMinus(t *testing.T) {
	for _, tc := range []struct {
		a           money.USCents
		now, before string
	}{
		{74_200, "leaves $742", "left $742"},
		{0, "leaves $0", "left $0"},
		{-13_600, "leaves a $136 refund", "left a $136 refund"},
	} {
		if got := estimated.Leaves(tc.a); got != tc.now {
			t.Errorf("Leaves(%v) = %q, want %q", tc.a, got, tc.now)
		}
		if got := estimated.Left(tc.a); got != tc.before {
			t.Errorf("Left(%v) = %q, want %q", tc.a, got, tc.before)
		}
	}
}

func waiverFacts(t *testing.T, pos *estimated.PositionsBuilder) estimated.Facts {
	t.Helper()
	p, err := pos.Build()
	if err != nil {
		t.Fatal(err)
	}
	f, err := base(t).Positions(p).Build()
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// The federal-style waivers, as the Form 2210 instructions state them: a
// casualty or disaster, which the IRS judges; and retirement after reaching
// 62, or disability, in the tax year or the one before.
func TestStandardWaiver(t *testing.T) {
	born := d(1962, 3, 1)
	for _, tc := range []struct {
		name string
		pos  *estimated.PositionsBuilder
		want string // eligible, refused or undecided
	}{
		{"disaster", estimated.NewPositions().WaiverFor(estimated.CauseDisaster, d(2026, 8, 1)), "eligible"},
		{"retired at 64 in 2026", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, d(2026, 6, 1)).Born(born), "eligible"},
		{"retired at 63 in 2025", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, d(2025, 6, 1)).Born(born), "eligible"},
		{"retired in 2020", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, d(2020, 6, 1)).Born(born), "refused"},
		{"retired at 61", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, d(2026, 6, 1)).Born(d(1965, 1, 1)), "refused"},
		{"62 the day after retiring", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, d(2026, 2, 28)).Born(d(1964, 3, 1)), "refused"},
		{"retired, no date of birth", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, d(2026, 6, 1)), "undecided"},
		{"retired, no date", estimated.NewPositions().WaiverFor(estimated.CauseRetirement, estimated.Date{}).Born(born), "undecided"},
		{"disabled in 2025", estimated.NewPositions().WaiverFor(estimated.CauseDisability, d(2025, 11, 1)), "eligible"},
		{"disabled in 2023", estimated.NewPositions().WaiverFor(estimated.CauseDisability, d(2023, 11, 1)), "refused"},
	} {
		o, ok := estimated.StandardWaiver(waiverFacts(t, tc.pos), "the IRS", 62)
		got := map[bool]string{true: "eligible"}[o.Eligible()]
		if o.Refused() {
			got = "refused"
		}
		if o.Undecided() {
			got = "undecided"
		}
		if !ok || got != tc.want || o.Reason() == "" {
			t.Errorf("%s: %s, %q; want %s", tc.name, got, o.Reason(), tc.want)
		}
	}
	if _, ok := estimated.StandardWaiver(waiverFacts(t, estimated.NewPositions().Waiver(estimated.WaiverNone)), "the IRS", 62); ok {
		t.Error("an outcome for no waiver asked")
	}
}

func TestPenaltyCarriesItsWaiverOutcome(t *testing.T) {
	part, _ := estimated.NewPenaltyPart(estimated.PartInterest, 10_000, 700).Span(d(2026, 4, 15), d(2026, 12, 31)).Days(260).Charge(10_900).Source("s").Build()
	line, _ := estimated.NewPenaltyLine(1, d(2026, 4, 15), 10_000).Part(part).Build()
	short := []estimated.Installment{installment(t, 1, 20_000, 10_000, 10_000, false)}
	p, err := estimated.NewPenalty(d(2027, 4, 15)).Line(line).Because(chosen(t), short).Waiver(estimated.WaiverCasualty).
		WaiverOutcome(estimated.WaiverEligible("The IRS can waive it.")).Build()
	if o, ok := p.WaiverOutcome(); err != nil || !ok || !o.Eligible() || p.IfWaived() != 0 || p.Total() != 10_900 {
		t.Errorf("%v %v %v %v %v", err, ok, o, p.IfWaived(), p.Total())
	}
	if again, _ := p.ToBuilder().Build(); func() bool { o, _ := again.WaiverOutcome(); return !o.Eligible() }() {
		t.Error("ToBuilder lost the waiver outcome")
	}
	if _, err := estimated.NewPenalty(d(2027, 4, 15)).Because(chosen(t), short).WaiverOutcome(estimated.WaiverRefused("No. Really. Truly.")).Build(); err == nil {
		t.Error("a three-sentence waiver reason was accepted")
	}
}

// A waiver that lifts the penalty but not the interest leaves the interest.
func TestWaiverThatLeavesTheInterest(t *testing.T) {
	interest, _ := estimated.NewPenaltyPart(estimated.PartInterest, 10_000, 700).Span(d(2026, 4, 15), d(2026, 12, 31)).Days(260).Charge(164).Source("s").Build()
	flat, _ := estimated.NewPenaltyPart(estimated.PartFlat, 10_000, 900).Charge(900).Source("s").Build()
	line, _ := estimated.NewPenaltyLine(1, d(2026, 4, 15), 10_000).Part(flat).Part(interest).Build()
	short := []estimated.Installment{installment(t, 1, 20_000, 10_000, 10_000, false)}
	p, err := estimated.NewPenalty(d(2027, 4, 15)).Line(line).Because(chosen(t), short).Waiver(estimated.WaiverCasualty).
		WaiverOutcome(estimated.WaiverEligibleExcept("The penalty can be waived, though not the interest.", estimated.PartInterest)).Build()
	if err != nil || p.Total() != 1_064 || p.IfWaived() != 164 {
		t.Errorf("total %v, if waived %v, %v; want $10.64 and the $1.64 of interest", p.Total(), p.IfWaived(), err)
	}
}
