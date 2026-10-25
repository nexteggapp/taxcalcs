package federal

import (
	"errors"
	"fmt"
	"time"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	"github.com/nexteggapp/taxcalcs/taxyear/model/retirement"
	"github.com/nexteggapp/taxcalcs/taxyear/payroll"
	"github.com/nexteggapp/taxcalcs/ustaxcalc"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
)

// wages fills lines 1a and 25a from each job's W-2 box totals for the year,
// and works out Schedule 3 line 11, the Social Security tax withheld across
// a person's jobs over the year's maximum.
func (c *calc) wages() error {
	f := c.r.form.GetForm_1040()
	c.people = map[string]*payrollPerson{}
	byID := map[string]*scenariosv1.EmployerModel{}
	for _, m := range c.inputs.GetModel().GetEmployers() {
		byID[m.GetId()] = m
	}
	var wages, withheld money.USCents
	var paths []string
	for _, e := range c.proj.GetEmployers() {
		b := e.GetYear().GetBoxes()
		wages += model.Value(b.GetWages())
		withheld += model.Value(b.GetFederalWithheld())
		paths = append(paths, fmt.Sprintf("projection.employers[%s].year.boxes.wages", e.GetId()))
		c.person(byID[e.GetId()], b)
		for _, s := range b.GetStates() {
			c.r.stateWithheld[s.GetState()] += model.Value(s.GetWithheld())
		}
	}
	f.W2Wages, f.TotalWages = model.Money(wages), model.Money(wages)
	f.WithheldFromW2 = model.Money(withheld)
	c.r.term("federal.form_1040.w2_wages", nil, paths...)
	c.income = wages
	return c.excessSocialSecurity()
}

// excessSocialSecurity is Schedule 3 line 11: for a person with more than
// one job, the Social Security tax withheld, each job's held to the year's
// maximum, over that maximum.
func (c *calc) excessSocialSecurity() error {
	base, err := payroll.WageBase(c.ctx.Year())
	if err != nil {
		return err
	}
	most := payroll.SocialSecurityRate.Of(base)
	for id, p := range c.people {
		if len(p.ssWithheld) < 2 {
			continue
		}
		var total money.USCents
		for _, w := range p.ssWithheld {
			total += min(w, most)
		}
		if over := total - most; over > 0 {
			c.r.excess += over
			w := c.r.working("federal.schedule_3.excess_social_security_withheld")
			w.Rules = append(w.Rules, rule("26 U.S.C. § 31(b); Schedule 3 line 11",
				fmt.Sprintf("%v of Social Security tax withheld for %s across jobs, each held to %v, is %v over the year's maximum", total, id, most, over),
				"federal.schedule_3.excess_social_security_withheld"))
		}
	}
	c.r.form.GetSchedule_3().ExcessSocialSecurityWithheld = model.Money(c.r.excess)
	return nil
}

// person adds one job's W-2 boxes to its person's payroll figures. A job
// with workplace plan deferrals, or marked covered, covers the person for
// the § 219(g) limit.
func (c *calc) person(m *scenariosv1.EmployerModel, b *scenariosv1.W2Boxes) {
	p := c.people[m.GetPerson()]
	if p == nil {
		p = &payrollPerson{}
		c.people[m.GetPerson()] = p
	}
	p.ssWages += model.Value(b.GetSocialSecurityWages())
	p.medicareWages += model.Value(b.GetMedicareWages())
	p.ssWithheld = append(p.ssWithheld, model.Value(b.GetSocialSecurityWithheld()))
	deferred := model.Value(b.GetK401()) + model.Value(b.GetK403B()) + model.Value(b.GetRoth_401K()) + model.Value(b.GetRoth_403B())
	p.covered = p.covered || m.GetCoveredByPlan() || deferred > 0
}

