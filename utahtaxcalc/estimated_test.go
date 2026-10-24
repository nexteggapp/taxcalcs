// Utah's prepayment rules for 2026, checked against the Tax Commission's own
// documents and the Utah Code.
//
// Utah publishes no worked example of the extension penalty. The oracle is
// Publication 58's formula, (underpayment) x .24 x days / 365 with a 12% cap,
// computed here by hand for the cases below. Every share, rate, date and cap
// is compared against its extract in testdata/. The boundaries and the
// exceptions are hand-written cases.
//
// Utah's penalty rate is fixed by statute at 2% a month, not set by period,
// so no penalty can span two rate periods; interest, which is set by calendar
// year, is not part of this penalty.
package utahtaxcalc_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/utahtaxcalc"
	"github.com/nexteggapp/taxcalcs/working"
)

var day = estimated.MustDate

const utah = estimated.Jurisdiction("UT")

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
	return must(utahtaxcalc.EstimatedTax(2026))
}

func has(rs []working.Required, k working.PositionKind) bool {
	for _, r := range rs {
		if r.Position() == k {
			return true
		}
	}
	return false
}

func pay(on estimated.Date, a money.USCents) estimated.Payment {
	return must(estimated.NewPayment().On(on).Amount(a).Build())
}

var (
	due  = day(2027, 4, 15)
	ends = day(2027, 10, 15)
)

type household struct {
	tax         money.USCents
	noTax       bool
	withheld    money.USCents
	prior       *estimated.PriorReturn
	payments    []estimated.Payment
	extends     *bool // nil: not said
	returnFiled estimated.Date
	waiver      bool
	positions   *estimated.Positions
}

func yes() *bool { b := true; return &b }
func no() *bool  { b := false; return &b }

func (h household) facts(t *testing.T) estimated.Facts {
	t.Helper()
	fb := estimated.NewFigures()
	if !h.noTax {
		fb.Tax(h.tax)
	}
	b := estimated.NewFacts(utah, 2026).FilingStatus(filing.Single).ThisYear(must(fb.Build())).
		Paid(h.payments...).ReturnFiled(h.returnFiled)
	if h.withheld > 0 {
		b.Withheld(must(estimated.NewWithheld().On(day(2026, 12, 15)).Amount(h.withheld).Build()))
	}
	if h.prior != nil {
		b.PriorYear(*h.prior)
	}
	if h.extends != nil {
		b.State(must(utahtaxcalc.NewEstimatedFacts().FilesOnExtension(*h.extends).Build()))
	}
	if h.waiver {
		b.Positions(must(estimated.NewPositions().Waiver(estimated.WaiverNone).Build()))
	}
	if h.positions != nil {
		b.Positions(*h.positions)
	}
	return must(b.Build())
}

// prior is a filed 2025 return with its Utah tax liability.
func prior(tax money.USCents) *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(true).Figures(must(estimated.NewFigures().Tax(tax).Build())).Build())
	return &p
}

func notFiled() *estimated.PriorReturn {
	p := must(estimated.NewPriorReturn().Filed(false).Build())
	return &p
}

