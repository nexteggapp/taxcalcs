package employer_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/internal/civil"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/employer"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	formw2 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

// Slots: pay dates from first_pay_date and the frequency, weekend dates
// moved to the Friday before, none before starts.
func TestPayDates(t *testing.T) {
	semi := employer.PayDates(2026, scenariosv1.PayFrequency_PAY_FREQUENCY_SEMI_MONTHLY, civil.Must(2026, time.January, 15), civil.Date{})
	if len(semi) != 24 {
		t.Fatalf("%d twice-monthly dates, want 24", len(semi))
	}
	got := strings.Join(dates(semi), " ")
	for _, want := range []string{"2026-08-14", "2026-08-31", "2026-10-30", "2026-11-13", "2026-12-31"} {
		if !strings.Contains(got, want) {
			t.Errorf("twice-monthly dates %s lack %s", got, want)
		}
	}
	bi := employer.PayDates(2026, scenariosv1.PayFrequency_PAY_FREQUENCY_BIWEEKLY, civil.Must(2026, time.January, 9), civil.Date{})
	if len(bi) != 26 || bi[0].String() != "2026-01-09" || bi[25].String() != "2026-12-25" {
		t.Errorf("every-other-week dates %v, want 26 from Jan 9 to Dec 25", dates(bi))
	}
	monthly := employer.PayDates(2026, scenariosv1.PayFrequency_PAY_FREQUENCY_MONTHLY, civil.Date{}, civil.Must(2026, time.June, 1))
	if len(monthly) != 7 || monthly[0].String() != "2026-06-30" || monthly[4].String() != "2026-10-30" {
		t.Errorf("monthly dates from June %v, want 7, the last weekday of each month", dates(monthly))
	}
}

// Evidence fills every pay date up to the latest evidence date; the default
// part, SameAsLatest, the pay dates after it. A covered pay date with no
// paystub, deposit or year-to-date column is an expected paycheck copying
// the template, so the $140,000 year is $140,000.
func TestEvidenceThenDefault(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)))
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	if got := item(t, p, "employers/acme/2026-08-14/regular"); got.GetStatus() != scenariosv1.ItemStatus_ITEM_STATUS_MADE ||
		got.GetOrigin().GetBasis() != scenariosv1.Origin_BASIS_DOCUMENT {
		t.Errorf("the Aug 14 paystub's item is %v", got)
	}
	for _, it := range p.GetItems() {
		if it.GetDate().GetMonth() < 8 && (it.GetStatus() != scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED ||
			it.GetOrigin().GetTemplate() != "acme-s1" || len(it.GetEstimated()) == 0) {
			t.Errorf("a covered pay date's item is %v, want an estimated copy of acme-s1", it)
		}
	}
	rest := item(t, p, "employers/acme/2026-12-31/regular")
	if rest.GetStatus() != scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED || rest.GetOrigin().GetPart() != "acme-default" {
		t.Errorf("Dec 31 is %v, want expected from the default part", rest)
	}
	if g := cents(rest.GetPaycheck().GetGross()); !near(g, 583333) {
		t.Errorf("Dec 31 gross %v, want the latest paystub's $5,833.33", g)
	}
	if f := cents(rest.GetPaycheck().GetFederalWithheld()); f != 70000 {
		t.Errorf("Dec 31 federal withheld %v, want the paystub's $700 repeated", f)
	}
	if n := len(p.GetItems()); n != 24 {
		t.Errorf("%d items, want every pay date", n)
	}
	if g := cents(p.GetYear().GetItems().GetGross()); g != dollars(money.USCents(24*583333)) {
		t.Errorf("gross for the year %v, want 24 paychecks", g)
	}
}

// A regular paystub fills the pay date nearest it; payroll moves a weekend
// pay date to the business day before.
func TestPaystubFillsNearestPayDate(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 13)))
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	got := item(t, p, "employers/acme/2026-08-14/regular")
	if got.GetDate().GetDay() != 13 {
		t.Errorf("the item is dated %v, want the paystub's own date", got.GetDate())
	}
}

// Deposits within three days of a paystub that sum to its net pair with
// it, so split deposits pair too.
func TestSplitDepositsPairByNet(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)),
		deposits("acme-d", deposit("acme-d1", time.August, 14, 3000), deposit("acme-d2", time.August, 15, 1000), deposit("acme-d3", time.August, 14, 12.50)))
	p, j := expand(t, ctxAt(t, time.September, 8, nil), m)
	got := item(t, p, "employers/acme/2026-08-14/regular")
	if cents(got.GetTakeHome()) != 400000 || len(got.GetOrigin().GetEvidence()) != 3 {
		t.Errorf("the paystub's item took home %v from %v, want $4,000 from the stub and two deposits", cents(got.GetTakeHome()), got.GetOrigin().GetEvidence())
	}
	extra := item(t, p, "employers/acme/2026-08-14/deposit/acme-d3")
	if extra.GetKind() != scenariosv1.ItemKind_ITEM_KIND_DEPOSIT || !hasQuestion(j, "employers/acme/deposit/acme-d3") {
		t.Errorf("the $12.50 deposit is %v, want an extra paycheck asked about", extra)
	}
}

// A paystub with no net pairs by date with the deposits within three days,
// taken as one set.
func TestNetlessStubPairsByDate(t *testing.T) {
	f := paycheck()
	f.Net = nil
	m := job(stubs("acme-p", stub("acme-s1", time.August, 14, factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR, f)),
		deposits("acme-d", deposit("acme-d1", time.August, 14, 3000), deposit("acme-d2", time.August, 14, 999)))
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	if got := item(t, p, "employers/acme/2026-08-14/regular"); cents(got.GetTakeHome()) != 399900 {
		t.Errorf("take-home %v, want both deposits, $3,999", cents(got.GetTakeHome()))
	}
}