// selfEmployment works out Schedule SE for each person with business
// profit, taking their W-2 Social Security and Medicare wages first.
func (c *calc) selfEmployment() error {
	profit := map[string]money.USCents{}
	byID := map[string]*scenariosv1.BusinessModel{}
	for _, m := range c.inputs.GetModel().GetBusinesses() {
		byID[m.GetId()] = m
	}
	var total money.USCents
	var order []string
	for _, b := range c.proj.GetBusinesses() {
		person := byID[b.GetId()].GetPerson()
		if _, ok := profit[person]; !ok {
			order = append(order, person)
		}
		v := model.Value(b.GetYear().GetBoxes().GetNetProfit())
		profit[person] += v
		total += v
	}
	sch1 := c.r.form.GetSchedule_1()
	sch1.BusinessIncomeOrLoss = model.Money(total)
	c.income += total
	for _, person := range order {
		if profit[person] == 0 {
			continue
		}
		p := c.people[person]
		if p == nil {
			p = &payrollPerson{}
		}
		res, err := ustaxcalc.SelfEmploymentTax(c.ctx.Year(), c.r.status, profit[person], p.ssWages, p.medicareWages)
		if err != nil {
			return err
		}
		c.add("federal.schedule_2.self_employment_tax", res.Applied(), res.Required())
		s := res.Value()
		// Schedule SE's lines are each rounded once, where they are worked
		// out, and its totals add them.
		s.NetEarnings, s.SocialSecurity = model.Dollars(s.NetEarnings), model.Dollars(s.SocialSecurity)
		s.Medicare, s.AboveTheLineDeduction = model.Dollars(s.Medicare), model.Dollars(s.AboveTheLineDeduction)
		c.seTax += s.SocialSecurity + s.Medicare
		c.seHalf += s.AboveTheLineDeduction
		c.r.form.ScheduleSe = append(c.r.form.ScheduleSe, &form1040.ScheduleSE{TaxYear: int32(c.ctx.Year()),
			NetBusinessProfitOrLoss: model.Money(profit[person]), NetEarningsAfterReduction: model.Money(s.NetEarnings),
			SocialSecurityTax: model.Money(s.SocialSecurity), MedicareTax: model.Money(s.Medicare),
			SelfEmploymentTax: model.Money(s.SocialSecurity + s.Medicare), OneHalfDeduction: model.Money(s.AboveTheLineDeduction)})
	}
	c.r.form.GetSchedule_2().SelfEmploymentTax = model.Money(c.seTax)
	sch1.DeductiblePartOfSelfEmploymentTax = model.Money(c.seHalf)
	return nil
}

// investments fills lines 2 and 3 from the accounts' 1099 box totals, and
// Schedule D from the brokers' Form 8949 boxes, home sales, and last year's
// carryovers.
func (c *calc) investments() error {
	f := c.r.form.GetForm_1040()
	var interest, exempt, ordinary, qualified, dist money.USCents
	for _, a := range c.proj.GetAccounts() {
		b := a.GetYear().GetBoxes()
		interest += model.Value(b.GetInterest()) + model.Value(b.GetTreasuryInterest())
		exempt += model.Value(b.GetTaxExemptInterest())
		ordinary += model.Value(b.GetOrdinaryDividends())
		qualified += model.Value(b.GetQualifiedDividends())
		dist += model.Value(b.GetCapitalGainDistributions())
	}
	f.TaxableInterest, f.TaxExemptInterest = model.Money(interest), model.Money(exempt)
	f.OrdinaryDividends, f.QualifiedDividends = model.Money(ordinary), model.Money(qualified)
	c.qualified = qualified
	var short, long money.USCents
	for _, b := range c.proj.GetBrokers() {
		for _, x := range b.GetYear().GetBoxes() {
			if x.GetBox() >= scenariosv1.Form8949Box_FORM_8949_BOX_D {
				long += model.Value(x.GetGain())
			} else {
				short += model.Value(x.GetGain())
			}
		}
	}
	otherByID := map[string]*scenariosv1.OtherIncomeModel{}
	for _, m := range c.inputs.GetModel().GetOtherIncome() {
		otherByID[m.GetId()] = m
	}
	for _, o := range c.proj.GetOtherIncome() {
		m := otherByID[o.GetId()]
		if !isHomeSale(m) {
			continue
		}
		for _, it := range o.GetItems() {
			if x := it.GetOtherIncome(); x != nil {
				if x.Taxable != nil {
					long += model.Value(x.GetTaxable())
				} else {
					long += model.Value(x.GetGross())
				}
			}
		}
	}
	shortCarry := c.carryover(factsv1.CarryoverKind_CARRYOVER_KIND_SHORT_TERM_CAPITAL_LOSS)
	longCarry := c.carryover(factsv1.CarryoverKind_CARRYOVER_KIND_LONG_TERM_CAPITAL_LOSS)
	g, err := ustaxcalc.NewCapitalGains().ShortTerm(short).LongTerm(long).ShortTermCarryover(shortCarry).
		LongTermCarryover(longCarry).Distributions(dist).Build()
	if err != nil {
		return err
	}
	net, err := ustaxcalc.NetCapitalGains(c.ctx.Year(), c.r.status, g)
	if err != nil {
		return err
	}
	c.add("federal.form_1040.capital_gain_or_loss", net.Applied(), net.Required())
	c.gains = net.Value()
	sd := c.r.form.GetScheduleD()
	sd.ShortTermCapitalLossCarryover, sd.LongTermCapitalLossCarryover = model.Money(shortCarry), model.Money(longCarry)
	sd.CapitalGainDistributions = model.Money(dist)
	sd.NetShortTermGainOrLoss, sd.NetLongTermGainOrLoss = model.Money(c.gains.ShortTerm()), model.Money(c.gains.LongTerm())
	sd.NetGainOrLoss = model.Money(c.gains.Combined())
	f.CapitalGainOrLoss = model.Money(c.gains.InIncome())
	c.income += interest + ordinary + c.gains.InIncome()
	return nil
}