// Every 2026 figure the rules use, found in the document that publishes it.
func TestEstimatedFiguresAreInTheirSources(t *testing.T) {
	mustContain(t, "ucode-59-10-516.txt",
		"(Effective 5/5/2021)",
		"for a return filed by a taxpayer except for a partnership, the extension described in Subsection (1)(a) may be up to six months",
		"an amount equal to the lesser of: (i) 90% of the total tax reported on the return for the current taxable year; or (ii) 100% of the total tax liability for the taxable year immediately preceding the current taxable year.",
		"If a taxpayer fails to meet the requirements of Subsection (2), the commission may apply to the total balance due a penalty as provided in Section 59-1-401.")
	mustContain(t, "ucode-59-1-401.txt",
		"the penalty per month during the period of the extension of time for filing the return is an amount equal to 2% of the tax due on the return, unpaid as of the day on which the return is due as provided by law.",
		"If a person does not file a return within an extension of time allowed by Section 59-7-505 or 59-10-516, the person: (a) is not subject to a penalty in the amount described in Subsection (5)(b)",
		"upon reasonable cause shown, the commission may waive, reduce, or compromise any of the penalties or interest imposed under this part.")
	mustContain(t, "ucode-59-10-514.txt",
		"an individual income tax return filed for a tax imposed in accordance with Part 1, Determination and Reporting of Tax Liability and Information, shall be filed with the commission on or before the day on which a federal individual income tax return is due")
	mustContain(t, "tc-546.txt",
		"Rev. 10/26",
		"You have an automatic six-month extension to file your return",
		"If you do not pay the required amount by the original return due date, we will assess a penalty equal to 2 percent of the unpaid tax for each month of the extension period.")
	mustContain(t, "tc-546-worksheet.txt",
		"Pay the amount on line 8 on or before the return due date.",
		"2. Minimum payment rate . . . . . . . . . . . . . . . . 2 x .90",
		"4. Last year’s Utah tax liability (as filed, amended or audited)",
		// The PDF puts a control character after the dash.
		"5. Minimum tax due by original due date -",
		"enter the lesser of line 3 or line 4 . . . . . . . . . 5")
	mustContain(t, "pub-58.txt",
		"Rev. 11/25",
		"The penalty is 2 percent per month, calculated on a daily basis, until the date the return is filed or the extension expires.",
		"(underpayment) x .24* x (number of days outstanding) / 365 * 2 percent rate per month up to a maximum of six months, or 12 percent (annual 24 percent)",
		"You do not need to prepay an individual or fiduciary income tax return if you filed a return the previous year and the tax was zero. If you did NOT file a return the previous year, you must prepay 90 percent of the current year’s tax liability.",
		"The insufficient prepayment penalty will then not apply.",
		"• 90 percent of the current year’s tax liability, or",
		"• 100 percent of the previous year’s tax liability.")
	mustContain(t, "tc-40inst-2025-p4-5.txt",
		"by April 15, 2026, if you file on a calendar year basis (tax year ends Dec. 31, 2025)",
		"If the due date falls on a Saturday, Sunday or legal holiday, the due date is the next business day.",
		"Utah does not require quarterly estimated tax payments.",
		"All extension returns must be filed by Oct. 15, 2026.",
		"90 percent of your 2025 tax due (TC-40, line 27 plus line 30) if you did not have a Utah tax liability in 2024 or if this is your first year filing.",
		"You may prepay through withholding (W-2, TC-675R, 1099-R, etc.), payments applied from previous year refunds, credits and credit carryovers")
	mustContain(t, "tc-40inst-2025-p13.txt",
		"The penalty for underpaying an extension prepayment is 2 percent of the unpaid tax per month of the extension period.")
}

// The one date is April 15, 2027, a Thursday, and the extension ends on
// Friday, October 15, 2027. Neither moves.
func TestEstimatedScheduleIsTheOneDueDate(t *testing.T) {
	res := must(rules(t).Schedule(household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar, extends: yes()}.facts(t)))
	s := res.Value()
	if len(s) != 1 {
		t.Fatalf("%d installments, want one", len(s))
	}
	if s[0].Due() != due || s[0].NeededBy() != 9_000*money.Dollar || s[0].Status() != estimated.StatusMet {
		t.Errorf("due %v, needs %v, %v; want %v, $9,000, met", s[0].Due(), s[0].NeededBy(), s[0].Status(), due)
	}
	for _, d := range []estimated.Date{due, ends} {
		if wd := d.Weekday(); wd == time.Saturday || wd == time.Sunday {
			t.Errorf("%v falls on a %v", d, wd)
		}
	}
	// The extension's end is where an unfiled return's penalty stops.
	p := must(rules(t).Penalty(household{tax: 10_000 * money.Dollar, extends: yes()}.facts(t), day(2027, 12, 31))).Value()
	if p.AsOf() != ends {
		t.Errorf("figured to %v, want the end of the extension, %v", p.AsOf(), ends)
	}
}

