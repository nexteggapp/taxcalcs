package golden

// The Go tests of taxyear's own package that build households in code,
// models_test.go, new_forms_test.go and property_test.go, copied here as
// fixtures: each Compute they make is recorded, with its file, in
// testdata/taxyear/fixtures.json.gz (the first eight of each property
// test's 200 households), and the Rust tests compute the same files. The
// tests' functions are renamed fixtureX from TestX, and their helpers
// from taxyear_test.go take a "ty" prefix, as the golden package holds
// others of the same names.

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"strings"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	usv1 "github.com/nexteggapp/taxcalcs/us/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	form1099b "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_b"
	form1099c "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_c"
	form1099misc "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_misc"
	form1099nec "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_nec"
	form1099oid "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_oid"
	form1099q "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_q"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	form1099s "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_s"
	form1099sa "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_sa"
	form5498 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_5498"
	form5498sa "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_5498_sa"
	schedulek1 "github.com/nexteggapp/taxprotos/go/us/federal/v1/schedule_k1"
	form540 "github.com/nexteggapp/taxprotos/go/us/state/ca/v1/form_540"
)

// tyFixture is one Compute a fixture made: the file and the result.
type tyFixture struct {
	Test   string `json:"test"`
	File   string `json:"file"`
	Result string `json:"result"`
	Error  string `json:"error"`
}

var (
	tyMu       sync.Mutex
	tyRecorded []tyFixture
	tyCounts   = map[string]int{}
)

// recordCompute is taxyear.Compute, recording the file and the result
// under the test's name: at most eight a test.
func recordCompute(t *testing.T, f *scenariosv1.TaxYearScenarios) (taxyear.Result, error) {
	r, err := taxyear.Compute(f)
	tyMu.Lock()
	defer tyMu.Unlock()
	name := strings.SplitN(t.Name(), "/", 3)[1]
	if tyCounts[name] < 8 {
		tyCounts[name]++
		v := tyFixture{Test: name, File: encode(t, f), Error: errText(err)}
		if err == nil {
			v.Result = encode(t, r.Message())
		}
		tyRecorded = append(tyRecorded, v)
	}
	return r, err
}

// tyExample is docs/scenarios.md's JSONC example.
func tyExample() []byte {
	b, err := os.ReadFile("../taxyear/testdata/example.jsonc")
	if err != nil {
		panic(err)
	}
	return b
}

func TestTaxYearFixtures(t *testing.T) {
	t.Run("OtherModels", fixtureOtherModels)
	t.Run("DistributionWithoutCodeIsLeftOut", fixtureDistributionWithoutCodeIsLeftOut)
	t.Run("PaceByCadence", fixturePaceByCadence)
	t.Run("RegisterAsOfJanuary", fixtureRegisterAsOfJanuary)
	t.Run("PaymentWindow", fixturePaymentWindow)
	t.Run("NewInputs", fixtureNewInputs)
	t.Run("OverrideWas", fixtureOverrideWas)
	t.Run("CarriesForwardAndRefundYear", fixtureCarriesForwardAndRefundYear)
	t.Run("FormReconcilesToItsPayersReceipts", fixtureFormReconcilesToItsPayersReceipts)
	t.Run("StateTaxFromFiledReturn", fixtureStateTaxFromFiledReturn)
	t.Run("BrokerTakesThePrintedBox", fixtureBrokerTakesThePrintedBox)
	t.Run("ForeignTaxIsNotPaced", fixtureForeignTaxIsNotPaced)
	t.Run("PlannedPaymentThatFallsShort", fixturePlannedPaymentThatFallsShort)
	t.Run("ClosedAccountStopsPaying", fixtureClosedAccountStopsPaying)
	t.Run("StateRefundTaxBenefit", fixtureStateRefundTaxBenefit)
	t.Run("StateRefundWithNoPriorYear", fixtureStateRefundWithNoPriorYear)
	t.Run("StateRefundFromLine12", fixtureStateRefundFromLine12)
	t.Run("Form1099OIDInAccountModel", fixtureForm1099OIDInAccountModel)
	t.Run("Form1099MISCInBusinessModel", fixtureForm1099MISCInBusinessModel)
	t.Run("NewFormsInOtherIncomeModel", fixtureNewFormsInOtherIncomeModel)
	t.Run("NewFormsInRetirementModel", fixtureNewFormsInRetirementModel)
	t.Run("TotalsEqualItems", fixtureTotalsEqualItems)
	t.Run("IDsAreStableAcrossScenarios", fixtureIDsAreStableAcrossScenarios)
	t.Run("EvidenceIsNeverRefused", fixtureEvidenceIsNeverRefused)
	checkGzip(t, "taxyear/fixtures.json.gz", tyRecorded)
}

// Keeps imports the copied tests may not all use.
var (
	_ = fmt.Sprintf
	_ = math.Round
	_ = rand.New
	_ = proto.Clone
)

// From taxyear/models_test.go.