// carryover is what last year carried into this one of a kind: the facts'
// carryovers, else what last year's federal return says it carries
// forward.
func (c *calc) carryover(kind factsv1.CarryoverKind) money.USCents {
	var t money.USCents
	found := false
	for _, co := range c.facts.GetCarryovers() {
		if co.GetKind() == kind {
			t, found = t+model.Value(co.GetAmount()), true
		}
	}
	if found {
		return t
	}
	for _, p := range c.facts.GetPriorYear() {
		if p.GetJurisdiction() != "US" || len(p.GetPeople()) > 0 {
			continue
		}
		for _, cf := range p.GetCarriesForward() {
			if cf.GetKind() == kind {
				t += model.Value(cf.GetAmount())
			}
		}
	}
	return t
}

// isHomeSale reports whether an other income model represents a home sale,
// either by its kind or by containing a Form 1099-S or a home sale one-off part.
func isHomeSale(m *scenariosv1.OtherIncomeModel) bool {
	if m == nil {
		return false
	}
	if m.GetKind() == scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE {
		return true
	}
	for _, p := range m.GetParts() {
		if p.GetOneOff().GetHomeSale() != nil {
			return true
		}
		for _, f := range p.GetFromForms().GetForms() {
			if f.GetForm_1099S() != nil {
				return true
			}
		}
	}
	return false
}

// other adds other income by kind: retirement distributions on lines 4 and
// 5 with the 10% additional tax on early ones; unemployment, alimony,
// gambling and rents on Schedule 1; a state refund when last year itemised.
// Social Security benefits are left out, as ustaxcalc carries no rule for
// them yet. Taxable gain from a home sale is routed to Schedule D in
// investments.
func (c *calc) other() error {
	byID := map[string]*scenariosv1.OtherIncomeModel{}
	for _, m := range c.inputs.GetModel().GetOtherIncome() {
		byID[m.GetId()] = m
	}
	var t otherTotals
	var sums []*otherSum
	// Every row gets its summary, whatever the return below does.
	defer func() { c.otherSummary(sums, t) }()
	for _, o := range c.proj.GetOtherIncome() {
		var kind scenariosv1.OtherIncomeKind
		m := byID[o.GetId()]
		if m != nil {
			kind = m.GetKind()
			if kind == scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNSPECIFIED && isHomeSale(m) {
				kind = scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE
			}
		}
		sum := &otherSum{id: o.GetId(), kind: kind, from: len(t.dists), rentFrom: t.rent, refundFrom: t.refund}
		sums = append(sums, sum)
		for _, it := range o.GetItems() {
			sum.add(m, it.GetOtherIncome())
			if m != nil && m.GetKind() == scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND && m.RefundYearItemised != nil {
				// The refund's own year says whether it is income.
				t.withheld += model.Value(it.GetOtherIncome().GetFederalWithheld())
				if m.GetRefundYearItemised() {
					t.decidedRefund += model.Value(it.GetOtherIncome().GetGross())
				}
				continue
			}
			if !t.add(kind, it.GetOtherIncome(), &c.r.notes) {
				sum.unknown = true
			}
		}
		sum.to, sum.rent, sum.refund = len(t.dists), t.rent-sum.rentFrom, t.refund-sum.refundFrom
	}
	f := c.r.form.GetForm_1040()
	f.IraDistributions, f.IraDistributionsTaxable = model.Money(t.ira), model.Money(t.iraTaxable)
	f.PensionsAndAnnuities, f.PensionsAndAnnuitiesTaxable = model.Money(t.pension), model.Money(t.pensionTaxable)
	f.WithheldFrom_1099 = model.Money(t.withheld)
	if t.socialSecurity != 0 {
		f.SocialSecurityBenefits = model.Money(t.socialSecurity)
	}
	if err := c.earlyDistributions(t.dists); err != nil {
		return err
	}
	if t.rent < 0 {
		res, err := ustaxcalc.RentalLossAllowed(c.r.status, -t.rent, c.income+t.iraTaxable+t.pensionTaxable+t.unemployment, false)
		if err != nil {
			return err
		}
		c.add("federal.schedule_1.rental_royalty_partnership_s_corp_trust", res.Applied(), res.Required())
		t.rent = -model.Dollars(res.Value())
	}
	undecided, itemised := c.stateRefunds(t.refund, t.decidedRefund)
	c.refundsAll, c.refundsTaxable = t.refund, undecided
	c.itemisedAll, c.itemisedTaxable = t.decidedRefund, itemised
	t.refund = undecided + itemised
	c.schedule1(t)
	return nil
}