// Two candidate sets for one paystub are a question; until it is answered
// the set nearest in date pairs.
func TestTwoDepositSetsPairTheNearest(t *testing.T) {
	f := paycheck()
	f.Net = nil
	m := job(stubs("acme-p", stub("acme-s1", time.August, 14, factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR, f)),
		deposits("acme-d", deposit("acme-d1", time.August, 15, 3000), deposit("acme-d2", time.August, 17, 999)))
	p, j := expand(t, ctxAt(t, time.September, 8, nil), m)
	if got := item(t, p, "employers/acme/2026-08-14/regular"); cents(got.GetTakeHome()) != 300000 {
		t.Errorf("take-home %v, want the Aug 15 set, $3,000", cents(got.GetTakeHome()))
	}
	if !hasQuestion(j, "employers/acme/paystub/acme-s1/deposits") {
		t.Errorf("questions %v, want one about the two sets", questionIDs(j))
	}
	// By net: two deposits each equal to it.
	m = job(stubs("acme-p", regular("acme-s1", time.August, 14)),
		deposits("acme-d", deposit("acme-d1", time.August, 17, 4000), deposit("acme-d2", time.August, 14, 4000)))
	p, j = expand(t, ctxAt(t, time.September, 8, nil), m)
	if got := item(t, p, "employers/acme/2026-08-14/regular"); len(got.GetOrigin().GetEvidence()) != 2 || got.GetOrigin().GetEvidence()[1] != "acme-d2" {
		t.Errorf("evidence %v, want the stub and the Aug 14 deposit", got.GetOrigin().GetEvidence())
	}
	if !hasQuestion(j, "employers/acme/paystub/acme-s1/deposits") {
		t.Errorf("questions %v, want one about the two sets", questionIDs(j))
	}
}

// A deposit in a pay period no paystub fills is that paycheck, copying the
// latest regular paystub before it, marked as estimated.
func TestDepositFillsPayDateFromTemplate(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)), deposits("acme-d", deposit("acme-d1", time.August, 31, 4000)))
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	got := item(t, p, "employers/acme/2026-08-31/regular")
	switch {
	case got.GetStatus() != scenariosv1.ItemStatus_ITEM_STATUS_MADE:
		t.Errorf("status %v, want made", got.GetStatus())
	case got.GetOrigin().GetBasis() != scenariosv1.Origin_BASIS_LEDGER || got.GetOrigin().GetTemplate() != "acme-s1":
		t.Errorf("origin %v, want the bank entry, copying acme-s1", got.GetOrigin())
	case !near(cents(got.GetPaycheck().GetGross()), 583333) || len(got.GetEstimated()) == 0:
		t.Errorf("figures %v estimated %v, want the paystub's, estimated", got.GetPaycheck(), got.GetEstimated())
	}
}

// Any other unpaired deposit is an extra paycheck, its gross estimated from
// the latest regular paystub, with nothing withheld, and the result asks.
func TestExtraDepositCountsWithNothingWithheld(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)), deposits("acme-d", deposit("acme-d1", time.August, 22, 400)))
	p, j := expand(t, ctxAt(t, time.September, 8, nil), m)
	got := item(t, p, "employers/acme/2026-08-22/deposit/acme-d1")
	if cents(got.GetPaycheck().GetFederalWithheld()) != 0 || !near(cents(got.GetPaycheck().GetGross()), 583333) {
		t.Errorf("extra deposit %v, want the template's $5,833.33 gross and nothing withheld", got.GetPaycheck())
	}
	if !hasQuestion(j, "employers/acme/deposit/acme-d1") {
		t.Errorf("questions %v, want one about the deposit", questionIDs(j))
	}
}

// An entry answered as not this source's leaves the model.
func TestNotThisSourceLeavesTheModel(t *testing.T) {
	d := deposit("acme-d1", time.August, 22, 400)
	d.Matches = &scenariosv1.Match{To: &scenariosv1.Match_NotThisSource_{NotThisSource: &scenariosv1.Match_NotThisSource{}}}
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)), deposits("acme-d", d))
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	for _, it := range p.GetItems() {
		if strings.Contains(it.GetId(), "acme-d1") {
			t.Errorf("the deposit made an item, %s", it.GetId())
		}
	}
}

// Bonus, vest and final paystubs are additive, by their evidence id.
func TestBonusStubIsAdditive(t *testing.T) {
	bonus := stub("acme-b1", time.August, 20, factsv1.PaycheckKind_PAYCHECK_KIND_BONUS, &factsv1.PayFigures{Gross: usd(10000), FederalWithheld: usd(2200)})
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14), bonus))
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	if got := item(t, p, "employers/acme/2026-08-20/bonus/acme-b1"); got.GetKind() != scenariosv1.ItemKind_ITEM_KIND_BONUS {
		t.Errorf("kind %v, want bonus", got.GetKind())
	}
}

// A year-to-date ADJUSTMENT covers the pay dates with neither a paystub nor
// a deposit, at the latest paystub with a year-to-date column.
func TestYearToDateAdjustment(t *testing.T) {
	s := regular("acme-s1", time.August, 14)
	s.Paystub.YearToDate = &factsv1.PayFigures{Gross: usd(5833.33 * 15), FederalWithheld: usd(700 * 15)}
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", s)))
	adj := item(t, p, "employers/acme/2026-08-14/regular/adjustment")
	if adj.GetKind() != scenariosv1.ItemKind_ITEM_KIND_ADJUSTMENT || cents(adj.GetPaycheck().GetFederalWithheld()) != 980000 {
		t.Errorf("adjustment %v, want 14 paychecks' $9,800 federal", adj.GetPaycheck())
	}
	if cents(p.GetSoFar().GetItems().GetGross()) != dollars(cents(s.GetPaystub().GetYearToDate().GetGross())) {
		t.Errorf("gross so far %v, want the year-to-date column's", cents(p.GetSoFar().GetItems().GetGross()))
	}
}

// A YearTotal fills the pay dates after the evidence with equal shares of
// the year less the evidence, for the lines it states only.
func TestYearTotalSharesWhatIsLeft(t *testing.T) {
	yt := &scenariosv1.EmployerPart{Id: "acme-yt", Part: &scenariosv1.EmployerPart_YearTotal_{YearTotal: &scenariosv1.EmployerPart_YearTotal{
		Year: &factsv1.PayFigures{Gross: usd(5833.33*15 + 900*9)}, TypedAgainst: day(time.August, 14)}}}
	s := regular("acme-s1", time.August, 14)
	s.Paystub.YearToDate = &factsv1.PayFigures{Gross: usd(5833.33 * 15)}
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", s), yt))
	dec := item(t, p, "employers/acme/2026-12-31/regular")
	if cents(dec.GetPaycheck().GetGross()) != 90000 || dec.GetOrigin().GetPart() != "acme-yt" {
		t.Errorf("Dec 31 is %v from %s, want a $900 share from the total", dec.GetPaycheck().GetGross(), dec.GetOrigin().GetPart())
	}
	if k := dec.GetPaycheck().GetPretax(); len(k) != 1 || cents(k[0].GetAmount()) != 50000 {
		t.Errorf("Dec 31 pre-tax %v, want the default's $500 401(k): the total states gross only", k)
	}
	if !strings.Contains(strings.Join(dec.GetEstimated(), " "), "paycheck.federal_withheld") {
		t.Errorf("estimated %v, want federal withholding computed", dec.GetEstimated())
	}
	if got := cents(p.GetYear().GetItems().GetGross()); got != dollars(cents(usd(5833.33*15+900*9))) {
		t.Errorf("gross for the year %v, want the total", got)
	}
}

