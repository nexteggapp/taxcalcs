package taxyear_test

import (
	"strings"
	"testing"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	usv1 "github.com/nexteggapp/taxcalcs/us/v1"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	form1099b "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_b"
	form1099nec "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_nec"
	form1099r "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_r"
	form540 "github.com/nexteggapp/taxprotos/go/us/state/ca/v1/form_540"
)

// household is one person, single, in California all year, as of Sep 8,
// 2026, with one model of each kind but employers.
func others() *scenariosv1.TaxYearScenarios {
	facts := &factsv1.TaxYearFacts{TaxYear: 2026,
		People:    []*factsv1.Person{{Id: "you", DateOfBirth: &commonv1.Date{Year: 1980, Month: 1, Day: 1}, Relationship: factsv1.Relationship_RELATIONSHIP_YOU}},
		Residence: []*factsv1.ResidencePeriod{{Id: "r1", State: "CA", From: date(1, 1)}},
		Elections: []*factsv1.Election{{Id: "fs", Choice: &factsv1.Election_FilingStatus{FilingStatus: form1040.FilingStatus_FILING_STATUS_SINGLE}}},
	}
	credit := func(id string, m int32, v float64) *scenariosv1.AccountPart_FromCredits_Credit {
		return &scenariosv1.AccountPart_FromCredits_Credit{Id: id, Date: date(m, 28), Amount: usd(v), Kind: scenariosv1.CreditKind_CREDIT_KIND_INTEREST}
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
				{Id: "biz-e1", Date: date(3, 10), Amount: usd(8000), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT},
				{Id: "biz-e2", Date: date(4, 10), Amount: usd(800), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE}}}}},
				{Id: "biz-m", Part: &scenariosv1.BusinessPart_Monthly_{Monthly: &scenariosv1.BusinessPart_Monthly{Receipts: usd(1000), Expenses: usd(100)}}}},
			DefaultPart: &scenariosv1.BusinessDefault{Id: "biz-d", Rest: &scenariosv1.BusinessDefault_PaceSoFar_{PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{}}}}},
		Brokers: []*scenariosv1.BrokerModel{{Id: "schwab", Name: "Schwab",
			Holdings: []*scenariosv1.Holding{{Id: "vti", Symbol: "VTI", Price: usd(300), Lots: []*scenariosv1.Lot{
				{Id: "vti-1", Acquired: &commonv1.Date{Year: 2020, Month: 1, Day: 1}, Shares: &commonv1.Quantity{Units: 10}, Basis: usd(1000)},
				{Id: "vti-2", Acquired: date(6, 1), Shares: &commonv1.Quantity{Units: 10}, Basis: usd(2500)}}}},
			Parts: []*scenariosv1.BrokerPart{
				{Id: "schwab-t", Part: &scenariosv1.BrokerPart_FromTrades_{FromTrades: &scenariosv1.BrokerPart_FromTrades{Trades: []*scenariosv1.BrokerPart_FromTrades_Trade{
					{Id: "schwab-t1", Holding: "vti", Date: date(7, 1), Shares: &commonv1.Quantity{Units: 15}, Proceeds: usd(4500)}}}}},
				{Id: "schwab-g", Part: &scenariosv1.BrokerPart_PlannedGain_{PlannedGain: &scenariosv1.BrokerPart_PlannedGain{Date: date(11, 1), Term: scenariosv1.GainTerm_GAIN_TERM_SHORT, Amount: usd(-500)}}}}}},
		Itemised: []*scenariosv1.ItemisedModel{
			{Id: "gifts", Name: "Gifts", Kind: &scenariosv1.ItemisedModel_Charity{Charity: &scenariosv1.CharityModel{
				LastYear: []*scenariosv1.LastYearGift{{Date: &commonv1.Date{Year: 2025, Month: 3, Day: 1}, Amount: usd(50)}, {Date: &commonv1.Date{Year: 2025, Month: 12, Day: 15}, Amount: usd(1200)}},
				Parts:    []*scenariosv1.CharityPart{{Id: "gifts-l", Part: &scenariosv1.CharityPart_LikeLastYear_{LikeLastYear: &scenariosv1.CharityPart_LikeLastYear{}}}}}}},
			{Id: "home", Name: "Home", Kind: &scenariosv1.ItemisedModel_PropertyTax{PropertyTax: &scenariosv1.PropertyTaxModel{
				DueDates: []*commonv1.Date{date(4, 10), date(12, 10)},
				Parts: []*scenariosv1.PropertyTaxPart{{Id: "home-p", Part: &scenariosv1.PropertyTaxPart_PaidFromBank_{PaidFromBank: &scenariosv1.PropertyTaxPart_PaidFromBank{Payments: []*scenariosv1.PropertyTaxPart_PaidFromBank_Payment{
					{Id: "home-1", Date: date(4, 9), Amount: usd(3000)}}}}},
					{Id: "home-y", Part: &scenariosv1.PropertyTaxPart_YearTotal_{YearTotal: &scenariosv1.PropertyTaxPart_YearTotal{Total: usd(6400)}}}},
				DefaultPart: &scenariosv1.PropertyTaxDefault{Id: "home-d", Rest: &scenariosv1.PropertyTaxDefault_LikeLastYear_{LikeLastYear: &scenariosv1.PropertyTaxDefault_LikeLastYear{}}}}}},
		},
		OtherIncome: []*scenariosv1.OtherIncomeModel{{Id: "edd", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNEMPLOYMENT, Payer: "EDD",
			Parts: []*scenariosv1.OtherIncomePart{{Id: "edd-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{Gross: usd(1200), FederalWithheld: usd(120)}}}}}},
		Retirement: []*scenariosv1.RetirementModel{{Id: "you-r", Person: "you", Parts: []*scenariosv1.RetirementPart{
			{Id: "you-ira", Part: &scenariosv1.RetirementPart_IraContribution_{IraContribution: &scenariosv1.RetirementPart_IraContribution{Date: date(12, 1), Kind: scenariosv1.IraKind_IRA_KIND_TRADITIONAL, Amount: usd(1000)}}}}}},
		TaxPaid: []*scenariosv1.TaxPaidModel{{Id: "us", Jurisdiction: "US", Parts: []*scenariosv1.TaxPaidPart{
			{Id: "us-q1", Part: &scenariosv1.TaxPaidPart_PaymentMade_{PaymentMade: &scenariosv1.TaxPaidPart_PaymentMade{Date: date(4, 15), Amount: usd(1000),
				Purpose: &factsv1.PaymentPurpose{Purpose: &factsv1.PaymentPurpose_EstimatedFor{EstimatedFor: 2026}}}}}},
			DefaultPart: &scenariosv1.TaxPaidDefault{Id: "us-d", Rest: &scenariosv1.TaxPaidDefault_SpreadTheRest_{SpreadTheRest: &scenariosv1.TaxPaidDefault_SpreadTheRest{}}}}},
	}
	return &scenariosv1.TaxYearScenarios{SchemaVersion: 1, TaxYear: 2026, AsOf: date(9, 8),
		Shared:    &scenariosv1.ScenarioInputs{Facts: facts, Model: model},
		Scenarios: []*scenariosv1.Scenario{{Id: "plan", Name: "Your plan"}}}
}

func TestOtherModels(t *testing.T) {
	r, err := taxyear.Compute(others())
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
		if got := figure(t, r, "plan", c.path); got != c.want {
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
func TestDistributionWithoutCodeIsLeftOut(t *testing.T) {
	f := others()
	f.GetShared().GetModel().OtherIncome = append(f.GetShared().GetModel().OtherIncome, &scenariosv1.OtherIncomeModel{
		Id: "fund", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS, Payer: "Fund",
		Parts: []*scenariosv1.OtherIncomePart{{Id: "fund-f", Part: &scenariosv1.OtherIncomePart_FromForms_{FromForms: &scenariosv1.OtherIncomePart_FromForms{
			Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{Id: "fund-r", Source: &factsv1.DocumentRef{Name: "1099-R"},
				Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099R{Form_1099R: &form1099r.Form1099R{TaxYear: 2026,
					GrossDistribution: usd(5000), TaxableAmount: usd(5000), FederalTaxWithheld: usd(500), DistributionCodes: []string{""}}}}}}}}}})
	r, err := taxyear.Compute(f)
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
	if got := figure(t, r, "plan", "federal.form_1040.withheld_from_1099"); got != 62_000 {
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
func TestPaceByCadence(t *testing.T) {
	f := others()
	m := f.GetShared().GetModel()
	credit := func(id string, mo, d int32, v float64, k scenariosv1.CreditKind) *scenariosv1.AccountPart_FromCredits_Credit {
		return &scenariosv1.AccountPart_FromCredits_Credit{Id: id, Date: date(mo, d), Amount: usd(v), Kind: k}
	}
	m.Accounts = append(m.Accounts, &scenariosv1.AccountModel{Id: "fund", Name: "Fund",
		Parts: []*scenariosv1.AccountPart{{Id: "fund-p", Part: &scenariosv1.AccountPart_FromCredits_{FromCredits: &scenariosv1.AccountPart_FromCredits{Credits: []*scenariosv1.AccountPart_FromCredits_Credit{
			credit("fund-1", 3, 31, 40, scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND),
			credit("fund-2", 6, 30, 60, scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND),
			credit("fund-3", 5, 14, 300, scenariosv1.CreditKind_CREDIT_KIND_TREASURY_INTEREST)}}}}},
		DefaultPart: &scenariosv1.AccountDefault{Id: "fund-d", Rest: &scenariosv1.AccountDefault_PaceSoFar_{PaceSoFar: &scenariosv1.AccountDefault_PaceSoFar{}}}})
	entry := func(id string, mo int32) *scenariosv1.BusinessPart_FromLedger_Entry {
		return &scenariosv1.BusinessPart_FromLedger_Entry{Id: id, Date: date(mo, 10), Amount: usd(3000), Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT}
	}
	m.Businesses = []*scenariosv1.BusinessModel{{Id: "biz", Name: "Biz", Person: "you",
		Parts:       []*scenariosv1.BusinessPart{{Id: "biz-p1", Part: &scenariosv1.BusinessPart_FromLedger_{FromLedger: &scenariosv1.BusinessPart_FromLedger{Entries: []*scenariosv1.BusinessPart_FromLedger_Entry{entry("biz-1", 1), entry("biz-2", 4), entry("biz-3", 7)}}}}},
		DefaultPart: &scenariosv1.BusinessDefault{Id: "biz-d", Rest: &scenariosv1.BusinessDefault_PaceSoFar_{PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{}}}}}
	r, err := taxyear.Compute(f)
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
		if got := figure(t, r, "plan", c.path); got != c.want {
			t.Errorf("%s = %d, want %d", c.path, got, c.want)
		}
	}
}

// The broker register is as of January 1: a purchase during the year adds
// a lot, and a sale takes the earliest lots first, the purchase's among
// them, at the register's basis.
func TestRegisterAsOfJanuary(t *testing.T) {
	f := others()
	b := f.GetShared().GetModel().GetBrokers()[0]
	b.Parts = []*scenariosv1.BrokerPart{
		{Id: "schwab-b", Part: &scenariosv1.BrokerPart_Purchases_{Purchases: &scenariosv1.BrokerPart_Purchases{Purchases: []*scenariosv1.BrokerPart_Purchases_Purchase{
			{Id: "schwab-buy", Holding: "vti", Date: date(2, 1), Shares: &commonv1.Quantity{Units: 5}, Cost: usd(1500)}}}}},
		{Id: "schwab-t", Part: &scenariosv1.BrokerPart_FromTrades_{FromTrades: &scenariosv1.BrokerPart_FromTrades{Trades: []*scenariosv1.BrokerPart_FromTrades_Trade{
			{Id: "schwab-t1", Holding: "vti", Date: date(7, 1), Shares: &commonv1.Quantity{Units: 22}, Proceeds: usd(6600)}}}}},
	}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	// 22 shares: the 2020 lot's 10, long-term ($3,000 on $1,000); then the
	// Feb 1 purchase's 5 ($1,500 on $1,500) and 7 of June's 10 ($2,100 on
	// $1,750), short-term.
	if got := figure(t, r, "plan", "projection.brokers[schwab].year.items.long_term_gain"); got != 200_000 {
		t.Errorf("long-term gain %d, want $2,000", got)
	}
	if got := figure(t, r, "plan", "projection.brokers[schwab].year.items.short_term_gain"); got != 35_000 {
		t.Errorf("short-term gain %d, want $350", got)
	}
}

// A property tax payment fills the nearest installment whose due date it
// precedes by no more than 120 days; one earlier is a question, and counts
// as a payment of its own.
func TestPaymentWindow(t *testing.T) {
	f := others()
	home := f.GetShared().GetModel().GetItemised()[1].GetPropertyTax()
	home.Parts = []*scenariosv1.PropertyTaxPart{{Id: "home-p", Part: &scenariosv1.PropertyTaxPart_PaidFromBank_{PaidFromBank: &scenariosv1.PropertyTaxPart_PaidFromBank{Payments: []*scenariosv1.PropertyTaxPart_PaidFromBank_Payment{
		{Id: "home-1", Date: date(1, 2), Amount: usd(3000)},
		{Id: "home-2", Date: date(8, 12), Amount: usd(3000)}}}}}}
	r, err := taxyear.Compute(f)
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
	home.Parts[0].GetPaidFromBank().Payments[1].Date = date(8, 11)
	r, _ = taxyear.Compute(f)
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
func TestNewInputs(t *testing.T) {
	f := others()
	m := f.GetShared().GetModel()
	trades := m.GetBrokers()[0].GetParts()[0].GetFromTrades().GetTrades()
	trades[0].Basis, trades[0].Acquired = usd(4000), &commonv1.Date{Year: 2020, Month: 1, Day: 1}
	trades[0].WashSaleDisallowed, trades[0].Box = usd(100), scenariosv1.Form8949Box_FORM_8949_BOX_E
	m.Itemised = append(m.Itemised, &scenariosv1.ItemisedModel{Id: "loan", Name: "Loan", Kind: &scenariosv1.ItemisedModel_Mortgage{Mortgage: &scenariosv1.MortgageModel{
		Parts: []*scenariosv1.MortgagePart{
			{Id: "loan-s", Part: &scenariosv1.MortgagePart_FromStatements_{FromStatements: &scenariosv1.MortgagePart_FromStatements{Statements: []*scenariosv1.MortgagePart_FromStatements_Statement{
				{Id: "loan-s1", Statement: &factsv1.MortgageStatement{Date: date(6, 1), Interest: usd(1000)}}}}}},
			{Id: "loan-b", Part: &scenariosv1.MortgagePart_PaidFromBank_{PaidFromBank: &scenariosv1.MortgagePart_PaidFromBank{Payments: []*scenariosv1.MortgagePart_PaidFromBank_Payment{
				{Id: "loan-p7", Date: date(7, 1), Amount: usd(2500)}, {Id: "loan-p8", Date: date(8, 1), Amount: usd(2500)}}}}},
			{Id: "loan-r", Part: &scenariosv1.MortgagePart_RestOfYear{RestOfYear: &scenariosv1.RestOfYear{Amount: usd(3800)}}}},
		DefaultPart: &scenariosv1.MortgageDefault{Id: "loan-d", Rest: &scenariosv1.MortgageDefault_Amortised_{Amortised: &scenariosv1.MortgageDefault_Amortised{}}}}}},
		&scenariosv1.ItemisedModel{Id: "more", Name: "More gifts", Kind: &scenariosv1.ItemisedModel_Charity{Charity: &scenariosv1.CharityModel{Parts: []*scenariosv1.CharityPart{
			{Id: "more-r", Part: &scenariosv1.CharityPart_RestOfYear{RestOfYear: &scenariosv1.RestOfYear{Amount: usd(700), Date: date(12, 20)}}}}}}})
	m.Accounts = append(m.Accounts, &scenariosv1.AccountModel{Id: "fund", Name: "Fund", LastYearQualifiedShare: &commonv1.Quantity{Units: 75},
		Parts: []*scenariosv1.AccountPart{{Id: "fund-p", Part: &scenariosv1.AccountPart_FromCredits_{FromCredits: &scenariosv1.AccountPart_FromCredits{Credits: []*scenariosv1.AccountPart_FromCredits_Credit{
			{Id: "fund-1", Date: date(6, 30), Amount: usd(400), Kind: scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND}}}}}},
		DefaultPart: &scenariosv1.AccountDefault{Id: "fund-d", Rest: &scenariosv1.AccountDefault_PaceSoFar_{PaceSoFar: &scenariosv1.AccountDefault_PaceSoFar{}}}})
	entries := m.GetBusinesses()[0].GetParts()[0].GetFromLedger()
	entries.Entries = append(entries.Entries, &scenariosv1.BusinessPart_FromLedger_Entry{Id: "biz-e3", Date: date(5, 10), Amount: usd(5000),
		Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_EXPENSE, Line: scenariosv1.ScheduleCExpense_SCHEDULE_C_EXPENSE_DEPRECIATION, Category: "Equipment"})
	m.GetBusinesses()[0].Parts = m.GetBusinesses()[0].GetParts()[:1]
	m.OtherIncome = append(m.OtherIncome, &scenariosv1.OtherIncomeModel{Id: "flat", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RENTAL, Property: "Flat",
		Parts: []*scenariosv1.OtherIncomePart{{Id: "flat-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{
			Gross: usd(12000), RentalExpenses: &scenariosv1.RentalExpenses{Repairs: usd(1000), Depreciation: usd(3000)}}}}}})
	r, err := taxyear.Compute(f)
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
		if got := figure(t, r, "plan", c.path); got != c.want {
			t.Errorf("%s = %d, want %d", c.path, got, c.want)
		}
	}
	if box := plan.Proto().GetProjection().GetBrokers()[0].GetItems()[0].GetSale().GetBox(); box != scenariosv1.Form8949Box_FORM_8949_BOX_E {
		t.Errorf("box %v, want the trade's E", box)
	}
}

// An override's was is the figure the person saw; when today's computed
// figure differs, its working says the sources changed.
func TestOverrideWas(t *testing.T) {
	for _, c := range []struct {
		was     float64
		changed bool
	}{{0, false}, {9999, true}} {
		f := readExample(t)
		f.GetShared().Overrides = []*scenariosv1.Override{{Path: "projection.employers[acme].so_far.items.gross", Amount: usd(12000), Was: usd(11666.66)}}
		if c.was != 0 {
			f.GetShared().GetOverrides()[0].Was = usd(c.was)
		}
		r, err := taxyear.Compute(f)
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
func TestCarriesForwardAndRefundYear(t *testing.T) {
	yes, no := true, false
	for _, c := range []struct {
		itemised *bool
		want     int64
	}{{&yes, 50_000}, {&no, 0}} {
		f := others()
		facts := f.GetShared().GetFacts()
		facts.PriorYear = []*factsv1.PriorYearReturn{{Id: "py", Jurisdiction: "US", Status: factsv1.PriorYearReturn_STATUS_FILED,
			TotalTax: usd(1000), Itemised: &no,
			CarriesForward: []*factsv1.CarriedForward{{Kind: factsv1.CarryoverKind_CARRYOVER_KIND_LONG_TERM_CAPITAL_LOSS, Amount: usd(1200)}}}}
		f.GetShared().GetModel().OtherIncome = append(f.GetShared().GetModel().OtherIncome, &scenariosv1.OtherIncomeModel{
			Id: "ftb", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND, State: "CA", RefundYearItemised: c.itemised,
			Parts: []*scenariosv1.OtherIncomePart{{Id: "ftb-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{Gross: usd(500)}}}}})
		r, err := taxyear.Compute(f)
		if err != nil {
			t.Fatal(err)
		}
		if got := figure(t, r, "plan", "federal.schedule_d.long_term_capital_loss_carryover"); got != 120_000 {
			t.Errorf("long-term carryover %d, want last year's $1,200 carried forward", got)
		}
		if got := figure(t, r, "plan", "federal.schedule_1.taxable_state_local_refunds"); got != c.want {
			t.Errorf("refund year itemised %v: taxable refund %d, want %d", *c.itemised, got, c.want)
		}
	}
}

// A 1099-NEC states its payer's receipts for the whole year. One that
// names its payer and no client is reconciled to the ledger's receipts
// from that payer, not added to them.
func TestFormReconcilesToItsPayersReceipts(t *testing.T) {
	f := others()
	entry := func(id string, mo int32) *scenariosv1.BusinessPart_FromLedger_Entry {
		return &scenariosv1.BusinessPart_FromLedger_Entry{Id: id, Date: date(mo, 10), Amount: usd(3000), Client: "Northwind",
			Kind: scenariosv1.BusinessEntryKind_BUSINESS_ENTRY_KIND_RECEIPT}
	}
	nec := &form1099nec.Form1099NEC{TaxYear: 2026, Payer: &commonv1.Party{Name: "Northwind"}, NonemployeeCompensation: usd(7000)}
	f.GetShared().GetModel().Businesses = []*scenariosv1.BusinessModel{{Id: "biz", Name: "Biz", Person: "you",
		Parts: []*scenariosv1.BusinessPart{
			{Id: "biz-p1", Part: &scenariosv1.BusinessPart_FromLedger_{FromLedger: &scenariosv1.BusinessPart_FromLedger{Entries: []*scenariosv1.BusinessPart_FromLedger_Entry{entry("biz-1", 1), entry("biz-2", 4)}}}},
			{Id: "biz-p2", Part: &scenariosv1.BusinessPart_FromForms_{FromForms: &scenariosv1.BusinessPart_FromForms{Forms: []*scenariosv1.BusinessPart_FromForms_Form{
				{Id: "biz-nec", Form: &scenariosv1.BusinessPart_FromForms_Form_Form_1099Nec{Form_1099Nec: nec}}}}}}},
		DefaultPart: &scenariosv1.BusinessDefault{Id: "biz-d", Rest: &scenariosv1.BusinessDefault_PaceSoFar_{PaceSoFar: &scenariosv1.BusinessDefault_PaceSoFar{}}}}}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := figure(t, r, "plan", "projection.businesses[biz].year.items.receipts"); got != 700_000 {
		t.Errorf("the year's receipts are %d, want the form's 700000", got)
	}
}

// A state's tax for a finished year is its filed return's total tax; with
// the year in progress, or without the return, it isn't worked out.
func TestStateTaxFromFiledReturn(t *testing.T) {
	ca := func(f *scenariosv1.TaxYearScenarios) *commonv1.Money {
		r, err := taxyear.Compute(f)
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
	f := others()
	f.GetShared().GetFacts().Unassigned = []*factsv1.Document{{Id: "return.CA.2026",
		Form:   &factsv1.Document_CaliforniaReturn{CaliforniaReturn: &factsv1.CaliforniaReturn{Form_540: &form540.Form540{TaxYear: 2026, TotalTax: usd(2114)}}},
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
func TestBrokerTakesThePrintedBox(t *testing.T) {
	f := others()
	tx := &form1099b.Form1099B{TaxYear: 2026, Form_8949Checkbox: "D", DateAcquired: &commonv1.Date{Year: 2020, Month: 1, Day: 2},
		DateSoldOrDisposed: date(3, 4), Proceeds: usd(1000), CostOrOtherBasis: usd(400),
		HoldingPeriod: form1099b.Form1099B_HOLDING_PERIOD_LONG_TERM}
	f.GetShared().GetModel().Brokers = []*scenariosv1.BrokerModel{{Id: "b", Name: "B", Parts: []*scenariosv1.BrokerPart{
		{Id: "b-f", Part: &scenariosv1.BrokerPart_FromForms_{FromForms: &scenariosv1.BrokerPart_FromForms{Statements: []*scenariosv1.BrokerPart_FromForms_Statement{
			{Id: "b-s", Statement: &factsv1.Form1099BStatement{Transactions: []*form1099b.Form1099B{tx}}}}}}}}}}
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	boxes := r.Proto().GetScenarios()[0].GetProjection().GetBrokers()[0].GetYear().GetBoxes()
	if len(boxes) != 1 || boxes[0].GetBox() != scenariosv1.Form8949Box_FORM_8949_BOX_D {
		t.Errorf("boxes %v, want the printed D", boxes)
	}
}

// Foreign tax is what the account shows taken so far; it isn't paced.
func TestForeignTaxIsNotPaced(t *testing.T) {
	f := others()
	a := f.GetShared().GetModel().GetAccounts()[0]
	credits := a.GetParts()[0].GetFromCredits()
	credits.Credits = append(credits.Credits, &scenariosv1.AccountPart_FromCredits_Credit{Id: "ally-ft", Date: date(3, 28), Amount: usd(9.40),
		Kind: scenariosv1.CreditKind_CREDIT_KIND_FOREIGN_TAX})
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := figure(t, r, "plan", "projection.accounts[ally].year.items.foreign_tax_paid"); got != 900 {
		t.Errorf("the year's foreign tax is %d, want the $9 taken so far", got)
	}
}

// A due date still to come with a payment planned on it that doesn't meet
// it reads as planned but short, not as having nothing planned.
func TestPlannedPaymentThatFallsShort(t *testing.T) {
	f := others()
	us := f.GetShared().GetModel().GetTaxPaid()[0]
	us.Parts = append(us.Parts, &scenariosv1.TaxPaidPart{Id: "us-sep", Part: &scenariosv1.TaxPaidPart_PlannedPayment_{
		PlannedPayment: &scenariosv1.TaxPaidPart_PlannedPayment{Due: date(9, 15), Amount: usd(500)}}})
	f.GetShared().GetModel().GetBusinesses()[0].Parts[1].GetMonthly().Receipts = usd(9000)
	r, err := taxyear.Compute(f)
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
func TestClosedAccountStopsPaying(t *testing.T) {
	f := others()
	credit := func(id string, mo, d int32, v float64) *scenariosv1.AccountPart_FromCredits_Credit {
		return &scenariosv1.AccountPart_FromCredits_Credit{Id: id, Date: date(mo, d), Amount: usd(v), Kind: scenariosv1.CreditKind_CREDIT_KIND_DIVIDEND}
	}
	m := f.GetShared().GetModel()
	m.Accounts = append(m.Accounts, &scenariosv1.AccountModel{Id: "fund", Name: "Fund",
		Parts: []*scenariosv1.AccountPart{
			{Id: "fund-p", Part: &scenariosv1.AccountPart_FromCredits_{FromCredits: &scenariosv1.AccountPart_FromCredits{Credits: []*scenariosv1.AccountPart_FromCredits_Credit{
				credit("fund-1", 3, 31, 40), credit("fund-2", 6, 30, 60)}}}},
			{Id: "fund-c", Part: &scenariosv1.AccountPart_Closed_{Closed: &scenariosv1.AccountPart_Closed{On: date(9, 1)}}}},
		DefaultPart: &scenariosv1.AccountDefault{Id: "fund-d", Rest: &scenariosv1.AccountDefault_PaceSoFar_{PaceSoFar: &scenariosv1.AccountDefault_PaceSoFar{}}}})
	r, err := taxyear.Compute(f)
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
	if got := figure(t, r, "plan", "projection.accounts[fund].year.items.ordinary_dividends"); got != 10_000 {
		t.Errorf("dividends for the year %d, want the $100 paid before it closed", got)
	}
}

// A state refund is taxable only as far as deducting the tax lowered last
// year's tax: the Form 1040 instructions' State and Local Income Tax
// Refund Worksheet. With last year's return here, $16,000 itemised against
// a $15,750 standard deduction makes $250 of a $500 refund taxable. With
// only "itemised" typed, the figures aren't known: all of it counts, and
// the result asks for last year's Schedule A.
func TestStateRefundTaxBenefit(t *testing.T) {
	yes := true
	refund := &scenariosv1.OtherIncomeModel{Id: "ftb", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND, State: "CA",
		Parts: []*scenariosv1.OtherIncomePart{{Id: "ftb-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{Gross: usd(500)}}}}}
	filed := &factsv1.PriorYearReturn{Id: "py", Jurisdiction: "US", Status: factsv1.PriorYearReturn_STATUS_FILED,
		Filed: &factsv1.PriorYearReturn_Federal{Federal: &factsv1.FederalReturn{
			Form_1040: &form1040.Form1040{TaxYear: 2025, FilingStatus: form1040.FilingStatus_FILING_STATUS_SINGLE, Itemized: true},
			ScheduleA: &form1040.ScheduleA{TaxYear: 2025, StateLocalIncomeOrSalesTaxes: usd(6000), TotalStateAndLocalTaxes: usd(9000),
				StateAndLocalTaxesAfterCap: usd(9000), TotalItemizedDeductions: usd(16_000)}}}}
	typed := &factsv1.PriorYearReturn{Id: "py", Jurisdiction: "US", Status: factsv1.PriorYearReturn_STATUS_FILED, TotalTax: usd(1000), Itemised: &yes}
	for _, c := range []struct {
		name  string
		prior *factsv1.PriorYearReturn
		want  int64
		asks  bool
	}{{"return here", filed, 25_000, false}, {"itemised typed", typed, 50_000, true}} {
		f := others()
		f.GetShared().GetFacts().PriorYear = []*factsv1.PriorYearReturn{c.prior}
		f.GetShared().GetModel().OtherIncome = append(f.GetShared().GetModel().OtherIncome, refund)
		r, err := taxyear.Compute(f)
		if err != nil {
			t.Fatal(err)
		}
		plan, _ := r.Scenario("plan")
		if p := plan.Proto().GetProblems(); len(p) > 0 {
			t.Fatalf("%s: problems %v", c.name, p)
		}
		if got := figure(t, r, "plan", "federal.schedule_1.taxable_state_local_refunds"); got != c.want {
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
func TestStateRefundWithNoPriorYear(t *testing.T) {
	f := others()
	f.GetShared().GetModel().OtherIncome = append(f.GetShared().GetModel().OtherIncome, &scenariosv1.OtherIncomeModel{
		Id: "ftb", Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND, State: "CA",
		Parts: []*scenariosv1.OtherIncomePart{{Id: "ftb-y", Part: &scenariosv1.OtherIncomePart_YearAmount_{YearAmount: &scenariosv1.OtherIncomePart_YearAmount{Gross: usd(500)}}}}})
	r, err := taxyear.Compute(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := figure(t, r, "plan", "federal.schedule_1.taxable_state_local_refunds"); got != 50_000 {
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