// hsaDeduction holds a person's direct HSA contributions to what their
// limit leaves after the contributions through payroll and from their
// employers, § 223(b)(4), which the jobs have already held to it.
func (c *calc) hsaDeduction(id string, direct money.USCents) money.USCents {
	limit, ok := retirement.HsaLimit(c.ctx, id, &c.r.notes)
	if !ok {
		if direct > 0 {
			c.r.notes.LeaveOut("The HSA contribution limit, without the coverage or the year's figures; direct contributions are deducted as given")
		}
		return direct
	}
	var payroll money.USCents
	for _, e := range c.proj.GetEmployers() {
		for _, m := range c.inputs.GetModel().GetEmployers() {
			if m.GetId() == e.GetId() && m.GetPerson() == id {
				payroll += model.Value(e.GetYear().GetBoxes().GetHsa())
			}
		}
	}
	c.hsaLimits[id] = limit
	return min(direct, max(0, limit-payroll))
}

// otherSum is one other income model's year as the return reads it.
type otherSum struct {
	id                     string
	kind                   scenariosv1.OtherIncomeKind
	taxable, withheld, net money.USCents
	from, to               int // its distributions among the year's
	rentFrom, rent         money.USCents
	refundFrom, refund     money.USCents
	decided                bool
	// itemised is its state refunds whose own model says their year
	// itemised.
	itemised money.USCents
	// unknown is set when an item was left out, so the taxable amount
	// isn't known.
	unknown bool
}

func (s *otherSum) add(m *scenariosv1.OtherIncomeModel, x *scenariosv1.OtherIncomeFigures) {
	gross, taxable := model.Value(x.GetGross()), model.Value(x.GetGross())
	if x.GetTaxable() != nil {
		taxable = model.Value(x.GetTaxable())
	}
	s.withheld += model.Value(x.GetFederalWithheld())
	switch {
	case m.GetKind() == scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND && m.RefundYearItemised != nil:
		s.decided = true
		if m.GetRefundYearItemised() {
			s.itemised += gross
		}
	case m.GetKind() == scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_SOCIAL_SECURITY:
		s.net += gross
	default:
		s.taxable += taxable
	}
}

// otherSummary records each other income model's figures on the return:
// a rental loss shares what is allowed, and a state refund whose year is
// unsaid shares what the rules count of all of them.
func (c *calc) otherSummary(sums []*otherSum, t otherTotals) {
	var losses money.USCents
	for _, s := range sums {
		if s.rent < 0 {
			losses += -s.rent
		}
	}
	for _, s := range sums {
		out := &scenariosv1.OtherIncomeSummary{Id: s.id, Withheld: model.Money(s.withheld)}
		taxable := s.taxable
		switch {
		case s.rent < 0 && losses > 0:
			allowed := model.Scale(-t.rent, -s.rent, losses)
			taxable = -allowed
			out.LossCarried = model.Money(-s.rent - allowed)
		case s.kind == scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND && !s.decided && c.refundsAll > 0:
			taxable = model.Scale(c.refundsTaxable, s.refund, c.refundsAll)
		case s.itemised > 0 && c.itemisedAll > 0:
			taxable = model.Scale(c.itemisedTaxable, s.itemised, c.itemisedAll)
		}
		if !s.unknown {
			out.Taxable = model.Money(taxable)
		}
		if s.kind == scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_SOCIAL_SECURITY {
			out.NetBenefits = model.Money(s.net)
		}
		if s.to > s.from {
			if res, err := ustaxcalc.EarlyDistributionTax(c.ctx.Year(), t.dists[s.from:s.to]); err == nil {
				out.EarlyDistributionTax = model.Money(model.Dollars(res.Value()))
			}
		}
		c.r.summary.OtherIncome = append(c.r.summary.OtherIncome, out)
	}
}