// The minimum is met at exactly the lesser figure, and missed a cent below.
func TestEstimatedThresholdBoundaries(t *testing.T) {
	for _, tc := range []struct {
		withheld money.USCents
		met      bool
	}{{899_999, false}, {900_000, true}} {
		h := household{tax: 10_000 * money.Dollar, withheld: tc.withheld, extends: yes()}
		req := must(rules(t).Requirement(h.facts(t))).Value()
		if req.Required() == tc.met || req.Target() != 9_000*money.Dollar {
			t.Errorf("withheld %v: required %v, target %v (%s)", tc.withheld, req.Required(), req.Target(), req.Why())
		}
		in := must(rules(t).Schedule(h.facts(t))).Value()[0]
		if (in.Status() == estimated.StatusMet) != tc.met {
			t.Errorf("withheld %v: %v", tc.withheld, in.Status())
		}
		pen := must(rules(t).Penalty(h.facts(t), ends)).Value()
		if (pen.Total() == 0) != tc.met {
			t.Errorf("withheld %v: penalty %v", tc.withheld, pen.Total())
		}
	}
	// A prepayment on the due date counts; one the day after does not.
	for _, tc := range []struct {
		on  estimated.Date
		met bool
	}{{due, true}, {due.AddDays(1), false}} {
		h := household{tax: 10_000 * money.Dollar, payments: []estimated.Payment{pay(tc.on, 9_000*money.Dollar)}, extends: yes()}
		if got := must(rules(t).Schedule(h.facts(t))).Value()[0].Status() == estimated.StatusMet; got != tc.met {
			t.Errorf("paid %v: met %v, want %v", tc.on, got, tc.met)
		}
	}
}

// The two options, each chosen when it is the lesser, and each when it is
// unavailable.
func TestEstimatedTargetOptions(t *testing.T) {
	for name, tc := range map[string]struct {
		h      household
		target money.USCents
		basis  estimated.Basis
		ask    working.PositionKind
	}{
		"90% is the lesser":         {household{tax: 10_000 * money.Dollar, prior: prior(12_000 * money.Dollar), extends: yes()}, 9_000 * money.Dollar, estimated.BasisThisYearTax, 0},
		"100% of 2025 is less":      {household{tax: 10_000 * money.Dollar, prior: prior(8_000 * money.Dollar), extends: yes()}, 8_000 * money.Dollar, estimated.BasisPriorYearTax, 0},
		"no 2025 return on record":  {household{tax: 10_000 * money.Dollar, extends: yes()}, 9_000 * money.Dollar, estimated.BasisThisYearTax, working.PositionPriorYearReturn},
		"no 2025 return filed":      {household{tax: 10_000 * money.Dollar, prior: notFiled(), extends: yes()}, 9_000 * money.Dollar, estimated.BasisThisYearTax, working.PositionUnsettledRule},
		"this year's tax not given": {household{noTax: true, prior: prior(8_000 * money.Dollar), extends: yes()}, 8_000 * money.Dollar, estimated.BasisPriorYearTax, working.PositionThisYearTax},
	} {
		res := must(rules(t).Requirement(tc.h.facts(t)))
		r := res.Value()
		c, ok := r.Chosen()
		if !ok || c.Basis() != tc.basis || r.Target() != tc.target {
			t.Errorf("%s: chose %v for %v; want %v for %v", name, c.Basis(), r.Target(), tc.basis, tc.target)
		}
		if tc.ask != 0 && !has(res.Required(), tc.ask) {
			t.Errorf("%s: %v is not asked", name, tc.ask)
		}
		opts := r.Options()
		if len(opts) != 2 || opts[0].Percent() != money.Percent(90) || opts[1].Percent() != money.Percent(100) {
			t.Errorf("%s: options %+v", name, opts)
		}
	}
	// Without this year's tax the 90% option says why it is unavailable.
	o := must(rules(t).Requirement(household{noTax: true, prior: prior(100), extends: yes()}.facts(t))).Value().Options()[0]
	if o.Available() || o.WhyNot() != estimated.NeedsThisYearTax {
		t.Errorf("this year's option: available %v, %q", o.Available(), o.WhyNot())
	}
	// With neither, there is no minimum, and nothing to schedule or charge.
	f := household{noTax: true, extends: yes()}.facts(t)
	if r := must(rules(t).Requirement(f)).Value(); r.TargetKnown() || r.Required() {
		t.Errorf("no tax and no 2025 return: target known %v, required %v", r.TargetKnown(), r.Required())
	}
	if _, err := rules(t).Schedule(f); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("Schedule: %v", err)
	}
	if _, err := rules(t).Penalty(f, ends); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("Penalty: %v", err)
	}
}