// household is one person, single, in California all year, as of Sep 8,
// 2026, with one model of each kind but employers.
func tyOthers() *scenariosv1.TaxYearScenarios {
	facts := &factsv1.TaxYearFacts{TaxYear: 2026,
		People:    []*factsv1.Person{{Id: "you", DateOfBirth: &commonv1.Date{Year: 1980, Month: 1, Day: 1}, Relationship: factsv1.Relationship_RELATIONSHIP_YOU}},
		Residence: []*factsv1.ResidencePeriod{{Id: "r1", State: "CA", From: tyDate(1, 1)}},
		Elections: []*factsv1.Election{{Id: "fs", Choice: &factsv1.Election_FilingStatus{FilingStatus: form1040.FilingStatus_FILING_STATUS_SINGLE}}},
	}
	credit := func(id string, m int32, v float64) *scenariosv1.AccountPart_FromCredits_Credit {
		return &scenariosv1.AccountPart_FromCredits_Credit{Id: id, Date: tyDate(m, 28), Amount: tyUsd(v), Kind: scenariosv1.CreditKind_CREDIT_KIND_INTEREST}
	}
	var credits []*scenariosv1.AccountPart_FromCredits_Credit
	for m := int32(1); m <= 8; m++ {
		credits = append(credits, credit("ally-c"+string(rune('0'+m)), m, 100))
	}
	model := &scenariosv1.TaxYearModel{
		Accounts: []*scenariosv1.AccountModel{{Id: "ally", Name: "Ally",
			Parts:       []*scenariosv1.AccountPart{{Id: "ally-p1", Part: &scenariosv1.AccountPart_FromCredits_{FromCredits: &scenariosv1.AccountPart_FromCredits{Credits: credits}}}},
			DefaultPart: &scenariosv1.AccountDefault{Id: "ally-d", Rest: &scenariosv1.AccountDefault_PaceSoFar_{PaceSoFar: &scenariosv1.AccountDefault_PaceSoFar{}}}}},
		Businesses: []*scenariosv1.BusinessModel{{Id: "biz", Name: "Biz", Person: "you",
			Parts: []*scenariosv1.BusinessPart{{Id: "biz-p1", Part: &scenariosv1.BusinessPart_FromLedger_{FromLedger: &scenariosv1.BusinessPart_FromLedger{Entries: []*scenariosv1.BusinessPart_FromLedger_Entry{
				{Id: "biz-e1", Date: tyDate(3, 10), Amount: tyUsd(8000), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT},
				{Id: "biz-e2", Date: tyDate(4, 10), Amount: tyUsd(800), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE}}}}},
				{Id: "biz-m", Part: &scenariosv1.BusinessPart_Monthly_{Monthly: &scenariosv1.BusinessPart_Monthly{Receipts: tyUsd(1000), Expenses: tyUsd(100)}}}},
			DefaultPart: &scenariosv1.BusinessDefault{Id: "biz-d", Rest: &scenariosv1.BusinessDefault_PaceSoFar_{PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{}}}}},
		Brokers: []*scenariosv1.BrokerModel{{Id: "schwab", Name: "Schwab",
			Holdings: []*scenariosv1.Holding{{Id: "vti", Symbol: "VTI", Price: tyUsd(300), Lots: []*scenariosv1.Lot{
				{Id: "vti-1", Acquired: &commonv1.Date{Year: 2020, Month: 1, Day: 1}, Shares: &commonv1.Quantity{Units: 10}, Basis: tyUsd(1000)},
				{Id: "vti-2", Acquired: tyDate(6, 1), Shares: &commonv1.Quantity{Units: 10}, Basis: tyUsd(2500)}}}},
			Parts: []*scenariosv1.BrokerPart{
				{Id: "schwab-t", Part: &scenariosv1.BrokerPart_FromTrades_{FromTrades: &scenariosv1.BrokerPart_FromTrades{Trades: []*scenariosv1.BrokerPart_FromTrades_Trade{
					{Id: "schwab-t1", Holding: "vti", Date: tyDate(7, 1), Shares: &commonv1.Quantity{Units: 15}, Proceeds: tyUsd(4500)}}}}},
				{Id: "schwab-g", Part: &scenariosv1.BrokerPart_PlannedGain_{PlannedGain: &scenariosv1.BrokerPart_PlannedGain{Date: tyDate(11, 1), Term: scenariosv1.GainTerm_GAIN_TERM_SHORT, Amount: tyUsd(-500)}}}}}},
		Itemised: []*scenariosv1.ItemisedModel{
			{Id: "gifts", Name: "Gifts", Kind: &scenariosv1.ItemisedModel_Charity{Charity: &scenariosv1.CharityModel{
				LastYear: []*scenariosv1.LastYearGift{{Date: &commonv1.Date{Year: 2025, Month: 3, Day: 1}, Amount: tyUsd(50)}, {Date: &commonv1.Date{Year: 2025, Month: 12, Day: 15}, Amount: tyUsd(1200)}},
				Parts:    []*scenariosv1.CharityPart{{Id: "gifts-l", Part: &scenariosv1.CharityPart_LikeLastYear_{LikeLastYear: &scenariosv1.CharityPart_LikeLastYear{}}}}}}},
			{Id: "home", Name: "Home", Kind: &scenariosv1.ItemisedModel_PropertyTax{PropertyTax: &scenariosv1.PropertyTaxModel{
				DueDates: []*commonv1.Date{tyDate(4, 10), tyDate(12, 10)},
				Parts: []*scenariosv1.PropertyTaxPart{{Id: "home-p", Part: &scenariosv1.PropertyTaxPart_PaidFromBank_{PaidFromBank: &scenariosv1.PropertyTaxPart_PaidFromBank{Payments: []*scenariosv1.PropertyTaxPart_PaidFromBank_Payment{
					{Id: "home-1", Date: tyDate(4, 9), Amount: tyUsd(3000)}}}}},
					{Id: "home-y", Part: &scenariosv1.PropertyTaxPart_YearTotal_{YearTotal: &scenariosv1.PropertyTaxPart_YearTotal{Total: tyUsd(6400)}}}},
				DefaultPart: &scenariosv1.PropertyTaxDefault{Id: "home-d", Rest: &scenariosv1.PropertyTaxDefault_LikeLastYear_{LikeLastYear: &scenariosv1.PropertyTaxDefault_LikeLastYear{}}}}}},
		},
		OtherIncome: []*scenariosv1.OtherIncomeModel{{Id: "edd", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNEMPLOYMENT, Payer: "EDD",
			Parts: []*scenariosv1.OtherIncomePart{{Id: "edd-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{Gross: tyUsd(1200), FederalWithheld: tyUsd(120)}}}}}},
		Retirement: []*scenariosv1.RetirementModel{{Id: "you-r", Person: "you", Parts: []*scenariosv1.RetirementPart{
			{Id: "you-ira", Part: &scenariosv1.RetirementPart_IraContribution_{IraContribution: &scenariosv1.RetirementPart_IraContribution{Date: tyDate(12, 1), Kind: scenariosv1.IraKind_IRA_KIND_TRADITIONAL, Amount: tyUsd(1000)}}}}}},
		TaxPaid: []*scenariosv1.TaxPaidModel{{Id: "us", Jurisdiction: "US", Parts: []*scenariosv1.TaxPaidPart{
			{Id: "us-q1", Part: &scenariosv1.TaxPaidPart_PaymentMade_{PaymentMade: &scenariosv1.TaxPaidPart_PaymentMade{Date: tyDate(4, 15), Amount: tyUsd(1000),
				Purpose: &factsv1.PaymentPurpose{Purpose: &factsv1.PaymentPurpose_EstimatedFor{EstimatedFor: 2026}}}}}},
			DefaultPart: &scenariosv1.TaxPaidDefault{Id: "us-d", Rest: &scenariosv1.TaxPaidDefault_SpreadTheRest_{SpreadTheRest: &scenariosv1.TaxPaidDefault_SpreadTheRest{}}}}},
	}
	return &scenariosv1.TaxYearScenarios{SchemaVersion: 1, TaxYear: 2026, AsOf: tyDate(9, 8),
		Shared:    &scenariosv1.ScenarioInputs{Facts: facts, Model: model},
		Scenarios: []*scenariosv1.Scenario{{Id: "plan", Name: "Your plan"}}}
}

func fixtureOtherModels(t *testing.T) {
	r, err := recordCompute(t, tyOthers())
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.Scenario("plan")
	if p := plan.Proto().GetProblems(); len(p) > 0 {
		t.Fatalf("problems: %v", p)
	}
	for _, c := range []struct {
		path string
		want int64
	}{
		// Eight months at $100, then September to December at the pace so
		// far.
		{"projection.accounts[ally].year.boxes.interest", 120_000},
		{"projection.accounts[ally].items[accounts/ally/2026-09-28/interest].account.interest", 10_000},
		// $8,000 of receipts and $800 of expenses so far; Monthly from the
		// day after the evidence, September, adds $1,000 and $100 a month.
		{"projection.businesses[biz].year.boxes.net_profit", 720_000 + 4*90_000},
		// 15 shares first in, first out: the 2020 lot's 10, long-term, and 5
		// of June's, short-term; then a planned $500 short-term loss.
		{"projection.brokers[schwab].year.items.long_term_gain", 300_000 - 100_000},
		{"projection.brokers[schwab].year.items.short_term_gain", 150_000 - 125_000 - 50_000},
		// LikeLastYear repeats the Dec 15 gift, not March's, which is past.
		{"projection.itemised[gifts].year.boxes.gifts_by_cash", 120_000},
		// The $6,400 total less the $3,000 paid, on the Dec 10 installment.
		{"projection.itemised[home].year.boxes.real_estate_taxes", 640_000},
		{"projection.other_income[edd].year.items.gross", 120_000},
		{"projection.retirement[you-r].year.items.traditional_ira", 100_000},
		{"federal.schedule_1.unemployment_compensation", 120_000},
		{"federal.form_1040.withheld_from_1099", 12_000},
	} {
		if got := tyFigure(t, r, "plan", c.path); got != c.want {
			t.Errorf("%s = %d, want %d", c.path, got, c.want)
		}
	}
	us := plan.Proto().GetEstimated()[0]
	if us.GetJurisdiction() != "US" || us.GetRules().GetOutcome().String() != "ESTIMATED_OUTCOME_COMPUTED" {
		t.Fatalf("US estimated %v", us)
	}
	// SpreadTheRest fills the open due dates after as_of.
	var spread int
	for _, it := range plan.Proto().GetProjection().GetTaxPaid()[0].GetItems() {
		if it.GetOrigin().GetPart() == "us-d" {
			spread++
		}
	}
	req := us.GetRules().GetRequirement()
	if req.GetRequired() && spread == 0 {
		t.Errorf("payments are required, target %d, and SpreadTheRest added none", req.GetTargetCents())
	}
}

// A 1099-R whose box 7 code is blank can't be taxed by the rules. It is
// left out of lines 4 and 5, and the return and every other income row's
// summary are still computed.
func fixtureDistributionWithoutCodeIsLeftOut(t *testing.T) {
	f := tyOthers()
	f.GetShared().GetModel().OtherIncome = append(f.GetShared().GetModel().OtherIncome, &scenariosv1.OtherIncomeModel{
		Id: "fund", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS, Payer: "Fund",
		Parts: []*scenariosv1.OtherIncomePart{{Id: "fund-f", Part: &scenariosv1.OtherIncomePart_FromForms_{FromForms: &scenariosv1.OtherIncomePart_FromForms{
			Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{Id: "fund-r", Source: &factsv1.DocumentRef{Name: "1099-R"},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099R{Form_1099R: &form1099r.Form1099R{TaxYear: 2026,
					GrossDistribution: tyUsd(5000), TaxableAmount: tyUsd(5000), FederalTaxWithheld: tyUsd(500), DistributionCodes: []string{""}}}}}}}}}})
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.Scenario("plan")
	if p := plan.Proto().GetProblems(); len(p) > 0 {
		t.Fatalf("problems: %v", p)
	}
	rows := map[string]*scenariosv1.OtherIncomeSummary{}
	for _, s := range plan.Proto().GetSummary().GetOtherIncome() {
		rows[s.GetId()] = s
	}
	if rows["edd"].GetTaxable().GetMinor() != 120_000 {
		t.Errorf("edd summary %v, want $1,200 taxable", rows["edd"])
	}
	fund := rows["fund"]
	if fund == nil || fund.Taxable != nil || fund.GetWithheld().GetMinor() != 50_000 {
		t.Errorf("fund summary %v, want its $500 withheld and no taxable amount", fund)
	}
	if got := tyFigure(t, r, "plan", "federal.form_1040.withheld_from_1099"); got != 62_000 {
		t.Errorf("withheld from 1099s %d, want $620", got)
	}
	if got := plan.Proto().GetFederal().GetForm_1040().GetPensionsAndAnnuitiesTaxable().GetMinor(); got != 0 {
		t.Errorf("taxable pensions %d, want the distribution left out", got)
	}
	var left bool
	for _, n := range plan.Proto().GetNotIncluded() {
		left = left || strings.Contains(n.GetText().GetText(), "box 7 code")
	}
	if !left {
		t.Errorf("not_included %v, want the distribution without a code", plan.Proto().GetNotIncluded())
	}
}

// The pace so far recurs at the median gap between credits, at their
// average: quarterly dividends on Mar 31 and Jun 30 recur Sep 30 and Dec 31;
// a single Treasury credit is annual, so recurs next year. A business whose
// entries fall in Jan, Apr and Jul recurs in Oct only.
func fixturePaceByCadence(t *testing.T) {
	f := tyOthers()
	m := f.GetShared().GetModel()
	credit := func(id string, mo, d int32, v float64, k scenariosv1.CreditKind) *scenariosv1.AccountPart_FromCredits_Credit {
		return &scenariosv1.AccountPart_FromCredits_Credit{Id: id, Date: tyDate(mo, d), Amount: tyUsd(v), Kind: k}
	}
	m.Accounts = append(m.Accounts, &scenariosv1.AccountModel{Id: "fund", Name: "Fund",
		Parts: []*scenariosv1.AccountPart{{Id: "fund-p", Part: &scenariosv1.AccountPart_FromCredits_{FromCredits: &scenariosv1.AccountPart_FromCredits{Credits: []*scenariosv1.AccountPart_FromCredits_Credit{
			credit("fund-1", 3, 31, 40, scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND),
			credit("fund-2", 6, 30, 60, scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND),
			credit("fund-3", 5, 14, 300, scenariosv1.CreditKind_CREDIT_KIND_TREASURY_INTEREST)}}}}},
		DefaultPart: &scenariosv1.AccountDefault{Id: "fund-d", Rest: &scenariosv1.AccountDefault_PaceSoFar_{PaceSoFar: &scenariosv1.AccountDefault_PaceSoFar{}}}})
	entry := func(id string, mo int32) *scenariosv1.BusinessPart_FromLedger_Entry {
		return &scenariosv1.BusinessPart_FromLedger_Entry{Id: id, Date: tyDate(mo, 10), Amount: tyUsd(3000), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT}
	}
	m.Businesses = []*scenariosv1.BusinessModel{{Id: "biz", Name: "Biz", Person: "you",
		Parts:       []*scenariosv1.BusinessPart{{Id: "biz-p1", Part: &scenariosv1.BusinessPart_FromLedger_{FromLedger: &scenariosv1.BusinessPart_FromLedger{Entries: []*scenariosv1.BusinessPart_FromLedger_Entry{entry("biz-1", 1), entry("biz-2", 4), entry("biz-3", 7)}}}}},
		DefaultPart: &scenariosv1.BusinessDefault{Id: "biz-d", Rest: &scenariosv1.BusinessDefault_PaceSoFar_{PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{}}}}}
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		path string
		want int64
	}{
		{"projection.accounts[fund].items[accounts/fund/2026-09-30/dividend].account.ordinary_dividends", 5_000},
		{"projection.accounts[fund].items[accounts/fund/2026-12-31/dividend].account.ordinary_dividends", 5_000},
		{"projection.accounts[fund].year.items.treasury_interest", 30_000},
		{"projection.businesses[biz].items[businesses/biz/2026-10-31/month].business.receipts", 300_000},
		{"projection.businesses[biz].year.boxes.gross_receipts", 1_200_000},
	} {
		if got := tyFigure(t, r, "plan", c.path); got != c.want {
			t.Errorf("%s = %d, want %d", c.path, got, c.want)
		}
	}
}

// The broker register is as of January 1: a purchase during the year adds
// a lot, and a sale takes the earliest lots first, the purchase's among
// them, at the register's basis.
func fixtureRegisterAsOfJanuary(t *testing.T) {
	f := tyOthers()
	b := f.GetShared().GetModel().GetBrokers()[0]
	b.Parts = []*scenariosv1.BrokerPart{
		{Id: "schwab-b", Part: &scenariosv1.BrokerPart_Purchases_{Purchases: &scenariosv1.BrokerPart_Purchases{Purchases: []*scenariosv1.BrokerPart_Purchases_Purchase{
			{Id: "schwab-buy", Holding: "vti", Date: tyDate(2, 1), Shares: &commonv1.Quantity{Units: 5}, Cost: tyUsd(1500)}}}}},
		{Id: "schwab-t", Part: &scenariosv1.BrokerPart_FromTrades_{FromTrades: &scenariosv1.BrokerPart_FromTrades{Trades: []*scenariosv1.BrokerPart_FromTrades_Trade{
			{Id: "schwab-t1", Holding: "vti", Date: tyDate(7, 1), Shares: &commonv1.Quantity{Units: 22}, Proceeds: tyUsd(6600)}}}}},
	}
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	// 22 shares: the 2020 lot's 10, long-term ($3,000 on $1,000); then the
	// Feb 1 purchase's 5 ($1,500 on $1,500) and 7 of June's 10 ($2,100 on
	// $1,750), short-term.
	if got := tyFigure(t, r, "plan", "projection.brokers[schwab].year.items.long_term_gain"); got != 200_000 {
		t.Errorf("long-term gain %d, want $2,000", got)
	}
	if got := tyFigure(t, r, "plan", "projection.brokers[schwab].year.items.short_term_gain"); got != 35_000 {
		t.Errorf("short-term gain %d, want $350", got)
	}
}

// A property tax payment fills the nearest installment whose due date it
// precedes by no more than 120 days; one earlier is a question, and counts
// as a payment of its own.
func fixturePaymentWindow(t *testing.T) {
	f := tyOthers()
	home := f.GetShared().GetModel().GetItemised()[1].GetPropertyTax()
	home.Parts = []*scenariosv1.PropertyTaxPart{{Id: "home-p", Part: &scenariosv1.PropertyTaxPart_PaidFromBank_{PaidFromBank: &scenariosv1.PropertyTaxPart_PaidFromBank{Payments: []*scenariosv1.PropertyTaxPart_PaidFromBank_Payment{
		{Id: "home-1", Date: tyDate(1, 2), Amount: tyUsd(3000)},
		{Id: "home-2", Date: tyDate(8, 12), Amount: tyUsd(3000)}}}}}}
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.Scenario("plan")
	var ids, asked []string
	for _, it := range plan.Proto().GetProjection().GetItemised()[1].GetItems() {
		ids = append(ids, it.GetId())
	}
	for _, q := range plan.Proto().GetQuestions() {
		asked = append(asked, q.GetId())
	}
	got := strings.Join(ids, " ")
	// Jan 2 is 98 days before Apr 10; Aug 12 is 120 days before Dec 10.
	if !strings.Contains(got, "itemised/home/2026-04-10/installment") || !strings.Contains(got, "itemised/home/2026-12-10/installment") {
		t.Errorf("items %s, want both installments filled", got)
	}
	home.Parts[0].GetPaidFromBank().Payments[1].Date = tyDate(8, 11)
	r, _ = recordCompute(t, f)
	plan, _ = r.Scenario("plan")
	asked = nil
	for _, q := range plan.Proto().GetQuestions() {
		asked = append(asked, q.GetId())
	}
	if !strings.Contains(strings.Join(asked, " "), "itemised/home/payment/home-2") {
		t.Errorf("questions %v, want one about the Aug 11 payment, 121 days early", asked)
	}
}

// TestNewInputs checks the inputs that let the file alone reproduce every
// figure: a trade's reported basis, RestOfYear, a mortgage's bank
// payments, last year's qualified share, a one-off business expense, a
// rental's Schedule E lines, and an override's was.
func fixtureNewInputs(t *testing.T) {
	f := tyOthers()
	m := f.GetShared().GetModel()
	trades := m.GetBrokers()[0].GetParts()[0].GetFromTrades().GetTrades()
	trades[0].Basis, trades[0].Acquired = tyUsd(4000), &commonv1.Date{Year: 2020, Month: 1, Day: 1}
	trades[0].WashSaleDisallowed, trades[0].Box = tyUsd(100), scenariosv1.Form8949Box_FORM_8949_BOX_E
	m.Itemised = append(m.Itemised, &scenariosv1.ItemisedModel{Id: "loan", Name: "Loan", Kind: &scenariosv1.ItemisedModel_Mortgage{Mortgage: &scenariosv1.MortgageModel{
		Parts: []*scenariosv1.MortgagePart{
			{Id: "loan-s", Part: &scenariosv1.MortgagePart_FromStatements_{FromStatements: &scenariosv1.MortgagePart_FromStatements{Statements: []*scenariosv1.MortgagePart_FromStatements_Statement{
				{Id: "loan-s1", Statement: &factsv1.MortgageStatement{Date: tyDate(6, 1), Interest: tyUsd(1000)}}}}}},
			{Id: "loan-b", Part: &scenariosv1.MortgagePart_PaidFromBank_{PaidFromBank: &scenariosv1.MortgagePart_PaidFromBank{Payments: []*scenariosv1.MortgagePart_PaidFromBank_Payment{
				{Id: "loan-p7", Date: tyDate(7, 1), Amount: tyUsd(2500)}, {Id: "loan-p8", Date: tyDate(8, 1), Amount: tyUsd(2500)}}}}},
			{Id: "loan-r", Part: &scenariosv1.MortgagePart_RestOfYear{RestOfYear: &scenariosv1.RestOfYear{Amount: tyUsd(3800)}}}},
		DefaultPart: &scenariosv1.MortgageDefault{Id: "loan-d", Rest: &scenariosv1.MortgageDefault_Amortised_{Amortised: &scenariosv1.MortgageDefault_Amortised{}}}}}},
		&scenariosv1.ItemisedModel{Id: "more", Name: "More gifts", Kind: &scenariosv1.ItemisedModel_Charity{Charity: &scenariosv1.CharityModel{Parts: []*scenariosv1.CharityPart{
			{Id: "more-r", Part: &scenariosv1.CharityPart_RestOfYear{RestOfYear: &scenariosv1.RestOfYear{Amount: tyUsd(700), Date: tyDate(12, 20)}}}}}}})
	m.Accounts = append(m.Accounts, &scenariosv1.AccountModel{Id: "fund", Name: "Fund", LastYearQualifiedShare: &commonv1.Quantity{Units: 75},
		Parts: []*scenariosv1.AccountPart{{Id: "fund-p", Part: &scenariosv1.AccountPart_FromCredits_{FromCredits: &scenariosv1.AccountPart_FromCredits{Credits: []*scenariosv1.AccountPart_FromCredits_Credit{
			{Id: "fund-1", Date: tyDate(6, 30), Amount: tyUsd(400), Kind: scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND}}}}}},
		DefaultPart: &scenariosv1.AccountDefault{Id: "fund-d", Rest: &scenariosv1.AccountDefault_PaceSoFar_{PaceSoFar: &scenariosv1.AccountDefault_PaceSoFar{}}}})
	entries := m.GetBusinesses()[0].GetParts()[0].GetFromLedger()
	entries.Entries = append(entries.Entries, &scenariosv1.BusinessPart_FromLedger_Entry{Id: "biz-e3", Date: tyDate(5, 10), Amount: tyUsd(5000),
		Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE, Line: scenariosv1.ScheduleCExpense_SCHEDULE_C_EXPENSE_DEPRECIATION, Category: "Equipment"})
	m.GetBusinesses()[0].Parts = m.GetBusinesses()[0].GetParts()[:1]
	m.OtherIncome = append(m.OtherIncome, &scenariosv1.OtherIncomeModel{Id: "flat", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RENTAL, Property: "Flat",
		Parts: []*scenariosv1.OtherIncomePart{{Id: "flat-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{
			Gross: tyUsd(12000), RentalExpenses: &scenariosv1.RentalExpenses{Repairs: tyUsd(1000), Depreciation: tyUsd(3000)}}}}}})
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.Scenario("plan")
	if p := plan.Proto().GetProblems(); len(p) > 0 {
		t.Fatalf("problems: %v", p)
	}
	for _, c := range []struct {
		path string
		want int64
	}{
		// The trade's own basis and wash sale: $4,500 less $4,000 plus $100,
		// long-term by its box E.
		{"projection.brokers[schwab].items[brokers/schwab/2026-07-01/sale_lot/schwab-t1].sale.gain", 60_000},
		// June's statement, then July and August paid from the bank at its
		// interest, then RestOfYear's $3,800.
		{"projection.itemised[loan].year.boxes.mortgage_interest", 300_000 + 380_000},
		{"projection.itemised[more].year.boxes.gifts_by_cash", 70_000},
		// 75% of ordinary dividends are qualified, by last year's share.
		{"projection.accounts[fund].year.boxes.qualified_dividends", 30_000},
		// The $5,000 of equipment counts once and isn't paced: the $800 of
		// other expenses over the three months with entries, Mar to May,
		// is $266.66 a month for September to December, $1,067 in whole
		// dollars.
		{"projection.businesses[biz].year.boxes.total_expenses", 580_000 + 106_700},
		{"projection.other_income[flat].year.items.expenses", 400_000},
	} {
		if got := tyFigure(t, r, "plan", c.path); got != c.want {
			t.Errorf("%s = %d, want %d", c.path, got, c.want)
		}
	}
	if box := plan.Proto().GetProjection().GetBrokers()[0].GetItems()[0].GetSale().GetBox(); box != scenariosv1.Form8949Box_FORM_8949_BOX_E {
		t.Errorf("box %v, want the trade's E", box)
	}
}