// otherTotals is the year's other income by the line it reaches.
type otherTotals struct {
	ira, iraTaxable, pension, pensionTaxable      money.USCents
	unemployment, alimony, gambling, refund, rent money.USCents
	socialSecurity, withheld, other               money.USCents
	// decidedRefund is the state refunds whose own model says the year
	// that deducted them itemised.
	decidedRefund money.USCents
	dists         []ustaxcalc.RetirementDistribution
}

// add counts one item by its model's kind. It returns false when the item
// is left out of the return's lines, and its withholding alone counts.
func (t *otherTotals) add(kind scenariosv1.OtherIncomeKind, x *scenariosv1.OtherIncomeFigures, notes *model.Notes) bool {
	gross := model.Value(x.GetGross())
	taxable := gross
	if x.GetTaxable() != nil {
		taxable = model.Value(x.GetTaxable())
	}
	t.withheld += model.Value(x.GetFederalWithheld())
	switch kind {
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RETIREMENT_WITHDRAWALS:
		code := "7"
		if len(x.GetDistributionCodes()) > 0 {
			code = x.GetDistributionCodes()[0]
		}
		d, err := ustaxcalc.NewRetirementDistribution(taxable, code)
		if err != nil {
			notes.LeaveOut("A Form 1099-R distribution without a box 7 code, or with a negative taxable amount, which can't be taxed; it is left out of lines 4 and 5")
			return false
		}
		t.dists = append(t.dists, d)
		if x.GetIra() {
			t.ira, t.iraTaxable = t.ira+gross, t.iraTaxable+taxable
		} else {
			t.pension, t.pensionTaxable = t.pension+gross, t.pensionTaxable+taxable
		}
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNEMPLOYMENT:
		t.unemployment += gross
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_ALIMONY:
		t.alimony += gross
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_GAMBLING:
		t.gambling += gross
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_STATE_REFUND:
		t.refund += gross
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_RENTAL:
		t.rent += gross - model.Value(x.GetExpenses())
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_SOCIAL_SECURITY:
		t.socialSecurity += gross
		notes.LeaveOut("The taxable part of Social Security benefits, which this build doesn't work out yet")
	case scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE:
		// Taxable gain on a home sale is capital gain routed to Schedule D in investments.
		// Any federal withholding is counted into t.withheld above.
	default:
		t.other += taxable
	}
	return true
}

// earlyDistributions is the 10% additional tax on early distributions,
// Schedule 2 line 8.
func (c *calc) earlyDistributions(dists []ustaxcalc.RetirementDistribution) error {
	if len(dists) == 0 {
		return nil
	}
	res, err := ustaxcalc.EarlyDistributionTax(c.ctx.Year(), dists)
	if err != nil {
		return err
	}
	c.add("federal.schedule_2.tax_on_iras_and_tax_favoured_accounts", res.Applied(), res.Required())
	c.early = model.Dollars(res.Value())
	c.r.form.GetSchedule_2().TaxOnIrasAndTaxFavouredAccounts = model.Money(c.early)
	if codes := ustaxcalc.UnhandledDistributionCodes(dists); len(codes) > 0 {
		c.r.notes.LeaveOut(fmt.Sprintf("How distributions coded %v on Form 1099-R are taxed, beyond counting their taxable amount", codes))
	}
	return nil
}