// A pay YearTotal stands aside once a paystub newer than typed_against is
// read, and the result says why.
func TestYearTotalStandsAsideForNewerPaystub(t *testing.T) {
	yt := &scenariosv1.EmployerPart{Id: "acme-yt", Part: &scenariosv1.EmployerPart_YearTotal_{YearTotal: &scenariosv1.EmployerPart_YearTotal{
		Year: &factsv1.PayFigures{Gross: usd(1)}, TypedAgainst: day(time.July, 31)}}}
	p, j := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", regular("acme-s1", time.August, 14)), yt))
	if dec := item(t, p, "employers/acme/2026-12-31/regular"); dec.GetOrigin().GetPart() != "acme-default" {
		t.Errorf("Dec 31 is filled by %s, want the default", dec.GetOrigin().GetPart())
	}
	if !hasQuestion(j, "employers/acme/stands-aside/acme-yt") {
		t.Errorf("questions %v, want the total standing aside", questionIDs(j))
	}
}

func payRate(id string, from *commonv1.Date, gross float64) *scenariosv1.EmployerPart {
	return &scenariosv1.EmployerPart{Id: id, Part: &scenariosv1.EmployerPart_PayRate_{PayRate: &scenariosv1.EmployerPart_PayRate{
		From: from, Paycheck: &factsv1.PayFigures{Gross: usd(gross)}}}}
}

// An assumed period part fills from its start until the next one's; its
// unset pre-tax lines are none and its unset withholding is computed.
func TestPayRateFromItsStart(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)), payRate("acme-raise", day(time.October, 1), 6250))
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	if sep := item(t, p, "employers/acme/2026-09-30/regular"); sep.GetOrigin().GetPart() != "acme-default" {
		t.Errorf("Sep 30 filled by %s, want the default", sep.GetOrigin().GetPart())
	}
	oct := item(t, p, "employers/acme/2026-10-15/regular")
	if oct.GetOrigin().GetPart() != "acme-raise" || cents(oct.GetPaycheck().GetGross()) != 625000 || len(oct.GetPaycheck().GetPretax()) != 0 {
		t.Errorf("Oct 15 is %v from %s, want $6,250 with no pre-tax lines from the raise", oct.GetPaycheck(), oct.GetOrigin().GetPart())
	}
	if cents(oct.GetPaycheck().GetFederalWithheld()) == 0 {
		t.Error("Oct 15's federal withholding was not computed")
	}
}

// An unset start is the day after the evidence; an earlier start is clipped
// to it; of several clipped to one day, the latest-started counts and the
// others stand aside.
func TestPayRatesClippedToTheEvidence(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)),
		payRate("acme-early", day(time.March, 1), 1000), payRate("acme-later", day(time.July, 1), 2000))
	p, j := expand(t, ctxAt(t, time.September, 8, nil), m)
	if aug := item(t, p, "employers/acme/2026-08-31/regular"); aug.GetOrigin().GetPart() != "acme-later" {
		t.Errorf("Aug 31 filled by %s, want the later-started change", aug.GetOrigin().GetPart())
	}
	if !hasQuestion(j, "employers/acme/stands-aside/acme-early") {
		t.Errorf("questions %v, want the earlier change standing aside", questionIDs(j))
	}
}

// A job with no paystub yet fills its pay dates from EachPaycheck.
func TestEachPaycheckForAJobWithNoPaystub(t *testing.T) {
	m := job()
	m.Starts = day(time.November, 1)
	m.DefaultPart = &scenariosv1.EmployerDefault{Id: "acme-default", Rest: &scenariosv1.EmployerDefault_EachPaycheck{EachPaycheck: &factsv1.PayFigures{Gross: usd(4000)}}}
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	if len(p.GetItems()) != 4 {
		t.Errorf("%d items, want Nov 13 to Dec 31", len(p.GetItems()))
	}
	if g := cents(p.GetYear().GetItems().GetGross()); g != 1_600_000 {
		t.Errorf("gross for the year %v, want 4 × $4,000", g)
	}
}

// No pay date after a known end is filled; evidence after it counts, and
// the result asks about the date.
func TestKnownEnd(t *testing.T) {
	ended := &scenariosv1.EmployerPart{Id: "acme-end", Part: &scenariosv1.EmployerPart_Ended_{Ended: &scenariosv1.EmployerPart_Ended{LastPaycheck: day(time.July, 31)}}}
	p, j := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", regular("acme-s1", time.August, 14)), ended))
	for _, it := range p.GetItems() {
		if it.GetDate().GetMonth() > 7 && it.GetId() != "employers/acme/2026-08-14/regular" {
			t.Errorf("%s is after the end and isn't evidence", it.GetId())
		}
	}
	if !hasQuestion(j, "employers/acme/ended/acme-end") {
		t.Errorf("questions %v, want one about the end date", questionIDs(j))
	}
}

func ends(id string, on *commonv1.Date, final *scenariosv1.EmployerPart_Ends_FinalPayment) *scenariosv1.EmployerPart {
	return &scenariosv1.EmployerPart{Id: id, Part: &scenariosv1.EmployerPart_Ends_{Ends: &scenariosv1.EmployerPart_Ends{LastPaycheck: on, Final: final}}}
}