// An override's was is the figure the person saw; when today's computed
// figure differs, its working says the sources changed.
func fixtureOverrideWas(t *testing.T) {
	for _, c := range []struct {
		was     float64
		changed bool
	}{{0, false}, {9999, true}} {
		f := tyReadExample(t)
		f.GetShared().Overrides = []*scenariosv1.Override{{Path: "projection.employers[acme].so_far.items.gross", Amount: tyUsd(12000), Was: tyUsd(11666.66)}}
		if c.was != 0 {
			f.GetShared().GetOverrides()[0].Was = tyUsd(c.was)
		}
		r, err := recordCompute(t, f)
		if err != nil {
			t.Fatal(err)
		}
		p, _ := taxyear.ParsePath("projection.employers[acme].so_far.items.gross")
		plan, _ := r.Scenario("plan")
		fig, ok := plan.Figure(p)
		if !ok || fig.Working() == nil {
			t.Fatalf("no working for the override")
		}
		w := fig.Working()
		if w.GetSourcesChanged() != c.changed || w.GetAutomatic().GetMinor() != 1_166_700 {
			t.Errorf("was %v: working %v, want automatic $11,667, sources changed %v", c.was, w, c.changed)
		}
	}
}

// Last year's return's carries_forward stands in for the facts'
// carryovers, and a state refund's own refund_year_itemised decides whether
// it is income, whatever last year's return did.
func fixtureCarriesForwardAndRefundYear(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		itemised *bool
		want     int64
	}{{&yes, 50_000}, {&no, 0}} {
		f := tyOthers()
		facts := f.GetShared().GetFacts()
		facts.PriorYear = []*factsv1.PriorYearReturn{{Id: "py", Jurisdiction: "US", Status: factsv1.PriorYearReturn_STATUS_FILED,
			TotalTax: tyUsd(1000), Itemised: &no,
			CarriesForward: []*factsv1.CarriedForward{{Kind: factsv1.CarryoverKind_CARRYOVER_KIND_LONG_TERM_CAPITAL_LOSS, Amount: tyUsd(1200)}}}}
		f.GetShared().GetModel().OtherIncome = append(f.GetShared().GetModel().OtherIncome, &scenariosv1.OtherIncomeModel{
			Id: "ftb", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND, State: "CA", RefundYearItemised: c.itemised,
			Parts: []*scenariosv1.OtherIncomePart{{Id: "ftb-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{Gross: tyUsd(500)}}}}})
		r, err := recordCompute(t, f)
		if err != nil {
			t.Fatal(err)
		}
		if got := tyFigure(t, r, "plan", "federal.schedule_d.long_term_capital_loss_carryover"); got != 120_000 {
			t.Errorf("long-term carryover %d, want last year's $1,200 carried forward", got)
		}
		if got := tyFigure(t, r, "plan", "federal.schedule_1.taxable_state_local_refunds"); got != c.want {
			t.Errorf("refund year itemised %v: taxable refund %d, want %d", *c.itemised, got, c.want)
		}
	}
}

// A 1099-NEC states its payer's receipts for the whole year. One that
// names its payer and no client is reconciled to the ledger's receipts
// from that payer, not added to them.
func fixtureFormReconcilesToItsPayersReceipts(t *testing.T) {
	f := tyOthers()
	entry := func(id string, mo int32) *scenariosv1.BusinessPart_FromLedger_Entry {
		return &scenariosv1.BusinessPart_FromLedger_Entry{Id: id, Date: tyDate(mo, 10), Amount: tyUsd(3000), Client: "Northwind",
			Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT}
	}
	nec := &form1099nec.Form1099NEC{TaxYear: 2026, Payer: &commonv1.Party{Name: "Northwind"}, NonemployeeCompensation: tyUsd(7000)}
	f.GetShared().GetModel().Businesses = []*scenariosv1.BusinessModel{{Id: "biz", Name: "Biz", Person: "you",
		Parts: []*scenariosv1.BusinessPart{
			{Id: "biz-p1", Part: &scenariosv1.BusinessPart_FromLedger_{FromLedger: &scenariosv1.BusinessPart_FromLedger{Entries: []*scenariosv1.BusinessPart_FromLedger_Entry{entry("biz-1", 1), entry("biz-2", 4)}}}},
			{Id: "biz-p2", Part: &scenariosv1.BusinessPart_FromForms_{FromForms: &scenariosv1.BusinessPart_FromForms{Forms: []*scenariosv1.BusinessPart_FromForms_Form{
				{Id: "biz-nec", Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099Nec{Form_1099Nec: nec}}}}}}},
		DefaultPart: &scenariosv1.BusinessDefault{Id: "biz-d", Rest: &scenariosv1.BusinessDefault_PaceSoFar_{PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{}}}}}
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	if got := tyFigure(t, r, "plan", "projection.businesses[biz].year.items.receipts"); got != 700_000 {
		t.Errorf("the year's receipts are %d, want the form's 700000", got)
	}
}

// A state's tax for a finished year is its filed return's total tax; with
// the year in progress, or without the return, it isn't worked out.
func fixtureStateTaxFromFiledReturn(t *testing.T) {
	ca := func(f *scenariosv1.TaxYearScenarios) *commonv1.Money {
		r, err := recordCompute(t, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range r.Proto().GetScenarios()[0].GetEstimated() {
			if e.GetJurisdiction() == "CA" {
				return e.GetTaxForYear()
			}
		}
		t.Fatal("no estimated[CA]")
		return nil
	}
	f := tyOthers()
	f.GetShared().GetFacts().Unassigned = []*factsv1.Document{{Id: "return.CA.2026",
		Form:   &factsv1.Document_CaliforniaReturn{CaliforniaReturn: &factsv1.CaliforniaReturn{Form_540: &form540.Form540{TaxYear: 2026, TotalTax: tyUsd(2114)}}},
		Source: &factsv1.DocumentRef{Name: "2026 California return"}}}
	if got := ca(f); got != nil {
		t.Errorf("in progress, CA's tax for the year is %v, want unset", got)
	}
	f.AsOf = &commonv1.Date{Year: 2027, Month: 3, Day: 1}
	if got := ca(f).GetMinor(); got != 211_400 {
		t.Errorf("finished, CA's tax for the year is %d, want the return's 211400", got)
	}
	f.GetShared().GetFacts().Unassigned = nil
	if got := ca(f); got != nil {
		t.Errorf("finished without the return, CA's tax for the year is %v, want unset", got)
	}
}

// A 1099-B's printed Form 8949 checkbox decides the box, though the
// transaction doesn't say its basis went to the IRS.
func fixtureBrokerTakesThePrintedBox(t *testing.T) {
	f := tyOthers()
	tx := &form1099b.Form1099B{TaxYear: 2026, Form_8949Checkbox: "D", DateAcquired: &commonv1.Date{Year: 2020, Month: 1, Day: 2},
		DateSoldOrDisposed: tyDate(3, 4), Proceeds: tyUsd(1000), CostOrOtherBasis: tyUsd(400),
		HoldingPeriod: form1099b.Form1099B_HOLDING_PERIOD_LONG_TERM}
	f.GetShared().GetModel().Brokers = []*scenariosv1.BrokerModel{{Id: "b", Name: "B", Parts: []*scenariosv1.BrokerPart{
		{Id: "b-f", Part: &scenariosv1.BrokerPart_FromForms_{FromForms: &scenariosv1.BrokerPart_FromForms{Statements: []*scenariosv1.BrokerPart_FromForms_Statement{
			{Id: "b-s", Statement: &factsv1.Form1099BStatement{Transactions: []*form1099b.Form1099B{tx}}}}}}}}}}
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	boxes := r.Proto().GetScenarios()[0].GetProjection().GetBrokers()[0].GetYear().GetBoxes()
	if len(boxes) != 1 || boxes[0].GetBox() != scenariosv1.Form8949Box_FORM_8949_BOX_D {
		t.Errorf("boxes %v, want the printed D", boxes)
	}
}

// Foreign tax is what the account shows taken so far; it isn't paced.
func fixtureForeignTaxIsNotPaced(t *testing.T) {
	f := tyOthers()
	a := f.GetShared().GetModel().GetAccounts()[0]
	credits := a.GetParts()[0].GetFromCredits()
	credits.Credits = append(credits.Credits, &scenariosv1.AccountPart_FromCredits_Credit{Id: "ally-ft", Date: tyDate(3, 28), Amount: tyUsd(9.40),
		Kind: scenariosv1.CreditKind_CREDIT_KIND_FOREIGN_TAX})
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	if got := tyFigure(t, r, "plan", "projection.accounts[ally].year.items.foreign_tax_paid"); got != 900 {
		t.Errorf("the year's foreign tax is %d, want the $9 taken so far", got)
	}
}

// A due date still to come with a payment planned on it that doesn't meet
// it reads as planned but short, not as having nothing planned.
func fixturePlannedPaymentThatFallsShort(t *testing.T) {
	f := tyOthers()
	us := f.GetShared().GetModel().GetTaxPaid()[0]
	us.Parts = append(us.Parts, &scenariosv1.TaxPaidPart{Id: "us-sep", Part: &scenariosv1.TaxPaidPart_PlannedPayment_{
		PlannedPayment: &scenariosv1.TaxPaidPart_PlannedPayment{Due: tyDate(9, 15), Amount: tyUsd(500)}}})
	f.GetShared().GetModel().GetBusinesses()[0].Parts[1].GetMonthly().Receipts = tyUsd(9000)
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.Scenario("plan")
	for _, in := range plan.Proto().GetEstimated()[0].GetRules().GetInstallments() {
		if in.GetDue().GetMonth() != 9 {
			continue
		}
		t.Logf("Sep 15: needed %d, credited %d, status %v", in.GetNeededByCents(), in.GetCreditedByCents(), in.GetStatus())
		if in.GetStatus() != usv1.InstallmentStatus_INSTALLMENT_STATUS_PLANNED_SHORT {
			t.Errorf("Sep 15 is %v, want planned but short", in.GetStatus())
		}
		return
	}
	t.Fatal("no Sep 15 installment")
}

// An account closed on Sep 1 pays nothing after it: its quarterly
// dividends so far don't recur on Sep 30 or Dec 31.
func fixtureClosedAccountStopsPaying(t *testing.T) {
	f := tyOthers()
	credit := func(id string, mo, d int32, v float64) *scenariosv1.AccountPart_FromCredits_Credit {
		return &scenariosv1.AccountPart_FromCredits_Credit{Id: id, Date: tyDate(mo, d), Amount: tyUsd(v), Kind: scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND}
	}
	m := f.GetShared().GetModel()
	m.Accounts = append(m.Accounts, &scenariosv1.AccountModel{Id: "fund", Name: "Fund",
		Parts: []*scenariosv1.AccountPart{
			{Id: "fund-p", Part: &scenariosv1.AccountPart_FromCredits_{FromCredits: &scenariosv1.AccountPart_FromCredits{Credits: []*scenariosv1.AccountPart_FromCredits_Credit{
				credit("fund-1", 3, 31, 40), credit("fund-2", 6, 30, 60)}}}},
			{Id: "fund-c", Part: &scenariosv1.AccountPart_Closed_{Closed: &scenariosv1.AccountPart_Closed{On: tyDate(9, 1)}}}},
		DefaultPart: &scenariosv1.AccountDefault{Id: "fund-d", Rest: &scenariosv1.AccountDefault_PaceSoFar_{PaceSoFar: &scenariosv1.AccountDefault_PaceSoFar{}}}})
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	plan, _ := r.Scenario("plan")
	for _, a := range plan.Proto().GetProjection().GetAccounts() {
		if a.GetId() != "fund" {
			continue
		}
		for _, it := range a.GetItems() {
			if d := it.GetDate(); d.GetMonth() > 9 || d.GetMonth() == 9 && d.GetDay() > 1 {
				t.Errorf("item %s after the account closed on Sep 1", it.GetId())
			}
		}
	}
	if got := tyFigure(t, r, "plan", "projection.accounts[fund].year.items.ordinary_dividends"); got != 10_000 {
		t.Errorf("dividends for the year %d, want the $100 paid before it closed", got)
	}
}

// A state refund is taxable only as far as deducting the tax lowered last
// year's tax: the Form 1040 instructions' State and Local Income Tax
// Refund Worksheet. With last year's return here, $16,000 itemised against
// a $15,750 standard deduction makes $250 of a $500 refund taxable. With
// only "itemised" typed, the figures aren't known: all of it counts, and
// the result asks for last year's Schedule A.
func fixtureStateRefundTaxBenefit(t *testing.T) {
	yes := true
	refund := &scenariosv1.OtherIncomeModel{Id: "ftb", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND, State: "CA",
		Parts: []*scenariosv1.OtherIncomePart{{Id: "ftb-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{Gross: tyUsd(500)}}}}}
	filed := &factsv1.PriorYearReturn{Id: "py", Jurisdiction: "US", Status: factsv1.PriorYearReturn_STATUS_FILED,
		Filed: &factsv1.PriorYearReturn_Federal{Federal: &factsv1.FederalReturn{
			Form_1040: &form1040.Form1040{TaxYear: 2025, FilingStatus: form1040.FilingStatus_FILING_STATUS_SINGLE, Itemized: true},
			ScheduleA: &form1040.ScheduleA{TaxYear: 2025, StateLocalIncomeOrSalesTaxes: tyUsd(6000), TotalStateAndLocalTaxes: tyUsd(9000),
				StateAndLocalTaxesAfterCap: tyUsd(9000), TotalItemizedDeductions: tyUsd(16_000)}}}}
	typed := &factsv1.PriorYearReturn{Id: "py", Jurisdiction: "US", Status: factsv1.PriorYearReturn_STATUS_FILED, TotalTax: tyUsd(1000), Itemised: &yes}
	for _, c := range []struct {
		name  string
		prior *factsv1.PriorYearReturn
		want  int64
		asks  bool
	}{{"return here", filed, 25_000, false}, {"itemised typed", typed, 50_000, true}} {
		f := tyOthers()
		f.GetShared().GetFacts().PriorYear = []*factsv1.PriorYearReturn{c.prior}
		f.GetShared().GetModel().OtherIncome = append(f.GetShared().GetModel().OtherIncome, refund)
		r, err := recordCompute(t, f)
		if err != nil {
			t.Fatal(err)
		}
		plan, _ := r.Scenario("plan")
		if p := plan.Proto().GetProblems(); len(p) > 0 {
			t.Fatalf("%s: problems %v", c.name, p)
		}
		if got := tyFigure(t, r, "plan", "federal.schedule_1.taxable_state_local_refunds"); got != c.want {
			t.Errorf("%s: taxable refund %d, want %d", c.name, got, c.want)
		}
		var asked bool
		for _, q := range plan.Proto().GetQuestions() {
			asked = asked || q.GetId() == "federal/state-refund-schedule-a"
		}
		if asked != c.asks {
			t.Errorf("%s: asked for last year's Schedule A %v, want %v", c.name, asked, c.asks)
		}
		for _, s := range plan.Proto().GetSummary().GetOtherIncome() {
			if s.GetId() == "ftb" && s.GetTaxable().GetMinor() != c.want {
				t.Errorf("%s: summary taxable %v, want %d", c.name, s.GetTaxable(), c.want)
			}
		}
	}
}

// A state refund with no prior-year return in the facts counts in full,
// and the result asks whether last year itemised.
func fixtureStateRefundWithNoPriorYear(t *testing.T) {
	f := tyOthers()
	f.GetShared().GetModel().OtherIncome = append(f.GetShared().GetModel().OtherIncome, &scenariosv1.OtherIncomeModel{
		Id: "ftb", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND, State: "CA",
		Parts: []*scenariosv1.OtherIncomePart{{Id: "ftb-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{Gross: tyUsd(500)}}}}})
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	if got := tyFigure(t, r, "plan", "federal.schedule_1.taxable_state_local_refunds"); got != 50_000 {
		t.Errorf("taxable refund %d, want all $500", got)
	}
	plan, _ := r.Scenario("plan")
	var asked bool
	for _, q := range plan.Proto().GetQuestions() {
		asked = asked || q.GetId() == "federal/state-refund"
	}
	if !asked {
		t.Error("the result doesn't ask whether last year itemised")
	}
}

// Last year's Schedule A without its line 17: the itemised total is Form
// 1040 line 12, the deduction taken. $29,113 itemised against the $15,750
// standard deduction, with $6,513 of state income tax deducted and nothing
// held back by the cap, makes $6,513 of a $10,000 refund taxable. With no
// return for last year, all of it counts, and the result says so.
func fixtureStateRefundFromLine12(t *testing.T) {
	refund := &scenariosv1.OtherIncomeModel{Id: "ftb", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND, State: "CA",
		RefundYearItemised: proto.Bool(true),
		Parts:              []*scenariosv1.OtherIncomePart{{Id: "ftb-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{Gross: tyUsd(10_000)}}}}}
	filed := &factsv1.PriorYearReturn{Id: "py", Jurisdiction: "US", Status: factsv1.PriorYearReturn_STATUS_FILED,
		Filed: &factsv1.PriorYearReturn_Federal{Federal: &factsv1.FederalReturn{
			Form_1040: &form1040.Form1040{TaxYear: 2025, FilingStatus: form1040.FilingStatus_FILING_STATUS_SINGLE, Itemized: true, Deduction: tyUsd(29_113)},
			ScheduleA: &form1040.ScheduleA{TaxYear: 2025, StateLocalIncomeOrSalesTaxes: tyUsd(6_513), TotalStateAndLocalTaxes: tyUsd(12_844),
				StateAndLocalTaxesAfterCap: tyUsd(12_844)}}}}
	for _, c := range []struct {
		prior []*factsv1.PriorYearReturn
		want  int64
		left  bool
	}{{[]*factsv1.PriorYearReturn{filed}, 651_300, false}, {nil, 1_000_000, true}} {
		f := tyOthers()
		f.GetShared().GetFacts().PriorYear = c.prior
		f.GetShared().GetModel().OtherIncome = append(f.GetShared().GetModel().OtherIncome, refund)
		r, err := recordCompute(t, f)
		if err != nil {
			t.Fatal(err)
		}
		if got := tyFigure(t, r, "plan", "federal.schedule_1.taxable_state_local_refunds"); got != c.want {
			t.Errorf("prior %d: taxable refund %d, want %d", len(c.prior), got, c.want)
		}
		plan, _ := r.Scenario("plan")
		var left bool
		for _, n := range plan.Proto().GetNotIncluded() {
			left = left || n.GetText().GetText() == "How much of a state refund is taxable: without your 2025 return, all of it is counted"
		}
		if left != c.left {
			t.Errorf("prior %d: left out %v, want %v", len(c.prior), left, c.left)
		}
	}
}

// From taxyear/new_forms_test.go.

func tyDocRef(id string) *factsv1.DocumentRef {
	return &factsv1.DocumentRef{Name: id}
}

// TestForm1099OIDInAccountModel verifies that Form 1099-OID attached to an
// AccountModel expands interest, treasury interest, and tax-exempt interest.
func fixtureForm1099OIDInAccountModel(t *testing.T) {
	f := tyOthers()
	oid := &form1099oid.Form1099OID{
		TaxYear:               2026,
		OriginalIssueDiscount: tyUsd(1000),
		OtherPeriodicInterest: tyUsd(200),
		MarketDiscount:        tyUsd(50),
		AcquisitionPremium:    tyUsd(30),
		OriginalIssueDiscountOnTreasuryObligations: tyUsd(300),
		TaxExemptOid: tyUsd(150),
	}
	f.GetShared().GetModel().Accounts = []*scenariosv1.AccountModel{{
		Id:   "bank-oid",
		Name: "Treasury Bank",
		Parts: []*scenariosv1.AccountPart{{
			Id: "p1",
			Part: &scenariosv1.AccountPart_FromForms_{
				FromForms: &scenariosv1.AccountPart_FromForms{
					Forms: []*scenariosv1.AccountPart_FromForms_Form{{
						Id:     "oid-1",
						Source: tyDocRef("doc-oid-1"),
						Form:   &scenariosv1.AccountPart_FromForms_Form_Form_1099Oid{Form_1099Oid: oid},
					}},
				},
			},
		}},
		DefaultPart: &scenariosv1.AccountDefault{
			Id:   "bank-d",
			Rest: &scenariosv1.AccountDefault_PaceSoFar_{PaceSoFar: &scenariosv1.AccountDefault_PaceSoFar{}},
		},
	}}
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]
	if p := res.GetProblems(); len(p) > 0 {
		t.Fatalf("unexpected problems: %v", p)
	}
	// Taxable interest = (1000 + 200 + 50 - 30) + 300 = 1520
	taxableInterest := res.GetFederal().GetForm_1040().GetTaxableInterest().GetMinor()
	if taxableInterest != 152000 {
		t.Errorf("TaxableInterest = %d, want 152000 ($1520.00)", taxableInterest)
	}
	taxExempt := res.GetFederal().GetForm_1040().GetTaxExemptInterest().GetMinor()
	if taxExempt != 15000 {
		t.Errorf("TaxExemptInterest = %d, want 15000 ($150.00)", taxExempt)
	}
}

// TestForm1099MISCInBusinessModel verifies that Form 1099-MISC attached to a
// BusinessModel reconciles receipts properly.
func fixtureForm1099MISCInBusinessModel(t *testing.T) {
	f := tyOthers()
	misc := &form1099misc.Form1099MISC{
		TaxYear:     2026,
		Payer:       &commonv1.Party{Name: "ClientCorp"},
		OtherIncome: tyUsd(6000),
		Rents:       tyUsd(2000),
	}
	f.GetShared().GetModel().Businesses = []*scenariosv1.BusinessModel{{
		Id:     "biz-misc",
		Name:   "Consulting",
		Person: "you",
		Parts: []*scenariosv1.BusinessPart{{
			Id: "p1",
			Part: &scenariosv1.BusinessPart_FromForms_{
				FromForms: &scenariosv1.BusinessPart_FromForms{
					Forms: []*scenariosv1.BusinessPart_FromForms_Form{{
						Id:     "misc-1",
						Source: tyDocRef("doc-misc-1"),
						Form:   &scenariosv1.BusinessPart_FromForms_Form_Form_1099Misc{Form_1099Misc: misc},
					}},
				},
			},
		}},
		DefaultPart: &scenariosv1.BusinessDefault{
			Id:   "biz-d",
			Rest: &scenariosv1.BusinessDefault_PaceSoFar_{PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{}},
		},
	}}
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]
	if p := res.GetProblems(); len(p) > 0 {
		t.Fatalf("unexpected problems: %v", p)
	}
	bizIncome := res.GetFederal().GetSchedule_1().GetBusinessIncomeOrLoss().GetMinor()
	if bizIncome != 800000 {
		t.Errorf("BusinessIncome = %d, want 800000 ($8000.00)", bizIncome)
	}
}

// TestNewFormsInOtherIncomeModel verifies deterministic calculation of Form
// 1099-MISC, 1099-SA, 1099-S, 1099-C, 1099-Q, and Schedule K-1 forms in
// OtherIncomeModel without raising ambiguous questions.
func fixtureNewFormsInOtherIncomeModel(t *testing.T) {
	for _, tc := range []struct {
		name               string
		form               *scenariosv1.OtherIncomePart_FromForms_Form
		wantOtherIncome    int64
		wantWithheld1099   int64
		wantNotIncludedSub string
	}{
		{
			name: "Form 1099-MISC with federal withholding",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-misc",
				Source: tyDocRef("ref-misc"),
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Misc{
					Form_1099Misc: &form1099misc.Form1099MISC{
						TaxYear:            2026,
						OtherIncome:        tyUsd(1500),
						FederalTaxWithheld: tyUsd(150),
					},
				},
			},
			wantOtherIncome:  1500_00,
			wantWithheld1099: 150_00,
		},
		{
			name: "Form 1099-SA with partial qualified medical expenses",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-sa",
				Source: tyDocRef("ref-sa"),
				Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099SaSupplement{
					Form_1099SaSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099SASupplement{
						QualifiedExpenses: tyUsd(500),
					},
				},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Sa{
					Form_1099Sa: &form1099sa.Form1099SA{
						TaxYear:           2026,
						GrossDistribution: tyUsd(800),
					},
				},
			},
			wantOtherIncome:    300_00,
			wantNotIncludedSub: "20% additional tax",
		},
		{
			name: "Form 1099-SA fully covered by qualified medical expenses",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-sa-full",
				Source: tyDocRef("ref-sa-full"),
				Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099SaSupplement{
					Form_1099SaSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099SASupplement{
						QualifiedExpenses: tyUsd(800),
					},
				},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Sa{
					Form_1099Sa: &form1099sa.Form1099SA{
						TaxYear:           2026,
						GrossDistribution: tyUsd(800),
					},
				},
			},
			wantOtherIncome: 0,
		},
		{
			name: "Form 1099-C with partial Form 982 insolvency exclusion",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-c",
				Source: tyDocRef("ref-c"),
				Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099CSupplement{
					Form_1099CSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099CSupplement{
						ExcludedAmount: tyUsd(1200),
					},
				},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099C{
					Form_1099C: &form1099c.Form1099C{
						TaxYear:                2026,
						AmountOfDebtDischarged: tyUsd(2000),
					},
				},
			},
			wantOtherIncome: 800_00,
		},
		{
			name: "Form 1099-S with cost basis, selling expenses, and section 121 exclusion",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-s",
				Source: tyDocRef("ref-s"),
				Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099SSupplement{
					Form_1099SSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099SSupplement{
						Basis:           tyUsd(200000),
						SellingExpenses: tyUsd(20000),
						ExcludedAmount:  tyUsd(250000),
					},
				},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099S{
					Form_1099S: &form1099s.Form1099S{
						TaxYear:       2026,
						GrossProceeds: tyUsd(500000),
					},
				},
			},
			wantOtherIncome: 30000_00, // 500k - 200k - 20k - 250k = 30k
		},
		{
			name: "Form 1099-Q with partial qualified education expenses",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-q",
				Source: tyDocRef("ref-q"),
				Supplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099QSupplement{
					Form_1099QSupplement: &scenariosv1.OtherIncomePart_FromForms_Form_Form1099QSupplement{
						QualifiedExpenses: tyUsd(3000), // 60% covered -> 40% of $1,000 earnings = $400 taxable
					},
				},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Q{
					Form_1099Q: &form1099q.Form1099Q{
						TaxYear:           2026,
						GrossDistribution: tyUsd(5000),
						Earnings:          tyUsd(1000),
					},
				},
			},
			wantOtherIncome:    400_00,
			wantNotIncludedSub: "10% additional tax",
		},
		{
			name: "Schedule K-1 (Form 1065) ordinary business income",
			form: &scenariosv1.OtherIncomePart_FromForms_Form{
				Id:     "f-k1",
				Source: tyDocRef("ref-k1"),
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_ScheduleK1_1065{
					ScheduleK1_1065: &schedulek1.ScheduleK1Form1065{
						TaxYear:                2026,
						OrdinaryBusinessIncome: tyUsd(3500),
					},
				},
			},
			wantOtherIncome:    3500_00,
			wantNotIncludedSub: "Partner basis limits",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := tyOthers()
			f.GetShared().GetModel().OtherIncome = []*scenariosv1.OtherIncomeModel{{
				Id:    "other-src",
				Payer: "Payer",
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p-forms",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{tc.form},
						},
					},
				}},
			}}
			r, err := recordCompute(t, f)
			if err != nil {
				t.Fatalf("Compute failed: %v", err)
			}
			res := r.Proto().GetScenarios()[0]
			if p := res.GetProblems(); len(p) > 0 {
				t.Fatalf("unexpected problems: %v", p)
			}
			if got := res.GetFederal().GetSchedule_1().GetTotalOtherIncome().GetMinor(); got != tc.wantOtherIncome {
				t.Errorf("TotalOtherIncome = %d, want %d", got, tc.wantOtherIncome)
			}
			if got := res.GetFederal().GetForm_1040().GetWithheldFrom_1099().GetMinor(); got != tc.wantWithheld1099 {
				t.Errorf("WithheldFrom_1099 = %d, want %d", got, tc.wantWithheld1099)
			}
			for _, q := range res.GetQuestions() {
				if strings.HasPrefix(q.GetId(), "other_income/") {
					t.Errorf("unexpected other_income question: %v", q)
				}
			}
			if tc.wantNotIncludedSub != "" {
				var found bool
				for _, ni := range res.GetNotIncluded() {
					if strings.Contains(ni.GetText().GetText(), tc.wantNotIncludedSub) {
						found = true
					}
				}
				if !found {
					t.Errorf("NotIncluded = %v, want substring %q", res.GetNotIncluded(), tc.wantNotIncludedSub)
				}
			}
		})
	}
}

// TestNewFormsInRetirementModel verifies that Form 5498 and Form 5498-SA
// contributions are tracked in RetirementProjection.
func fixtureNewFormsInRetirementModel(t *testing.T) {
	f := tyOthers()
	f.GetShared().GetModel().Retirement = []*scenariosv1.RetirementModel{{
		Id:     "ret-1",
		Person: "you",
		Parts: []*scenariosv1.RetirementPart{{
			Id: "p-forms",
			Part: &scenariosv1.RetirementPart_FromForms_{
				FromForms: &scenariosv1.RetirementPart_FromForms{
					Forms: []*scenariosv1.RetirementPart_FromForms_Form{
						{
							Id:     "f-5498",
							Source: tyDocRef("ref-5498"),
							Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498{
								Form_5498: &form5498.Form5498{
									TaxYear:              2026,
									IraContributions:     tyUsd(3000),
									RothIraContributions: tyUsd(2000),
								},
							},
						},
						{
							Id:     "f-5498sa",
							Source: tyDocRef("ref-5498sa"),
							Form: &scenariosv1.RetirementPart_FromForms_Form_Form_5498Sa{
								Form_5498Sa: &form5498sa.Form5498SA{
									TaxYear:                      2026,
									TotalContributionsMadeInYear: tyUsd(1500),
								},
							},
						},
					},
				},
			},
		}},
	}}
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	res := r.Proto().GetScenarios()[0]
	retProj := res.GetProjection().GetRetirement()[0]
	if got := retProj.GetYear().GetItems().GetTraditionalIra().GetMinor(); got != 300000 {
		t.Errorf("TraditionalIra = %d, want 300000", got)
	}
	if got := retProj.GetYear().GetItems().GetRothIra().GetMinor(); got != 200000 {
		t.Errorf("RothIra = %d, want 200000", got)
	}
	if got := retProj.GetYear().GetItems().GetHsa().GetMinor(); got != 150000 {
		t.Errorf("Hsa = %d, want 150000", got)
	}
}

// From taxyear/property_test.go.

// tySlots are Acme's twice-monthly pay dates before the example's as_of,
// Sep 8, 2026: the 15th and the last day, a weekend moved to the Friday.
var tySlots = []*commonv1.Date{
	tyDate(1, 15), tyDate(1, 30), tyDate(2, 13), tyDate(2, 27), tyDate(3, 13), tyDate(3, 31), tyDate(4, 15), tyDate(4, 30),
	tyDate(5, 15), tyDate(5, 29), tyDate(6, 15), tyDate(6, 30), tyDate(7, 15), tyDate(7, 31), tyDate(8, 14), tyDate(8, 31),
}

func tyCents(v float64) *commonv1.Money { return tyUsd(v) }

// household makes a random household from the example: some of Acme's pay
// dates with a paystub, some with deposits, split or not, some extra
// deposits and a bonus; maybe a second job starting later; and in Raise in
// Oct a random raise, bonus and end.
func tyHousehold(r *rand.Rand) *scenariosv1.TaxYearScenarios {
	f, err := taxyear.Parse(tyExample())
	if err != nil {
		panic(err)
	}
	a := f.GetShared().GetModel().GetEmployers()[0]
	var stubs []*scenariosv1.EmployerPart_FromPaystubs_Stub
	var deps []*scenariosv1.EmployerPart_FromDeposits_Deposit
	for i, d := range tySlots {
		gross := 5833.33 + float64(r.Intn(3))*100
		fig := &factsv1.PayFigures{Gross: tyCents(gross), FederalWithheld: tyCents(700), Net: tyCents(gross - 1700),
			Pretax: []*factsv1.PayFigures_Pretax{{Kind: factsv1.PretaxKind_PRETAX_KIND_401K, Amount: tyCents(float64(r.Intn(5)) * 100)}}}
		if r.Intn(3) == 0 {
			fig.Net = nil
		}
		switch r.Intn(4) {
		case 0:
			stubs = append(stubs, &scenariosv1.EmployerPart_FromPaystubs_Stub{Id: fmt.Sprintf("acme-s%02d", i), Source: &factsv1.DocumentRef{Name: "stub"},
				Paystub: &factsv1.Paystub{PayDate: d, Kind: factsv1.PaycheckKind_PAYCHECK_KIND_REGULAR, ThisPaycheck: fig}})
			if fig.Net != nil && r.Intn(2) == 0 {
				deps = append(deps, &scenariosv1.EmployerPart_FromDeposits_Deposit{Id: fmt.Sprintf("acme-d%02da", i), Date: d, Amount: tyCents(gross - 1700)})
			}
		case 1:
			deps = append(deps, &scenariosv1.EmployerPart_FromDeposits_Deposit{Id: fmt.Sprintf("acme-d%02da", i), Date: d, Amount: tyCents(3000)},
				&scenariosv1.EmployerPart_FromDeposits_Deposit{Id: fmt.Sprintf("acme-d%02db", i), Date: d, Amount: tyCents(1102.18)})
		}
	}
	if r.Intn(2) == 0 {
		deps = append(deps, &scenariosv1.EmployerPart_FromDeposits_Deposit{Id: "acme-dx", Date: tyDate(6, 22), Amount: tyCents(250)})
	}
	if r.Intn(2) == 0 {
		stubs = append(stubs, &scenariosv1.EmployerPart_FromPaystubs_Stub{Id: "acme-bonus", Source: &factsv1.DocumentRef{Name: "bonus"},
			Paystub: &factsv1.Paystub{PayDate: tyDate(3, 20), Kind: factsv1.PaycheckKind_PAYCHECK_KIND_BONUS, ThisPaycheck: &factsv1.PayFigures{Gross: tyCents(5000), FederalWithheld: tyCents(1100)}}})
	}
	if len(stubs) > 0 && r.Intn(2) == 0 {
		last := stubs[len(stubs)-1]
		last.Paystub.YearToDate = &factsv1.PayFigures{Gross: tyCents(float64(len(tySlots)) * 5800)}
	}
	a.Parts = []*scenariosv1.EmployerPart{
		{Id: "acme-h4c8w2pq", Part: &scenariosv1.EmployerPart_FromPaystubs_{FromPaystubs: &scenariosv1.EmployerPart_FromPaystubs{Stubs: stubs}}},
		{Id: "acme-b6n3x8tf", Part: &scenariosv1.EmployerPart_FromDeposits_{FromDeposits: &scenariosv1.EmployerPart_FromDeposits{Deposits: deps}}},
	}
	if r.Intn(2) == 0 {
		f.GetShared().GetModel().Employers = append(f.GetShared().GetModel().Employers, &scenariosv1.EmployerModel{
			Id: "beta", Name: "Beta", Person: "you", PayFrequency: scenariosv1.PayFrequency_PAY_FREQUENCY_BIWEEKLY,
			FirstPayDate: tyDate(1, 9), Starts: tyDate(int32(r.Intn(10)+1), 1), Work: a.GetWork(), Supplemental: a.GetSupplemental(),
			DefaultPart: &scenariosv1.EmployerDefault{Id: "beta-default", Rest: &scenariosv1.EmployerDefault_EachPaycheck{EachPaycheck: &factsv1.PayFigures{Gross: tyCents(2000)}}},
		})
	}
	raise := f.GetScenarios()[1].GetChanges().GetModel().GetEmployers()[0].GetPatch()
	add := func(p *scenariosv1.EmployerPart) {
		raise.Parts = append(raise.Parts, &scenariosv1.EmployerModelDiff_PartsChange{Id: proto.String(p.GetId()), Op: &scenariosv1.EmployerModelDiff_PartsChange_Add{Add: p}})
	}
	raise.Parts = nil
	add(&scenariosv1.EmployerPart{Id: "acme-raise", Part: &scenariosv1.EmployerPart_PayRate_{PayRate: &scenariosv1.EmployerPart_PayRate{
		From: tyDate(int32(r.Intn(2)+9), int32(r.Intn(28)+1)), Paycheck: &factsv1.PayFigures{Gross: tyCents(6000 + float64(r.Intn(1000)))}}}})
	if r.Intn(2) == 0 {
		add(&scenariosv1.EmployerPart{Id: "acme-oneoff", Part: &scenariosv1.EmployerPart_OneOff_{OneOff: &scenariosv1.EmployerPart_OneOff{
			Date: tyDate(12, int32(r.Intn(20)+1)), Kind: factsv1.PaycheckKind_PAYCHECK_KIND_BONUS, Amount: tyCents(float64(r.Intn(20000)))}}})
	}
	if r.Intn(2) == 0 {
		add(&scenariosv1.EmployerPart{Id: "acme-ends", Part: &scenariosv1.EmployerPart_Ends_{Ends: &scenariosv1.EmployerPart_Ends{LastPaycheck: tyDate(11, int32(r.Intn(28)+1))}}})
	}
	return f
}

// eachProjection calls fn for each employer projection in every scenario.
func tyEachEmployer(res *scenariosv1.TaxYearResult, fn func(scenario string, p *scenariosv1.EmployerProjection)) {
	for _, s := range res.GetScenarios() {
		for _, p := range s.GetProjection().GetEmployers() {
			fn(s.GetScenario(), p)
		}
	}
}

func tyMustCompute(t *testing.T, seed int64, f *scenariosv1.TaxYearScenarios) *scenariosv1.TaxYearResult {
	t.Helper()
	res := tyCompute(t, f)
	var problems []string
	for _, p := range res.GetProblems() {
		problems = append(problems, p.GetPath()+": "+p.GetMessage().GetText())
	}
	for _, s := range res.GetScenarios() {
		for _, p := range s.GetProblems() {
			problems = append(problems, s.GetScenario()+" "+p.GetPath()+": "+p.GetMessage().GetText())
		}
	}
	if len(problems) > 0 {
		t.Fatalf("seed %d: problems:\n%s", seed, strings.Join(problems, "\n"))
	}
	return res
}

// TestTotalsEqualItems: every projection's item totals are the sum of its
// items, so far the made ones and the rest the expected ones.
func fixtureTotalsEqualItems(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		res := tyMustCompute(t, seed, tyHousehold(rand.New(rand.NewSource(seed))))
		tyEachEmployer(res, func(s string, p *scenariosv1.EmployerProjection) {
			var so, rest, all [5]int64
			add := func(to *[5]int64, f *factsv1.PayFigures) {
				to[0] += f.GetGross().GetMinor()
				to[1] += f.GetFederalWithheld().GetMinor()
				to[2] += f.GetSocialSecurityWithheld().GetMinor()
				to[3] += f.GetMedicareWithheld().GetMinor()
				for _, x := range f.GetPretax() {
					to[4] += x.GetAmount().GetMinor()
				}
			}
			for _, it := range p.GetItems() {
				add(&all, it.GetPaycheck())
				if it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
					add(&so, it.GetPaycheck())
				} else {
					add(&rest, it.GetPaycheck())
				}
			}
			for _, c := range []struct {
				name string
				want [5]int64
				got  *scenariosv1.PayTotals
			}{{"so_far", so, p.GetSoFar()}, {"rest", rest, p.GetRest()}, {"year", all, p.GetYear()}} {
				var got [5]int64
				add(&got, c.got.GetItems())
				if got != c.want {
					t.Errorf("seed %d %s %s %s: totals %v, items add to %v", seed, s, p.GetId(), c.name, got, c.want)
				}
			}
			box1 := tyDollars(so[0]-so[4]) + tyDollars(rest[0]-rest[4])
			if got := p.GetYear().GetBoxes().GetWages().GetMinor(); got != box1 && len(p.GetItems()) > 0 {
				t.Errorf("seed %d %s %s: box 1 %d, want so far's and the rest's gross less pre-tax in dollars, added: %d", seed, s, p.GetId(), got, box1)
			}
			if p.GetYear().GetBoxes().GetWages().GetMinor() != p.GetSoFar().GetBoxes().GetWages().GetMinor()+p.GetRest().GetBoxes().GetWages().GetMinor() {
				t.Errorf("seed %d %s %s: box 1 for the year isn't so far's plus the rest's", seed, s, p.GetId())
			}
		})
	}
}