// schedule1 writes Schedule 1's additional income and line 8, and adds
// the other income to total income.
func (c *calc) schedule1(t otherTotals) {
	f, sch1 := c.r.form.GetForm_1040(), c.r.form.GetSchedule_1()
	sch1.UnemploymentCompensation, sch1.AlimonyReceived = model.Money(t.unemployment), model.Money(t.alimony)
	sch1.RentalRoyaltyPartnershipSCorpTrust, sch1.TaxableStateLocalRefunds = model.Money(t.rent), model.Money(t.refund)
	if t.gambling != 0 {
		sch1.OtherIncome = append(sch1.OtherIncome, &form1040.Schedule1Item{Description: "Gambling winnings", Amount: model.Money(t.gambling)})
	}
	if t.other != 0 {
		sch1.OtherIncome = append(sch1.OtherIncome, &form1040.Schedule1Item{Description: "Other income", Amount: model.Money(t.other)})
	}
	// HSA contributions from employers over the limit, on paychecks already
	// made, aren't in W-2 wages: the Instructions for Form 8889 put them in
	// other income.
	var hsa money.USCents
	for _, e := range c.proj.GetEmployers() {
		hsa += model.Value(e.GetHsaOverInIncome())
	}
	if hsa != 0 {
		sch1.OtherIncome = append(sch1.OtherIncome, &form1040.Schedule1Item{Description: "Excess employer HSA contributions", Amount: model.Money(hsa)})
	}
	sch1.TotalOtherIncome = model.Money(t.gambling + hsa + t.other)
	additional := model.Value(sch1.GetBusinessIncomeOrLoss()) + t.unemployment + t.alimony + t.gambling + hsa + t.rent + t.refund + t.other
	sch1.TotalAdditionalIncome = model.Money(additional)
	f.AdditionalIncome = model.Money(additional)
	c.income += t.iraTaxable + t.pensionTaxable + t.unemployment + t.alimony + t.gambling + hsa + t.rent + t.refund + t.other
}

// stateRefunds is the taxable part of last year's state refunds: undecided
// is those whose year the facts' prior-year return decides, and itemised
// those whose own model says their year itemised. A refund is taxable only
// as far as deducting the tax lowered that year's tax, by the State and
// Local Income Tax Refund Worksheet, which needs last year's Form 1040 and
// Schedule A. Without them, a refund from a year that itemised counts in
// full and the result asks for them; with no word on itemising, the
// undecided refunds count in full and the result asks.
func (c *calc) stateRefunds(undecided, itemised money.USCents) (money.USCents, money.USCents) {
	if undecided+itemised == 0 {
		return 0, 0
	}
	prior := c.priorFederal()
	f, a := prior.GetFederal().GetForm_1040(), prior.GetFederal().GetScheduleA()
	said := prior != nil && prior.Itemised != nil
	switch {
	case f != nil && f.GetItemized() && a != nil:
		if taxable, ok := c.refundWorksheet(undecided+itemised, f, a); ok {
			u := model.Scale(taxable, undecided, undecided+itemised)
			return u, taxable - u
		}
	case f != nil && !f.GetItemized(), said && !prior.GetItemised():
		if itemised == 0 {
			return 0, 0
		}
		undecided = 0
	case f == nil && !said && undecided > 0:
		c.r.notes.Ask(model.NewQuestion("federal/state-refund", "federal.schedule_1.taxable_state_local_refunds",
			"Did you itemise deductions on last year's federal return?", "Your state refund counts as income until you say.",
			"facts.prior_year"))
		if itemised == 0 {
			return undecided, 0
		}
	}
	c.r.notes.LeaveOut(fmt.Sprintf("How much of a state refund is taxable: without your %d return, all of it is counted", c.ctx.Year()-1))
	c.r.notes.Ask(model.NewQuestion("federal/state-refund-schedule-a", "federal.schedule_1.taxable_state_local_refunds",
		"What were the figures on last year's Schedule A?", "All of your state refund counts as income until you give them.",
		"facts.prior_year"))
	return undecided, itemised
}

// priorFederal is last year's federal return in the facts, the household's
// own before a separate one's, or nil.
func (c *calc) priorFederal() *factsv1.PriorYearReturn {
	var out *factsv1.PriorYearReturn
	for _, p := range c.facts.GetPriorYear() {
		if p.GetJurisdiction() == "US" && (out == nil || len(p.GetPeople()) == 0 && len(out.GetPeople()) > 0) {
			out = p
		}
	}
	return out
}

// itemisedTotal is last year's Schedule A line 17, or without it Form 1040
// line 12, the itemised deduction taken.
func itemisedTotal(f *form1040.Form1040, a *form1040.ScheduleA) money.USCents {
	if a.GetTotalItemizedDeductions() != nil {
		return model.Value(a.GetTotalItemizedDeductions())
	}
	return model.Value(f.GetDeduction())
}