// The test applies only to an extended return. Filing by the due date, or
// saying the filer will, takes it away; saying nothing assumes the extension
// and asks.
func TestEstimatedOnlyAnExtendedReturnIsTested(t *testing.T) {
	short := household{tax: 10_000 * money.Dollar}
	for name, tc := range map[string]struct {
		h      household
		exempt bool
		ask    bool
	}{
		"will extend":             {household{tax: short.tax, extends: yes()}, false, false},
		"will not extend":         {household{tax: short.tax, extends: no()}, true, false},
		"not said":                {household{tax: short.tax}, false, true},
		"filed by April 15":       {household{tax: short.tax, returnFiled: due}, true, false},
		"filed the day after":     {household{tax: short.tax, returnFiled: due.AddDays(1)}, false, true},
		"not said, filed on time": {household{tax: short.tax, returnFiled: day(2027, 3, 1)}, true, false},
	} {
		res := must(rules(t).Requirement(tc.h.facts(t)))
		r := res.Value()
		if r.Exempt() != tc.exempt || r.Required() == tc.exempt {
			t.Errorf("%s: exempt %v, required %v (%s)", name, r.Exempt(), r.Required(), r.Why())
		}
		if tc.exempt {
			c, ok := r.Chosen()
			if !ok || c.Basis() != estimated.BasisException || c.Amount() != 0 || r.Target() != 0 || c.Label() == "" {
				t.Errorf("%s: chosen %v %q at %v, want the exception at $0", name, c.Basis(), c.Label(), c.Amount())
			}
		}
		if !strings.HasPrefix(r.Why(), "Utah does not require estimated payments.") {
			t.Errorf("%s: the reason does not start by saying so: %q", name, r.Why())
		}
		if got := has(res.Required(), working.PositionJurisdictionFact); got != tc.ask {
			t.Errorf("%s: asked about the extension %v, want %v", name, got, tc.ask)
		}
		p := must(rules(t).Penalty(tc.h.facts(t), ends)).Value()
		if (p.Total() == 0) != tc.exempt {
			t.Errorf("%s: penalty %v", name, p.Total())
		}
	}
	// Filing by the due date is the exception, and the penalty's reason.
	f := household{tax: short.tax, returnFiled: due}.facts(t)
	c, _ := must(rules(t).Requirement(f)).Value().Chosen()
	if c.Label() != "Return filed and paid by the due date" {
		t.Errorf("filed by April 15: chosen %q", c.Label())
	}
	if got := must(rules(t).Penalty(f, ends)).Value().Reason(); got != "Return filed and paid by the due date." {
		t.Errorf("filed by April 15: penalty reason %q", got)
	}
}