// TestIDsAreStableAcrossScenarios: a pay date's item has the same id in
// every scenario, whichever part fills it, and every evidence item is the
// same in every scenario.
func fixtureIDsAreStableAcrossScenarios(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		res := tyMustCompute(t, seed, tyHousehold(rand.New(rand.NewSource(seed))))
		byDate := map[string]string{}
		made := map[string]map[string]bool{}
		tyEachEmployer(res, func(s string, p *scenariosv1.EmployerProjection) {
			for _, it := range p.GetItems() {
				if it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_MADE {
					if made[s] == nil {
						made[s] = map[string]bool{}
					}
					made[s][it.GetId()] = true
				}
				if it.GetKind() != scenariosv1.ItemKind_ITEM_KIND_PAYCHECK || strings.Count(it.GetId(), "/") != 3 {
					continue
				}
				d := it.GetDate()
				key := fmt.Sprintf("%s/%04d-%02d-%02d", p.GetId(), d.GetYear(), d.GetMonth(), d.GetDay())
				want := "employers/" + key + "/regular"
				if it.GetId() != want && it.GetStatus() == scenariosv1.ItemStatus_ITEM_STATUS_EXPECTED {
					t.Errorf("seed %d %s: the pay date %s's item is %s, want %s", seed, s, key, it.GetId(), want)
				}
				if prev, ok := byDate[key]; ok && prev != it.GetId() {
					t.Errorf("seed %d: pay date %s is %s in one scenario and %s in %s", seed, key, prev, it.GetId(), s)
				}
				byDate[key] = it.GetId()
			}
		})
		if !proto.Equal(tyMapMessage(made["plan"]), tyMapMessage(made["raise"])) {
			t.Errorf("seed %d: the made items differ between scenarios: %v and %v", seed, made["plan"], made["raise"])
		}
	}
}