// refundWorksheet runs the State and Local Income Tax Refund Worksheet on
// refund over last year's Form 1040 and Schedule A. It reports false when
// this build has no table for last year.
func (c *calc) refundWorksheet(refund money.USCents, f *form1040.Form1040, a *form1040.ScheduleA) (money.USCents, bool) {
	incomeTax := model.Value(a.GetStateLocalIncomeOrSalesTaxes())
	if a.GetElectedGeneralSalesTaxes() {
		// Sales tax was deducted in place of income tax, so no income tax
		// refund lowered last year's tax.
		incomeTax = 0
	}
	boxes := 0
	for _, b := range []bool{f.GetFilerBornBeforeCutoff(), f.GetFilerBlind(), f.GetSpouseBornBeforeCutoff(), f.GetSpouseBlind()} {
		if b {
			boxes++
		}
	}
	w, err := ustaxcalc.NewStateRefund(refund, statusOf(f.GetFilingStatus())).PriorIncomeTax(incomeTax).
		PriorSALT(model.Value(a.GetTotalStateAndLocalTaxes()), min(model.Value(a.GetStateAndLocalTaxesAfterCap()), model.Value(a.GetTotalStateAndLocalTaxes()))).
		PriorItemized(itemisedTotal(f, a)).PriorBoxes(boxes).Build()
	if err != nil {
		return 0, false
	}
	res, err := ustaxcalc.StateRefundTaxable(c.ctx.Year(), w)
	if err != nil {
		return 0, false
	}
	c.add("federal.schedule_1.taxable_state_local_refunds", res.Applied(), res.Required())
	return res.Value(), true
}

// adjustments works out Schedule 1's adjustments: half of self-employment
// tax, direct HSA contributions, and each person's IRA deduction under
// § 219, then adjusted gross income.
func (c *calc) adjustments() error {
	sch1, f := c.r.form.GetSchedule_1(), c.r.form.GetForm_1040()
	var traditional, roth, hsa = map[string]money.USCents{}, map[string]money.USCents{}, map[string]money.USCents{}
	people := map[string]string{}
	var savers []string
	for _, m := range c.inputs.GetModel().GetRetirement() {
		if _, seen := hsa[m.GetPerson()]; !seen {
			savers = append(savers, m.GetPerson())
			hsa[m.GetPerson()] = 0
		}
		people[m.GetId()] = m.GetPerson()
	}
	for _, r := range c.proj.GetRetirement() {
		t := r.GetYear().GetItems()
		traditional[people[r.GetId()]] += model.Value(t.GetTraditionalIra())
		roth[people[r.GetId()]] += model.Value(t.GetRothIra())
		hsa[people[r.GetId()]] += model.Value(t.GetHsa())
	}
	for _, id := range savers {
		hsa[id] = c.hsaDeduction(id, hsa[id])
		c.hsa += hsa[id]
	}
	magi := c.income - c.seHalf - c.hsa
	var deduction money.USCents
	ded := map[string]money.USCents{}
	limits := map[string]*ustaxcalc.IRALimits{}
	for _, id := range c.onReturn() {
		d, lim, err := c.ira(id, traditional[id], roth[id], magi)
		if err != nil {
			return err
		}
		deduction += d
		ded[id], limits[id] = d, lim
	}
	// Everyone on the return has an entry, saver or not, so a spouse's
	// limits show before their answers are in.
	for _, id := range union(c.onReturn(), savers) {
		c.retirementSummary(id, traditional[id], roth[id], ded[id]+hsa[id], limits[id])
	}
	sch1.HealthSavingsAccountDeduction, sch1.IraDeduction = model.Money(c.hsa), model.Money(deduction)
	total := c.seHalf + c.hsa + deduction
	sch1.TotalAdjustments = model.Money(total)
	f.TotalIncome = model.Money(c.income)
	f.AdjustmentsToIncome = model.Money(total)
	c.r.agi = c.income - total
	f.AdjustedGrossIncome = model.Money(c.r.agi)
	agi := "federal.form_1040.adjusted_gross_income"
	c.r.term(agi, nil, "federal.form_1040.total_income")
	c.r.minus(agi, "federal.form_1040.adjustments_to_income")
	w := c.r.working(agi)
	w.Rules = append(w.Rules, rule("26 U.S.C. § 62; Form 1040, lines 9 to 11",
		fmt.Sprintf("%v of income less %v of adjustments is adjusted gross income of %v", c.income, total, c.r.agi),
		"federal.form_1040.total_income", "federal.form_1040.adjustments_to_income", agi))
	return nil
}