// An assumed end stops the pay dates after it and adds the final payment,
// withheld as given; one on or before the latest evidence stands aside.
func TestAssumedEnd(t *testing.T) {
	final := &scenariosv1.EmployerPart_Ends_FinalPayment{Amount: usd(3000), FederalWithheld: usd(660), StateWithheld: usd(100)}
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", regular("acme-s1", time.August, 14)), ends("acme-ends", day(time.October, 15), final)))
	last := p.GetItems()[len(p.GetItems())-1]
	if last.GetId() != "employers/acme/2026-10-15/regular" {
		t.Errorf("the last item is %s, want Oct 15's paycheck", last.GetId())
	}
	f := item(t, p, "employers/acme/2026-10-15/final/acme-ends")
	if cents(f.GetPaycheck().GetFederalWithheld()) != 66000 || cents(f.GetPaycheck().GetStates()[0].GetTaxWithheld()) != 10000 {
		t.Errorf("final payment %v, want $660 and $100 withheld as given", f.GetPaycheck())
	}
	_, j := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", regular("acme-s1", time.August, 14)), ends("acme-ends", day(time.August, 1), nil)))
	if !hasQuestion(j, "employers/acme/stands-aside/acme-ends") {
		t.Errorf("questions %v, want the end standing aside", questionIDs(j))
	}
	// An end on the day of the latest evidence counts: no evidence is after
	// it.
	p, j = expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", regular("acme-s1", time.August, 14)), ends("acme-ends", day(time.August, 14), nil)))
	if last := p.GetItems()[len(p.GetItems())-1]; hasQuestion(j, "employers/acme/stands-aside/acme-ends") || last.GetDate().GetMonth() != 8 {
		t.Errorf("an end on the latest paystub's day stood aside, or left %s", last.GetId())
	}
}

func oneOff(id string, on *commonv1.Date, amount float64) *scenariosv1.EmployerPart {
	return &scenariosv1.EmployerPart{Id: id, Part: &scenariosv1.EmployerPart_OneOff_{OneOff: &scenariosv1.EmployerPart_OneOff{
		Date: on, Kind: factsv1.PaycheckKind_PAYCHECK_KIND_BONUS, Amount: usd(amount)}}}
}

// A bonus is withheld at 22% under the flat method, and 37% on supplemental
// wages over $1 million from the employer in the year.
func TestOneOffFlatSupplemental(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)), oneOff("acme-b1", day(time.December, 1), 10000), oneOff("acme-b2", day(time.December, 2), 1_000_000))
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	if got := cents(item(t, p, "employers/acme/2026-12-01/bonus/acme-b1").GetPaycheck().GetFederalWithheld()); got != 220000 {
		t.Errorf("the $10,000 bonus withholds %v, want 22%%", got)
	}
	// $990,000 at 22% and $10,000 at 37%.
	if got := cents(item(t, p, "employers/acme/2026-12-02/bonus/acme-b2").GetPaycheck().GetFederalWithheld()); got != 21_780_000+370_000 {
		t.Errorf("the $1 million bonus withholds %v, want $221,500", got)
	}
}

// Under the aggregate method a bonus is withheld on itself plus its
// period's paycheck, less that paycheck's own.
func TestOneOffAggregateSupplemental(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)), oneOff("acme-b1", day(time.December, 10), 10000))
	m.Supplemental = scenariosv1.SupplementalMethod_SUPPLEMENTAL_METHOD_AGGREGATE
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	got := cents(item(t, p, "employers/acme/2026-12-10/bonus/acme-b1").GetPaycheck().GetFederalWithheld())
	if got <= 220000 {
		t.Errorf("aggregate withholding %v, want more than the flat 22%% at this pay", got)
	}
}

// Evidence of a bonus within 31 days of a planned one is asked about; both
// count until answered, and answered yes only the evidence counts.
func TestPlannedBonusAgainstActual(t *testing.T) {
	bonus := stub("acme-b9", time.August, 20, factsv1.PaycheckKind_PAYCHECK_KIND_BONUS, &factsv1.PayFigures{Gross: usd(5000)})
	planned := oneOff("acme-plan", day(time.September, 1), 5000)
	p, j := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", regular("acme-s1", time.August, 14), bonus), planned))
	if !hasQuestion(j, "employers/acme/planned/acme-plan/acme-b9") {
		t.Errorf("questions %v, want one asking whether the bonus is the planned one", questionIDs(j))
	}
	item(t, p, "employers/acme/2026-09-01/bonus/acme-plan")
	planned.GetOneOff().Actual = &scenariosv1.Actual{Is: "acme-b9"}
	p, _ = expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", regular("acme-s1", time.August, 14), bonus), planned))
	for _, it := range p.GetItems() {
		if strings.HasSuffix(it.GetId(), "acme-plan") {
			t.Errorf("answered yes, the planned bonus still has an item, %s", it.GetId())
		}
	}
}

// Social Security stops at the wage base for each job; Medicare adds 0.9%
// on the job's wages over $200,000.
func TestSocialSecurityCapAndAdditionalMedicare(t *testing.T) {
	m := job()
	m.DefaultPart = &scenariosv1.EmployerDefault{Id: "acme-default", Rest: &scenariosv1.EmployerDefault_EachPaycheck{EachPaycheck: &factsv1.PayFigures{Gross: usd(20000)}}}
	p, _ := expand(t, ctxAt(t, time.January, 2, nil), m)
	if got := cents(p.GetYear().GetItems().GetSocialSecurityWithheld()); got != 1_143_900 {
		t.Errorf("Social Security withheld %v, want 6.2%% of the $184,500 wage base", got)
	}
	if got := cents(p.GetYear().GetBoxes().GetSocialSecurityWages()); got != 18_450_000 {
		t.Errorf("box 3 %v, want capped at the wage base", got)
	}
	// $480,000 of wages: 1.45% of all, and 0.9% of $280,000.
	if got := cents(p.GetYear().GetItems().GetMedicareWithheld()); got != 696_000+252_000 {
		t.Errorf("Medicare withheld %v, want $9,480", got)
	}
}