// Publication 58's formula: $10,000 of tax with $5,000 withheld misses the
// $9,000 minimum, so the whole $5,000 unpaid is charged. Filed June 14, 2027,
// 60 days: $5,000 × .24 × 60 / 365 = $197.26.
func TestEstimatedPenaltyArithmetic(t *testing.T) {
	h := household{tax: 10_000 * money.Dollar, withheld: 5_000 * money.Dollar, extends: yes(), returnFiled: day(2027, 6, 14)}
	res := must(rules(t).Penalty(h.facts(t), ends))
	p := res.Value()
	lines := p.Lines()
	if len(lines) != 1 || lines[0].Shortfall() != 5_000*money.Dollar || lines[0].Days() != 60 {
		t.Fatalf("lines %+v; want one on $5,000 for 60 days", lines)
	}
	parts := lines[0].Parts()
	if len(parts) != 1 || parts[0].Rate() != money.Percent(24) || parts[0].Charge() != 19_726 {
		t.Errorf("parts %+v; want one at 24%% charging $197.26", parts)
	}
	if p.Total() != 19_726 || p.AsOf() != day(2027, 6, 14) {
		t.Errorf("penalty %v to %v, want $197.26 to June 14, 2027", p.Total(), p.AsOf())
	}
	if !has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the reasonable-cause waiver is not asked about")
	}

	// A payment after the due date does not shorten it: the penalty runs
	// until the return is filed.
	h.payments = []estimated.Payment{pay(day(2027, 5, 1), 5_000*money.Dollar)}
	if got := must(rules(t).Penalty(h.facts(t), ends)).Value().Total(); got != 19_726 {
		t.Errorf("with the balance paid May 1: %v, want $197.26", got)
	}

	// Unfiled to the end of the extension: 183 days would be 12.03%, so the
	// 12% cap, $600.
	unfiled := household{tax: 10_000 * money.Dollar, withheld: 5_000 * money.Dollar, extends: yes()}
	if got := must(rules(t).Penalty(unfiled.facts(t), day(2027, 12, 31))).Value().Total(); got != 600*money.Dollar {
		t.Errorf("to the end of the extension: %v, want the 12%% cap, $600", got)
	}
	// 182 days, to October 14: $5,000 × .24 × 182 / 365 = $598.36, under the
	// cap.
	if got := must(rules(t).Penalty(unfiled.facts(t), day(2027, 10, 14))).Value().Total(); got != 59_836 {
		t.Errorf("to October 14: %v, want $598.36", got)
	}
	// Nothing accrues by the due date itself.
	if got := must(rules(t).Penalty(unfiled.facts(t), due)).Value().Total(); got != 0 {
		t.Errorf("to the due date: %v, want nothing", got)
	}
	// Answering the waiver question removes it.
	unfiled.waiver = true
	if res := must(rules(t).Penalty(unfiled.facts(t), ends)); has(res.Required(), working.PositionPenaltyWaiver) {
		t.Error("the waiver is asked about after it was answered")
	}
}

// Utah's only waiver is for reasonable cause, with no window or age, so a
// waiver the federal rules would allow or refuse is left to the Tax
// Commission, and the open point is reported.
func TestEstimatedWaiverIsReasonableCauseOnly(t *testing.T) {
	mustContain(t, "ucode-59-1-401.txt", "upon reasonable cause shown, the commission may waive, reduce, or compromise any of the penalties or interest imposed under this part")
	mustContain(t, "pub-58.txt", "We may waive, reduce or compromise any penalties or interest, based on reasonable cause.")
	for name, pos := range map[string]estimated.Positions{
		"a disaster":      must(estimated.NewPositions().WaiverFor(estimated.CauseDisaster, day(2026, 8, 1)).Build()),
		"retired at 64":   must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2026, 6, 1)).Born(day(1962, time.March, 1)).Build()),
		"retired in 2020": must(estimated.NewPositions().WaiverFor(estimated.CauseRetirement, day(2020, 6, 1)).Born(day(1955, time.March, 1)).Build()),
	} {
		h := household{tax: 10_000 * money.Dollar, withheld: 5_000 * money.Dollar, extends: yes(), returnFiled: day(2027, 6, 14), positions: &pos}
		res := must(rules(t).Penalty(h.facts(t), ends))
		p := res.Value()
		o, ok := p.WaiverOutcome()
		if p.Total() != 19_726 || !ok || !o.Undecided() || !strings.Contains(o.Reason(), "reasonable cause") {
			t.Errorf("%s: penalty %v, outcome %v %+v", name, p.Total(), ok, o)
		}
		if p.IfWaived() != p.Total() || !has(res.Required(), working.PositionUnsettledRule) {
			t.Errorf("%s: %v if waived, required %v", name, p.IfWaived(), res.Required())
		}
	}
}

// Filing by the due date sets the minimum at nothing, so neither figure is
// asked for.
func TestEstimatedExemptDoesNotAskForTheFigures(t *testing.T) {
	res := must(rules(t).Requirement(household{noTax: true, extends: no()}.facts(t)))
	if !res.Value().Exempt() || has(res.Required(), working.PositionPriorYearReturn) || has(res.Required(), working.PositionThisYearTax) {
		t.Errorf("exempt %v, required %v", res.Value().Exempt(), res.Required())
	}
}