// dollars rounds cents to whole dollars, half away from zero.
func tyDollars(c int64) int64 {
	if c < 0 {
		return -tyDollars(-c)
	}
	return (c + 50) / 100 * 100
}

func tyMapMessage(m map[string]bool) *factsv1.Person {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	tySortStrings(keys)
	return &factsv1.Person{Name: strings.Join(keys, ",")}
}

func tySortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// TestEvidenceIsNeverRefused: adding any paystub or deposit to a household
// that computes never gives it a problem, even after an assumed end, a
// planned change's start, or the paystub a total was typed against. The
// new evidence counts, and assumed parts it contradicts stand aside.
func fixtureEvidenceIsNeverRefused(t *testing.T) {
	for seed := int64(0); seed < 200; seed++ {
		r := rand.New(rand.NewSource(seed))
		f := tyHousehold(r)
		tyMustCompute(t, seed, f)
		a := f.GetShared().GetModel().GetEmployers()[0]
		for n := 0; n < 1+r.Intn(4); n++ {
			on := tyDate(int32(r.Intn(12)+1), int32(r.Intn(28)+1))
			switch r.Intn(3) {
			case 0:
				a.GetParts()[0].GetFromPaystubs().Stubs = append(a.GetParts()[0].GetFromPaystubs().Stubs, &scenariosv1.EmployerPart_FromPaystubs_Stub{
					Id: fmt.Sprintf("acme-new%d", n), Source: &factsv1.DocumentRef{Name: "new"},
					Paystub: &factsv1.Paystub{PayDate: on, Kind: factsv1.PaycheckKind(r.Intn(4) + 1), ThisPaycheck: &factsv1.PayFigures{Gross: tyCents(float64(r.Intn(9000)))}}})
			default:
				a.GetParts()[1].GetFromDeposits().Deposits = append(a.GetParts()[1].GetFromDeposits().Deposits, &scenariosv1.EmployerPart_FromDeposits_Deposit{
					Id: fmt.Sprintf("acme-newd%d", n), Date: on, Amount: tyCents(float64(r.Intn(9000)))})
			}
		}
		tyMustCompute(t, seed, f)
	}
}