// Item totals are always the sum of the items, and box 1 is gross less the
// pre-tax lines.
func TestTotalsAreTheItemsSum(t *testing.T) {
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", regular("acme-s1", time.August, 14)), payRate("acme-r", day(time.November, 1), 7000)))
	var gross, fed money.USCents
	for _, it := range p.GetItems() {
		gross += cents(it.GetPaycheck().GetGross())
		fed += cents(it.GetPaycheck().GetFederalWithheld())
	}
	if gross != cents(p.GetYear().GetItems().GetGross()) || fed != cents(p.GetYear().GetItems().GetFederalWithheld()) {
		t.Errorf("year %v, %v; the items add to %v, %v", p.GetYear().GetItems().GetGross(), p.GetYear().GetItems().GetFederalWithheld(), gross, fed)
	}
	year := p.GetYear()
	pre := money.USCents(0)
	for _, x := range year.GetItems().GetPretax() {
		pre += cents(x.GetAmount())
	}
	// Box 1 is gross less pre-tax, so far and the rest each rounded to the
	// dollar and added.
	if got, want := cents(year.GetBoxes().GetWages()), cents(year.GetItems().GetGross())-pre; got < want-100 || got > want+100 || got%100 != 0 {
		t.Errorf("box 1 %v, want gross less pre-tax, %v, in dollars", year.GetBoxes().GetWages(), want)
	}
	if cents(p.GetRest().GetBoxes().GetWages()) != cents(year.GetBoxes().GetWages())-cents(p.GetSoFar().GetBoxes().GetWages()) {
		t.Error("the rest of the year's box 1 is not the year's less so far")
	}
}

// A job worked across states splits box 16 by days, and its paychecks'
// state lines too.
func TestSplitWorkByDays(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)))
	m.Work = &scenariosv1.WorkPlace{Place: &scenariosv1.WorkPlace_Split_{Split: &scenariosv1.WorkPlace_Split{Days: []*scenariosv1.WorkPlace_Split_StateDays{
		{State: "CA", Days: 150}, {State: "OR", Days: 50}}}}}
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	states := p.GetYear().GetBoxes().GetStates()
	if len(states) != 2 || states[0].GetState() != "CA" || states[1].GetState() != "OR" {
		t.Fatalf("box 16 %v, want CA and OR", states)
	}
	box1 := cents(p.GetYear().GetBoxes().GetWages())
	if ca := cents(states[0].GetWages()); ca < box1*3/4-1 || ca > box1*3/4+1 {
		t.Errorf("California's box 16 %v, want three quarters of box 1, %v", ca, box1)
	}
}

// Without a W-4 federal withholding uses the paystub's filing status alone,
// and with one it reads Step 2 and Step 4(c).
func TestWithholdingReadsTheW4(t *testing.T) {
	f := paycheck()
	f.FederalWithheld = nil
	base := func() *scenariosv1.EmployerModel {
		return job(stubs("acme-p", stub("acme-s1", time.August, 14, factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR, f)))
	}
	fed := func(m *scenariosv1.EmployerModel) money.USCents {
		p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
		return cents(item(t, p, "employers/acme/2026-12-31/regular").GetPaycheck().GetFederalWithheld())
	}
	single := fed(base())
	// $5,333.33 taxable twice a month is $127,999.92 a year; less the
	// $16,100 standard deduction, the 2026 single brackets give $19,453.98,
	// a 24th of which is $810.58.
	if !near(single, 81_058) {
		t.Errorf("single with no W-4 withholds %v, want $810.58", single)
	}
	m := base()
	m.W4 = &scenariosv1.W4{Form: &factsv1.FormW4{FilingStatus: form1040.FilingStatus_FILING_STATUS_MARRIED_FILING_JOINTLY, ExtraWithholding: usd(50)}}
	if got := fed(m); got >= single {
		t.Errorf("married filing jointly withholds %v, want less than single's %v", got, single)
	}
	m.W4.Form.MultipleJobs = true
	if got := fed(m); got <= single/2 {
		t.Errorf("Step 2 checked withholds %v, want the higher table", got)
	}
}

// An override of a figure so far adds an ADJUSTMENT dated so_far_through
// that brings the items to the typed figure.
func TestOverrideAdjustment(t *testing.T) {
	o := &scenariosv1.Override{Path: employer.OverridePrefix("acme") + "federal_withheld", Amount: usd(9000)}
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", regular("acme-s1", time.August, 14))), o)
	adj := item(t, p, "employers/acme/override/federal_withheld")
	if adj.GetOrigin().GetBasis() != scenariosv1.Origin_BASIS_OVERRIDE || cents(adj.GetPaycheck().GetFederalWithheld()) != 830000 {
		t.Errorf("adjustment %v, want $8,300 more", adj)
	}
	if got := cents(p.GetSoFar().GetItems().GetFederalWithheld()); got != 900000 {
		t.Errorf("federal so far %v, want the typed $9,000", got)
	}
}

func TestMaxOutSharesTheLimitAcrossJobs(t *testing.T) {
	facts := &factsv1.TaxYearFacts{People: []*factsv1.Person{{Id: "you", DateOfBirth: &commonv1.Date{Year: 1990, Month: 1, Day: 1}}}}
	ctx := ctxAt(t, time.September, 8, facts)
	a, err := employer.Expand(ctx, job(stubs("acme-p", regular("acme-s1", time.August, 14))), nil)
	if err != nil {
		t.Fatal(err)
	}
	other := job()
	other.Id, other.Name = "beta", "Beta"
	other.DefaultPart = &scenariosv1.EmployerDefault{Id: "beta-default", Rest: &scenariosv1.EmployerDefault_EachPaycheck{EachPaycheck: &factsv1.PayFigures{Gross: usd(300)}}}
	other.Starts = day(time.December, 1)
	b, err := employer.Expand(ctx, other, nil)
	if err != nil {
		t.Fatal(err)
	}
	part := &scenariosv1.RetirementPart{Id: "you-max", Part: &scenariosv1.RetirementPart_MaxOutWorkplacePlan_{MaxOutWorkplacePlan: &scenariosv1.RetirementPart_MaxOutWorkplacePlan{}}}
	if _, err := employer.MaxOut(ctx, "you", part, []*employer.Job{a, b}); err != nil {
		t.Fatal(err)
	}
	pa, err := a.Projection()
	if err != nil {
		t.Fatal(err)
	}
	pb, err := b.Projection()
	if err != nil {
		t.Fatal(err)
	}
	total := cents(pa.GetYear().GetBoxes().GetK401()) + cents(pb.GetYear().GetBoxes().GetK401())
	if total != 2_450_000 {
		t.Errorf("the year's 401(k) across both jobs is %v, want the $24,500 limit", total)
	}
	// Beta's $300 paychecks can't take an equal share; Acme's take the rest.
	if dec := item(t, pb, "employers/beta/2026-12-15/regular"); cents(dec.GetPaycheck().GetPretax()[0].GetAmount()) != 30000 {
		t.Errorf("Beta's Dec 15 401(k) %v, want all $300 of it", dec.GetPaycheck().GetPretax())
	}
}