// Without this year's tax a missed minimum cannot be charged, because the
// charge is on the whole unpaid tax; a met one is an exact zero.
func TestEstimatedPenaltyWithoutThisYearsTax(t *testing.T) {
	h := household{noTax: true, prior: prior(8_000 * money.Dollar), withheld: 7_999 * money.Dollar, extends: yes()}
	if _, err := rules(t).Penalty(h.facts(t), ends); !errors.Is(err, estimated.ErrNeedsThisYearTax) {
		t.Errorf("short: %v, want ErrNeedsThisYearTax", err)
	}
	h.withheld = 8_000 * money.Dollar
	p := must(rules(t).Penalty(h.facts(t), ends)).Value()
	if p.Total() != 0 || p.UpperBound() {
		t.Errorf("met: %v, upper bound %v; want an exact zero", p.Total(), p.UpperBound())
	}
}

// The exceptions: a 2025 return with no tax sets the minimum at nothing, and
// a return filed after the extension falls under penalties not built.
func TestEstimatedExceptions(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0), extends: yes()}.facts(t)))
	if r := res.Value(); r.Target() != 0 || r.Required() {
		t.Errorf("2025 tax of zero: target %v, required %v", r.Target(), r.Required())
	}
	late := household{tax: 10_000 * money.Dollar, extends: yes(), returnFiled: ends.AddDays(1)}
	if _, err := rules(t).Penalty(late.facts(t), day(2027, 12, 31)); !errors.Is(err, estimated.ErrUnsupported) {
		t.Errorf("filed after the extension: %v, want ErrUnsupported", err)
	}
	onTime := household{tax: 10_000 * money.Dollar, extends: yes(), returnFiled: ends}
	if got := must(rules(t).Penalty(onTime.facts(t), day(2027, 12, 31))).Value().Total(); got != 1_200*money.Dollar {
		t.Errorf("filed on the last day of the extension: %v, want the cap on $10,000, $1,200", got)
	}
}

// Each divergence is reported with both readings and both sources, and the
// figures follow the reading this package chose.
func TestEstimatedDivergencesPinBothReadings(t *testing.T) {
	res := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, extends: yes()}.facts(t)))
	want := map[string][2]string{
		"Last year's tax was zero":       {"TC-546 and Publication 58", "§ 59-10-516(2)(b)"},
		"No return last year":            {"90% of this year's tax", "§ 59-10-516(2)(b)"},
		"By the day or by the month":     {"24% a year by the day", "§ 59-1-401(5)(b)"},
		"What the penalty is charged on": {"whole tax unpaid", "§§ 59-1-401(5)(b), 59-10-516(3)"},
	}
	if len(res.Divergences()) != len(want) {
		t.Errorf("%d divergences, want %d", len(res.Divergences()), len(want))
	}
	for _, d := range res.Divergences() {
		w, ok := want[d.Rule()]
		if !ok {
			t.Errorf("unexpected divergence %q", d.Rule())
			continue
		}
		if !strings.Contains(d.Form(), w[0]) || !strings.Contains(d.StatuteSource(), w[1]) || d.FormSource() == "" || d.Statute() == "" {
			t.Errorf("%q does not pin both readings: form %q, statute %q (%s)", d.Rule(), d.Form(), d.Statute(), d.StatuteSource())
		}
	}
	// The penalty is charged on the whole unpaid tax, the statute's reading;
	// the rest follow the forms. With nothing paid the minimum is missed, so
	// the penalty readings affect these facts and the last-year ones do not.
	for _, d := range res.Divergences() {
		uses := working.ReadingForm
		if d.Rule() == "What the penalty is charged on" {
			uses = working.ReadingStatute
		}
		if d.Uses() != uses {
			t.Errorf("%q uses %v, want %v", d.Rule(), d.Uses(), uses)
		}
		penalty := d.Rule() == "By the day or by the month" || d.Rule() == "What the penalty is charged on"
		if d.AffectsHousehold() != penalty {
			t.Errorf("%q affects these facts %v, want %v", d.Rule(), d.AffectsHousehold(), penalty)
		}
	}
	// A filer who met the minimum and filed last year with tax is touched by
	// none of them.
	met := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, withheld: 9_000 * money.Dollar, prior: prior(12_000 * money.Dollar), extends: yes()}.facts(t)))
	for _, d := range met.Divergences() {
		if d.AffectsHousehold() {
			t.Errorf("met the minimum: %q affects these facts", d.Rule())
		}
	}

	// Zero last year: the TC-40 instructions' reading would be $9,000; the
	// worksheet's is nothing.
	if got := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: prior(0), extends: yes()}.facts(t))).Value().Target(); got != 0 {
		t.Errorf("zero last year: minimum %v, want TC-546's zero rather than the TC-40 instructions' $9,000", got)
	}
	// No return last year: Publication 58's 90%, not the statute's zero.
	if got := must(rules(t).Requirement(household{tax: 10_000 * money.Dollar, prior: notFiled(), extends: yes()}.facts(t))).Value().Target(); got != 9_000*money.Dollar {
		t.Errorf("no return last year: minimum %v, want Publication 58's $9,000", got)
	}
	// 60 days: by the day $197.26; two months or part at 2% would be $200.
	// On the shortfall below the minimum, $4,000, it would be $157.81.
	h := household{tax: 10_000 * money.Dollar, withheld: 5_000 * money.Dollar, extends: yes(), returnFiled: day(2027, 6, 14)}
	if got := must(rules(t).Penalty(h.facts(t), ends)).Value().Total(); got != 19_726 {
		t.Errorf("60 days: %v, want $197.26 by the day on the whole unpaid tax", got)
	}
}