// From taxyear/taxyear_test.go.

func tyReadExample(t *testing.T) *scenariosv1.TaxYearScenarios {
	t.Helper()
	f, err := taxyear.Parse(tyExample())
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// usd is dollars and cents written as a literal, rounded to the cent.
func tyUsd(d float64) *commonv1.Money {
	return &commonv1.Money{Minor: int64(math.Round(d * 100)), Currency: "USD"}
}

func tyDate(m, d int32) *commonv1.Date { return &commonv1.Date{Year: 2026, Month: m, Day: d} }

func tyCompute(t *testing.T, f *scenariosv1.TaxYearScenarios) *scenariosv1.TaxYearResult {
	t.Helper()
	r, err := recordCompute(t, f)
	if err != nil {
		t.Fatal(err)
	}
	return r.Proto()
}

func tyFigure(t *testing.T, r taxyear.Result, scenario, path string) int64 {
	t.Helper()
	s, ok := r.Scenario(scenario)
	if !ok {
		t.Fatalf("no scenario %s", scenario)
	}
	p, err := taxyear.ParsePath(path)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := s.Figure(p)
	if !ok {
		t.Fatalf("%s: no figure at %s", scenario, path)
	}
	a, ok := f.Amount()
	if !ok {
		t.Fatalf("%s: %s is not an amount", scenario, path)
	}
	return int64(a)
}