// A deposit with no paystub before it copies the first paystub after it;
// with none, an assumed PayRate; with neither, its gross is the deposit,
// unwithheld, and the result asks.
func TestDepositTemplateChain(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)), deposits("acme-d", deposit("acme-d1", time.July, 31, 4000)))
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	if got := item(t, p, "employers/acme/2026-07-31/regular"); got.GetOrigin().GetTemplate() != "acme-s1" {
		t.Errorf("template %q, want the first paystub after, acme-s1", got.GetOrigin().GetTemplate())
	}
	m = job(deposits("acme-d", deposit("acme-d1", time.July, 31, 4000)), payRate("acme-r", day(time.October, 1), 6000))
	p, _ = expand(t, ctxAt(t, time.September, 8, nil), m)
	if got := item(t, p, "employers/acme/2026-07-31/regular"); got.GetOrigin().GetTemplate() != "acme-r" || cents(got.GetPaycheck().GetGross()) != 600000 {
		t.Errorf("Jul 31 is %v from %s, want the PayRate's $6,000", got.GetPaycheck().GetGross(), got.GetOrigin().GetTemplate())
	}
	p, j := expand(t, ctxAt(t, time.September, 8, nil), job(deposits("acme-d", deposit("acme-d1", time.July, 31, 4000))))
	got := item(t, p, "employers/acme/2026-07-31/regular")
	if cents(got.GetPaycheck().GetGross()) != 400000 || cents(got.GetPaycheck().GetFederalWithheld()) != 0 || !hasQuestion(j, "employers/acme/deposit/acme-d1") {
		t.Errorf("with no template Jul 31 is %v, want $4,000 gross unwithheld and a question", got.GetPaycheck())
	}
}

// With a year-to-date column the ADJUSTMENT covers the pay dates before
// it, and no copies are made for them.
func TestYearToDateCoversGaps(t *testing.T) {
	s := regular("acme-s1", time.August, 14)
	s.Paystub.YearToDate = &factsv1.PayFigures{Gross: usd(5833.33 * 15)}
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", s)))
	for _, it := range p.GetItems() {
		if it.GetDate().GetMonth() < 8 {
			t.Errorf("a pay date the year-to-date column covers has an item, %s", it.GetId())
		}
	}
}

// Copied Social Security stops at the wage base for each employer.
func TestCopiedSocialSecurityStopsAtTheBase(t *testing.T) {
	f := &factsv1.PayFigures{Gross: usd(20000), SocialSecurityWithheld: usd(1240), MedicareWithheld: usd(290)}
	s := stub("acme-s1", time.January, 15, factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR, f)
	p, _ := expand(t, ctxAt(t, time.January, 20, nil), job(stubs("acme-p", s)))
	if got := cents(p.GetYear().GetItems().GetSocialSecurityWithheld()); got != 1_143_900 {
		t.Errorf("Social Security for the year %v, want 6.2%% of the $184,500 base", got)
	}
}

// A new state is withheld at the latest paystub's state rate, marked
// estimated, and asked about.
func TestNewStateAtThePaystubRate(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)))
	m.Work = &scenariosv1.WorkPlace{Place: &scenariosv1.WorkPlace_State{State: "OR"}}
	p, j := expand(t, ctxAt(t, time.September, 8, nil), m)
	dec := item(t, p, "employers/acme/2026-12-31/regular")
	st := dec.GetPaycheck().GetStates()
	// California's $300 on $5,333.33 of wages.
	if len(st) != 1 || st[0].GetState() != "OR" || cents(st[0].GetTaxWithheld()) != 30000 {
		t.Errorf("Dec 31 states %v, want OR at the paystub's rate", st)
	}
	if !hasQuestion(j, "employers/acme/state-withholding/OR") {
		t.Errorf("questions %v, want one for the new state", questionIDs(j))
	}
}

// The Roth catch-up rule applies only once the plan's roth_option is known:
// unset, the result asks; false, no catch-up is allowed; true, it is Roth.
func TestRothCatchUpNeedsTheRothOption(t *testing.T) {
	facts := &factsv1.TaxYearFacts{People: []*factsv1.Person{{Id: "you", DateOfBirth: &commonv1.Date{Year: 1970, Month: 1, Day: 1}}}}
	part := &scenariosv1.RetirementPart{Id: "you-max", Part: &scenariosv1.RetirementPart_MaxOutWorkplacePlan_{MaxOutWorkplacePlan: &scenariosv1.RetirementPart_MaxOutWorkplacePlan{}}}
	run := func(option *bool) (*scenariosv1.EmployerProjection, []string) {
		ctx := ctxAt(t, time.September, 8, facts)
		m := job(stubs("acme-p", regular("acme-s1", time.August, 14)))
		s := m.GetParts()[0].GetFromPaystubs().GetStubs()[0]
		s.Paystub.YearToDate = &factsv1.PayFigures{Gross: usd(5833.33 * 15), Pretax: []*factsv1.PayFigures_Pretax{{Kind: factsv1.PretaxKind_PRETAX_KIND_401K, Amount: usd(7500)}}}
		m.LastYear = &scenariosv1.LastYearW2{W2: &formw2.FormW2{SocialSecurityWages: usd(160000)}}
		m.RothOption = option
		j, err := employer.Expand(ctx, m, nil)
		if err != nil {
			t.Fatal(err)
		}
		n, err := employer.MaxOut(ctx, "you", part, []*employer.Job{j})
		if err != nil {
			t.Fatal(err)
		}
		p, err := j.Projection()
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, q := range n.Questions() {
			ids = append(ids, q.ID())
		}
		return p, ids
	}
	yes, no := true, false
	p, asked := run(nil)
	if !strings.Contains(strings.Join(asked, " "), "employers/acme/roth-option") {
		t.Errorf("questions %v, want one about the Roth option", asked)
	}
	if got := cents(p.GetYear().GetBoxes().GetK401()); got != 3_250_000 {
		t.Errorf("unanswered, the 401(k) is %v, want the $32,500 limit pre-tax", got)
	}
	if p, _ = run(&no); cents(p.GetYear().GetBoxes().GetK401()) != 2_450_000 {
		t.Errorf("with no Roth option, the 401(k) is %v, want $24,500 and no catch-up", cents(p.GetYear().GetBoxes().GetK401()))
	}
	if p, _ = run(&yes); cents(p.GetYear().GetBoxes().GetRoth_401K()) != 800_000 {
		t.Errorf("with a Roth option, Roth is %v, want the $8,000 catch-up", cents(p.GetYear().GetBoxes().GetRoth_401K()))
	}
}