func TestEstimatedRefusesWhatIsNotBuilt(t *testing.T) {
	if _, err := utahtaxcalc.EstimatedTax(2025); !errors.Is(err, estimated.ErrNoRules) {
		t.Errorf("2025: %v, want ErrNoRules", err)
	}
	this := must(estimated.NewFigures().Tax(100).Build())
	for name, f := range map[string]estimated.Facts{
		"California's facts": must(estimated.NewFacts(estimated.California, 2026).FilingStatus(filing.Single).ThisYear(this).Build()),
		"2027":               must(estimated.NewFacts(utah, 2027).FilingStatus(filing.Single).ThisYear(this).Build()),
		"other extras":       must(estimated.NewFacts(utah, 2026).FilingStatus(filing.Single).ThisYear(this).State("not Utah's").Build()),
	} {
		if _, err := rules(t).Requirement(f); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := rules(t).Penalty(household{tax: 100}.facts(t), estimated.Date{}); err == nil {
		t.Error("a penalty with no date was accepted")
	}
}

func TestEstimatedExplanationNamesTheDifferences(t *testing.T) {
	e := rules(t).Explanation()
	text := e.Summary() + " " + strings.Join(e.Body(), " ") + " " + e.VersusFederal()
	for _, s := range []string{"no estimated tax", "April 15, 2027", "October 15, 2027", "90%", "100%", "2% a month", "12%", "whole unpaid balance", "no 110% tier"} {
		if !strings.Contains(text, s) {
			t.Errorf("the explanation does not mention %q", s)
		}
	}
}

// $3,600 withheld on $3,000 of tax never reads as a negative amount. Utah's
// prepayment test compares what was paid with the minimum, so no refund is
// stated.
func TestEstimatedWithholdingOverTheTaxHasNoNegativeAmount(t *testing.T) {
	figures := func(tax money.USCents) estimated.Figures {
		return must(estimated.NewFigures().Tax(tax).FederalAGI(60_000 * money.Dollar).StateAGI(60_000 * money.Dollar).Build())
	}
	last := must(estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(2_800 * money.Dollar)).Build())
	b := estimated.NewFacts("UT", 2026).FilingStatus(filing.Single).ThisYear(figures(3_000 * money.Dollar)).PriorYear(last)
	for m := time.January; m <= time.December; m++ {
		b.Withheld(must(estimated.NewWithheld().On(estimated.MustDate(2026, m, 28)).Amount(300 * money.Dollar).Build()))
	}
	res := must(rules(t).Requirement(must(b.Build())))
	r := res.Value()
	texts := []string{r.Why()}
	for _, o := range r.Options() {
		texts = append(texts, o.Label(), o.WhyNot())
	}
	for _, q := range res.Required() {
		texts = append(texts, q.Because(), q.Question(), q.Effect())
	}
	for _, d := range res.Divergences() {
		texts = append(texts, d.Rule(), d.Form(), d.Statute())
	}
	for _, s := range texts {
		if strings.Contains(s, "−$") || strings.Contains(s, "-$") {
			t.Errorf("a negative amount where a refund belongs: %q", s)
		}
	}
}