// ira is one person's deductible traditional IRA contribution, and adds
// any § 4973 tax on an excess, by ustaxcalc's § 219 and § 408A limits.
func (c *calc) ira(id string, traditional, roth, magi money.USCents) (money.USCents, *ustaxcalc.IRALimits, error) {
	if traditional == 0 && roth == 0 {
		return 0, nil, nil
	}
	b := ustaxcalc.NewIRAContribution(traditional).Roth(roth)
	if age, ok := c.ctx.AgeAtYearEnd(id); ok && age >= 50 {
		b.CatchUp()
	}
	if p := c.people[id]; p != nil {
		b.ActiveParticipant(p.covered)
	}
	if spouse := c.spouseOf(id); spouse != "" {
		if p := c.people[spouse]; p != nil {
			b.SpouseActiveParticipant(p.covered)
		}
	}
	contribution, err := b.Build()
	if err != nil {
		return 0, nil, err
	}
	res, err := ustaxcalc.IRAContributionLimits(c.ctx.Year(), c.r.status, contribution, magi)
	if errors.Is(err, ustaxcalc.ErrNoTable) {
		c.r.notes.LeaveOut("Whether your traditional IRA contribution is deductible")
		return 0, nil, nil
	}
	if err != nil {
		return 0, nil, err
	}
	c.add("federal.schedule_1.ira_deduction", res.Applied(), res.Required())
	lim := res.Value()
	c.excessIRA += model.Dollars(lim.ExcessTax())
	return model.Dollars(lim.Deduction()), &lim, nil
}

// union is a's ids, then b's not in a.
func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range append(append([]string(nil), a...), b...) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// onReturn is the ids of the people the return is for: the filer, and the
// spouse on a joint return.
func (c *calc) onReturn() []string {
	var out []string
	var spouse bool
	for _, p := range c.facts.GetPeople() {
		switch p.GetRelationship() {
		case factsv1.Relationship_RELATIONSHIP_YOU:
			out = append(out, p.GetId())
		case factsv1.Relationship_RELATIONSHIP_SPOUSE:
			if c.r.status == filing.MarriedFilingJointly {
				out, spouse = append(out, p.GetId()), true
			}
		}
	}
	if len(out) == 0 {
		out = []string{"you"}
	}
	// A joint return has a spouse before the facts hold them, named
	// "spouse" until they are added.
	if c.r.status == filing.MarriedFilingJointly && !spouse {
		out = append(out, spouseID)
	}
	return out
}

// spouseID is the spouse's id on a joint return whose facts don't hold
// them yet.
const spouseID = "spouse"

func (c *calc) spouseOf(id string) string {
	married := c.r.status == filing.MarriedFilingJointly || c.r.status == filing.MarriedFilingSeparately
	if !married {
		return ""
	}
	for _, p := range c.facts.GetPeople() {
		if p.GetId() != id && (p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_SPOUSE || p.GetRelationship() == factsv1.Relationship_RELATIONSHIP_YOU) {
			return p.GetId()
		}
	}
	return ""
}

// conditions are the § 63(f) facts for the filer and, on a joint return,
// the spouse.
func (c *calc) conditions() ustaxcalc.Conditions {
	var out ustaxcalc.Conditions
	for _, p := range c.facts.GetPeople() {
		born := p.GetDateOfBirth()
		aged := born.GetYear() != 0 && ustaxcalc.Aged65OrOlder(c.ctx.Year(),
			time.Date(int(born.GetYear()), time.Month(max(1, born.GetMonth())), int(max(1, born.GetDay())), 0, 0, 0, 0, time.UTC))
		switch p.GetRelationship() {
		case factsv1.Relationship_RELATIONSHIP_YOU:
			out.Aged65OrOlder, out.Blind = aged, p.GetBlind()
		case factsv1.Relationship_RELATIONSHIP_SPOUSE:
			if c.r.status == filing.MarriedFilingJointly {
				out.SpouseAged65OrOlder, out.SpouseBlind = aged, p.GetBlind()
			}
		}
	}
	return out
}