// near reports whether an item's amount is its exact amount shown in whole
// dollars: items are rounded with a running total, so each is within a
// dollar of what it was.
func near[T ~int64](got, want T) bool { return got%100 == 0 && got-want < 100 && want-got < 100 }

// dollars is an exact total as the projection shows it.
func dollars[T ~int64](v T) T { return T(model.Dollars(money.USCents(v))) }

// Once the year is over, a W-2 states it: the items add up to the form's
// pay, gross being box 1 plus every pre-tax line.
func TestW2StatesTheYear(t *testing.T) {
	ctx, err := model.NewContext(2026, &commonv1.Date{Year: 2027, Month: 3, Day: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	w2 := &formw2.FormW2{TaxYear: 2026, WagesTipsOtherCompensation: usd(120000), FederalTaxWithheld: usd(15000),
		SocialSecurityTaxWithheld: usd(8000), MedicareTaxWithheld: usd(2000),
		CodedAmounts:        []*formw2.FormW2_CodedAmount{{Code: "D", Amount: usd(12000)}},
		OtherItems:          []*formw2.FormW2_OtherItem{{Description: "Other pre-tax benefits", Amount: usd(1000)}},
		StateTaxWithholding: []*commonv1.StateTaxWithholding{{State: "CA", StateIncome: usd(121000), TaxWithheld: usd(6000)}}}
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)),
		&scenariosv1.EmployerPart{Id: "acme-w2", Part: &scenariosv1.EmployerPart_FromW2_{FromW2: &scenariosv1.EmployerPart_FromW2{W2: w2}}})
	p, _ := expand(t, ctx, m)
	y := p.GetYear().GetItems()
	for _, c := range []struct {
		what      string
		got, want money.USCents
	}{
		{"gross", cents(y.GetGross()), 13_300_000},
		{"federal withheld", cents(y.GetFederalWithheld()), 1_500_000},
		{"Social Security withheld", cents(y.GetSocialSecurityWithheld()), 800_000},
		{"Medicare withheld", cents(y.GetMedicareWithheld()), 200_000},
		{"CA withheld", cents(y.GetStates()[0].GetTaxWithheld()), 600_000},
	} {
		if c.got != c.want {
			t.Errorf("the year's %s is %v, want the W-2's %v", c.what, c.got, c.want)
		}
	}
}

// The employer's own HSA is spread over the pay dates, and California, which
// taxes it, counts so far's share in its wages.
func TestEmployerHsaInCaliforniaWages(t *testing.T) {
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)))
	m.EmployerHsa = usd(480)
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), m)
	// 15 of 24 pay dates are through Aug 14: $480 × 15/24 is $300.
	if got := cents(p.GetSoFar().GetBoxes().GetHsa()); got != 30_000 {
		t.Errorf("so far's HSA box is %v, want the employer's $300", got)
	}
	items, boxes := cents(p.GetSoFar().GetItems().GetStates()[0].GetStateIncome()), cents(p.GetSoFar().GetBoxes().GetStates()[0].GetWages())
	if boxes-items != 30_000 {
		t.Errorf("so far's California wages are %v on items of %v, want $300 more", boxes, items)
	}
}

// HSA through payroll over the limit comes off the paychecks to come, so it
// is in their wages; what is over on paychecks already made is income.
// California's wage lines stay, as they count HSA already.
func TestHoldHsa(t *testing.T) {
	f := paycheck()
	f.Pretax = append(f.Pretax, &factsv1.PayFigures_Pretax{Kind: factsv1.PretaxKind_PRETAX_KIND_HSA, Amount: usd(400)})
	// Social Security and Medicare on the $5,433.33 the HSA leaves.
	f.SocialSecurityWithheld, f.MedicareWithheld = usd(336.87), usd(78.78)
	m := job(stubs("acme-p", stub("acme-s1", time.August, 14, factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR, f)))
	ctx := ctxAt(t, time.September, 8, nil)
	before, _ := expand(t, ctx, m)
	j, err := employer.Expand(ctx, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 24 paychecks put in $9,600 against a $4,400 limit: $5,200 over. Only
	// Aug 14 is made; the 23 pay dates without evidence are to come, and
	// hold all of it.
	if err := employer.HoldHsa("you", money.Dollars(4_400), []*employer.Job{j}); err != nil {
		t.Fatal(err)
	}
	p, err := j.Projection()
	if err != nil {
		t.Fatal(err)
	}
	if cents(p.GetHsaOverInWages()) != 520_000 || cents(p.GetHsaOverInIncome()) != 0 || cents(p.GetHsaOver()) != 520_000 {
		t.Errorf("over %v in wages %v in income %v, want $5,200 in wages", p.GetHsaOver(), p.GetHsaOverInWages(), p.GetHsaOverInIncome())
	}
	if got := cents(p.GetYear().GetBoxes().GetWages()) - cents(before.GetYear().GetBoxes().GetWages()); got != 520_000 {
		t.Errorf("box 1 rose %v, want the $5,200 taken off the paychecks to come", got)
	}
	if a, b := cents(p.GetYear().GetBoxes().GetStates()[0].GetWages()), cents(before.GetYear().GetBoxes().GetStates()[0].GetWages()); a != b {
		t.Errorf("California's wages moved from %v to %v; they count HSA already", b, a)
	}
	if len(j.Working()) == 0 {
		t.Error("the excess has no working")
	}
	// Social Security and Medicare wages rise by the excess too, and their
	// tax is worked out on them. The cut runs from Dec 31 back through the
	// four pay dates before the paystub, Jun 15 to Jul 31, which copy it: on
	// Jul 31, its HSA line cut to nothing, it is 6.2% and 1.45% of the whole
	// $5,833.33, not the paystub's figures on $5,433.33.
	jul := item(t, p, "employers/acme/2026-07-31/regular").GetPaycheck()
	if ss, med := cents(jul.GetSocialSecurityWithheld()), cents(jul.GetMedicareWithheld()); !near(ss, 36_167) || !near(med, 8_458) {
		t.Errorf("Jul 31 Social Security %v and Medicare %v, want about $361.67 and $84.58", ss, med)
	}
}

// An excess beyond what the paychecks to come hold is on paychecks already
// made: other income.
func TestHoldHsaOnPaychecksMade(t *testing.T) {
	f := paycheck()
	f.Pretax = append(f.Pretax, &factsv1.PayFigures_Pretax{Kind: factsv1.PretaxKind_PRETAX_KIND_HSA, Amount: usd(400)})
	var ss []*scenariosv1.EmployerPart_FromPaystubs_Stub
	for _, d := range []struct {
		m time.Month
		d int
	}{{time.January, 15}, {time.January, 30}, {time.February, 13}, {time.February, 27}, {time.March, 13}, {time.March, 31}, {time.April, 15},
		{time.April, 30}, {time.May, 15}, {time.May, 29}, {time.June, 15}, {time.June, 30}, {time.July, 15}, {time.July, 31}, {time.August, 14}} {
		ss = append(ss, stub(fmt.Sprintf("acme-s%d-%d", d.m, d.d), d.m, d.d, factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR, f))
	}
	j, err := employer.Expand(ctxAt(t, time.September, 8, nil), job(stubs("acme-p", ss...)), nil)
	if err != nil {
		t.Fatal(err)
	}
	// 15 paychecks made hold $6,000 and 9 to come $3,600: $5,200 over a
	// $4,400 limit, $3,600 of it in wages and $1,600 income.
	if err := employer.HoldHsa("you", money.Dollars(4_400), []*employer.Job{j}); err != nil {
		t.Fatal(err)
	}
	p, err := j.Projection()
	if err != nil {
		t.Fatal(err)
	}
	if cents(p.GetHsaOverInWages()) != 360_000 || cents(p.GetHsaOverInIncome()) != 160_000 {
		t.Errorf("in wages %v and in income %v, want $3,600 and $1,600", p.GetHsaOverInWages(), p.GetHsaOverInIncome())
	}
}

// TestMoveToAStateWithNoWageTax: living in California through August, then
// Texas, which taxes no wages. The paychecks from September have Texas
// lines with their wages, rounded to the dollar, and nothing withheld, so
// Texas has the eight paychecks' wages; California's line has its employer
// HSA, $0.
func TestMoveToAStateWithNoWageTax(t *testing.T) {
	facts := &factsv1.TaxYearFacts{Residence: []*factsv1.ResidencePeriod{
		{Id: "ca", State: "CA", From: day(time.January, 1), To: day(time.August, 31)},
		{Id: "tx", State: "TX", From: day(time.September, 1), To: day(time.December, 31)},
	}}
	m := job(stubs("acme-p", regular("acme-s1", time.August, 14)))
	m.Work = &scenariosv1.WorkPlace{Place: &scenariosv1.WorkPlace_WhereLived_{WhereLived: &scenariosv1.WorkPlace_WhereLived{}}}
	p, _ := expand(t, ctxAt(t, time.September, 8, facts), m)
	dec := item(t, p, "employers/acme/2026-12-31/regular").GetPaycheck()
	var tx *commonv1.StateTaxWithholding
	for _, s := range dec.GetStates() {
		if s.GetState() == "TX" {
			tx = s
		}
	}
	if tx == nil || !near(cents(tx.GetStateIncome()), 533_333) || tx.GetTaxWithheld() == nil || cents(tx.GetTaxWithheld()) != 0 {
		t.Fatalf("Dec 31 paycheck's Texas line %v, want about $5,333.33 of wages and $0 withheld", tx)
	}
	var restTX, caHsa *scenariosv1.W2Boxes_State
	for _, s := range p.GetRest().GetBoxes().GetStates() {
		if s.GetState() == "TX" {
			restTX = s
		}
	}
	for _, s := range p.GetYear().GetBoxes().GetStates() {
		if s.GetState() == "CA" {
			caHsa = s
		}
	}
	if restTX == nil || !near(cents(restTX.GetWages()), 8*533_333) {
		t.Errorf("the rest's Texas wages %v, want the eight paychecks from September, about $42,666.64", restTX)
	}
	if caHsa.GetEmployerHsa() == nil || cents(caHsa.GetEmployerHsa()) != 0 {
		t.Errorf("California's year line %v, want its employer HSA set at $0", caHsa)
	}
}

// TestYearTotalCaliforniaFollowsBox1: a year total with $6,000 of HSA
// through payroll and a California line from before, $10,000 short. A
// part has no field for California wages, so the year's
// California wages are box 1 and the payroll HSA, not the stale line.
func TestYearTotalCaliforniaFollowsBox1(t *testing.T) {
	year := &factsv1.PayFigures{Gross: usd(140_000), Pretax: []*factsv1.PayFigures_Pretax{
		{Kind: factsv1.PretaxKind_PRETAX_KIND_401K, Amount: usd(12_000)}, {Kind: factsv1.PretaxKind_PRETAX_KIND_HSA, Amount: usd(6_000)}},
		States: []*commonv1.StateTaxWithholding{{State: "CA", StateIncome: usd(112_000)}}}
	yt := &scenariosv1.EmployerPart{Id: "acme-yt", Part: &scenariosv1.EmployerPart_YearTotal_{YearTotal: &scenariosv1.EmployerPart_YearTotal{
		Year: year, TypedAgainst: day(time.August, 14)}}}
	p, _ := expand(t, ctxAt(t, time.September, 8, nil), job(stubs("acme-p", regular("acme-s1", time.August, 14)), yt))
	y := p.GetYear()
	var hsa money.USCents
	for _, x := range y.GetItems().GetPretax() {
		if x.GetKind() == factsv1.PretaxKind_PRETAX_KIND_HSA {
			hsa += cents(x.GetAmount())
		}
	}
	ca := y.GetBoxes().GetStates()[0]
	if want := cents(y.GetBoxes().GetWages()) + hsa; ca.GetState() != "CA" || cents(ca.GetWages()) != want {
		t.Errorf("California's wages for the year %v, want box 1 and the payroll HSA, %v", ca.GetWages(), want)
	}
}
