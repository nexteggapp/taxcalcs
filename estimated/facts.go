package estimated

import (
	"errors"
	"fmt"
	"time"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
)

// Every type in this file has unexported fields, accessors that return copies,
// and a builder whose Build validates. A value cannot change after it has been
// checked, so a jurisdiction's rules never see a Facts that Build would have
// refused.

// amount is an optional figure: a value and whether the caller gave it.
type amount struct {
	v   money.USCents
	set bool
}

func (a amount) get() (money.USCents, bool) { return a.v, a.set }

// Figures are one year's figures for one jurisdiction.
type Figures struct {
	tax           amount
	federalAGI    amount
	stateAGI      amount
	grossIncome   amount
	taxableIncome amount
	localTax      amount
	withheld      amount
}

// Tax is the year's tax after credits, before withholding and estimated
// payments, as this jurisdiction measures it, and whether the caller gave it.
// Federally that is Form 2210 line 4; for California it is FTB 5805 Part II
// line 1; for Pennsylvania it is PA-40 line 12 less lines 21 to 23. A
// jurisdiction whose penalty form starts from the tax before credits takes the
// credits in its own extras and says so.
//
// This year's tax is often not known while the year is under way. Without
// it, the rules offer every option that does not need it, such as last
// year's tax, and report the question; see [ErrNeedsThisYearTax]. A prior
// return's figures always carry it.
func (f Figures) Tax() (money.USCents, bool) { return f.tax.get() }

// FederalAGI is federal adjusted gross income, Form 1040 line 11.
func (f Figures) FederalAGI() (money.USCents, bool) { return f.federalAGI.get() }

// StateAGI is the state's own adjusted gross income, such as California AGI
// from Form 540 line 17.
func (f Figures) StateAGI() (money.USCents, bool) { return f.stateAGI.get() }

// GrossIncome is gross income, which some states test instead of tax.
func (f Figures) GrossIncome() (money.USCents, bool) { return f.grossIncome.get() }

// TaxableIncome is taxable income as this jurisdiction measures it.
func (f Figures) TaxableIncome() (money.USCents, bool) { return f.taxableIncome.get() }

// LocalTax is local income tax paid with the state's, as in Maryland and
// Indiana. Included in Tax by whoever computed Tax; held separately so a
// jurisdiction that tests it apart can.
func (f Figures) LocalTax() (money.USCents, bool) { return f.localTax.get() }

// Withheld is the year's total withholding. For the current year the dated
// [Withheld] entries on [Facts] are what counts; this is for a prior year,
// whose withholding some exceptions test.
func (f Figures) Withheld() (money.USCents, bool) { return f.withheld.get() }

// FiguresBuilder builds [Figures].
type FiguresBuilder struct{ f Figures }

// NewFigures starts a year's figures.
func NewFigures() *FiguresBuilder { return &FiguresBuilder{} }

func (b *FiguresBuilder) Tax(a money.USCents) *FiguresBuilder {
	b.f.tax = amount{a, true}
	return b
}

func (b *FiguresBuilder) FederalAGI(a money.USCents) *FiguresBuilder {
	b.f.federalAGI = amount{a, true}
	return b
}

func (b *FiguresBuilder) StateAGI(a money.USCents) *FiguresBuilder {
	b.f.stateAGI = amount{a, true}
	return b
}

func (b *FiguresBuilder) GrossIncome(a money.USCents) *FiguresBuilder {
	b.f.grossIncome = amount{a, true}
	return b
}

func (b *FiguresBuilder) TaxableIncome(a money.USCents) *FiguresBuilder {
	b.f.taxableIncome = amount{a, true}
	return b
}

func (b *FiguresBuilder) LocalTax(a money.USCents) *FiguresBuilder {
	b.f.localTax = amount{a, true}
	return b
}

func (b *FiguresBuilder) Withheld(a money.USCents) *FiguresBuilder {
	b.f.withheld = amount{a, true}
	return b
}

// Build refuses a negative tax, withholding, local tax or gross income.
// Adjusted gross income can be negative and is not checked. The tax may be
// absent; a prior return's figures need it, which [PriorReturnBuilder.Build]
// checks.
func (b *FiguresBuilder) Build() (Figures, error) {
	f := b.f
	for name, a := range map[string]amount{"tax": f.tax, "withholding": f.withheld, "local tax": f.localTax, "gross income": f.grossIncome} {
		if a.set && a.v < 0 {
			return Figures{}, fmt.Errorf("estimated: %s of %v is negative", name, a.v)
		}
	}
	return f, nil
}

// PriorReturn is last year's return for this jurisdiction.
type PriorReturn struct {
	filed             bool
	notRequired       bool
	months            int
	figures           Figures
	hasFigures        bool
	fullYearResident  bool
	residenceAnswered bool
}

// Filed reports whether a return was filed for last year.
func (p PriorReturn) Filed() bool { return p.filed }

// NotRequiredToFile reports that no return was filed because none was
// required. Federally and in California that counts as having had no
// liability.
func (p PriorReturn) NotRequiredToFile() bool { return p.notRequired }

// Months is how many months last year's return covered. Twelve for a full
// year; fewer for a short year, which no prior-year test accepts.
func (p PriorReturn) Months() int { return p.months }

// Figures are last year's figures, if a return was filed.
func (p PriorReturn) Figures() (Figures, bool) { return p.figures, p.hasFigures }

// FullYearResident reports whether the filer was resident all of last year,
// and whether the caller said. Federally this is citizen or resident alien
// for the whole year, which the no-liability exception requires; for a state
// it is residence in that state.
func (p PriorReturn) FullYearResident() (resident, answered bool) {
	return p.fullYearResident, p.residenceAnswered
}

// PriorReturnBuilder builds a [PriorReturn].
type PriorReturnBuilder struct{ p PriorReturn }

// NewPriorReturn starts last year's return. Months defaults to twelve.
func NewPriorReturn() *PriorReturnBuilder { return &PriorReturnBuilder{p: PriorReturn{months: 12}} }

func (b *PriorReturnBuilder) Filed(filed bool) *PriorReturnBuilder {
	b.p.filed = filed
	return b
}

func (b *PriorReturnBuilder) NotRequiredToFile() *PriorReturnBuilder {
	b.p.notRequired = true
	return b
}

func (b *PriorReturnBuilder) Months(m int) *PriorReturnBuilder {
	b.p.months = m
	return b
}

func (b *PriorReturnBuilder) Figures(f Figures) *PriorReturnBuilder {
	b.p.figures, b.p.hasFigures = f, true
	return b
}

func (b *PriorReturnBuilder) FullYearResident(resident bool) *PriorReturnBuilder {
	b.p.fullYearResident, b.p.residenceAnswered = resident, true
	return b
}

// Build requires figures with the tax for a filed return, refuses figures and
// "not required" together with each other's opposite, and checks the months.
func (b *PriorReturnBuilder) Build() (PriorReturn, error) {
	p := b.p
	switch {
	case p.months < 1 || p.months > 12:
		return PriorReturn{}, fmt.Errorf("estimated: a return covers 1 to 12 months, not %d", p.months)
	case p.filed && !p.hasFigures:
		return PriorReturn{}, errors.New("estimated: a filed prior return needs its figures")
	case p.hasFigures && !p.figures.tax.set:
		return PriorReturn{}, errors.New("estimated: a prior return's figures need its tax")
	case p.filed && p.notRequired:
		return PriorReturn{}, errors.New("estimated: a prior return cannot be both filed and not required")
	}
	return p, nil
}

// Withheld is tax withheld on a date.
type Withheld struct {
	on     Date
	amount money.USCents
}

func (w Withheld) On() Date              { return w.on }
func (w Withheld) Amount() money.USCents { return w.amount }

// WithheldBuilder builds a [Withheld].
type WithheldBuilder struct{ w Withheld }

func NewWithheld() *WithheldBuilder { return &WithheldBuilder{} }

func (b *WithheldBuilder) On(d Date) *WithheldBuilder {
	b.w.on = d
	return b
}

func (b *WithheldBuilder) Amount(a money.USCents) *WithheldBuilder {
	b.w.amount = a
	return b
}

func (b *WithheldBuilder) Build() (Withheld, error) {
	if b.w.on.IsZero() {
		return Withheld{}, errors.New("estimated: withholding needs a date")
	}
	if b.w.amount < 0 {
		return Withheld{}, fmt.Errorf("estimated: withholding of %v is negative", b.w.amount)
	}
	return b.w, nil
}

// Payment is an estimated payment, or last year's overpayment applied forward.
type Payment struct {
	on        Date
	amount    money.USCents
	priorYear bool
}

// On is the date paid. For an overpayment applied forward it is ignored: the
// rules treat it as paid on the first installment's due date.
func (p Payment) On() Date              { return p.on }
func (p Payment) Amount() money.USCents { return p.amount }

// FromPriorYear reports that this is last year's overpayment applied to this
// year's estimated tax.
func (p Payment) FromPriorYear() bool { return p.priorYear }

// PaymentBuilder builds a [Payment].
type PaymentBuilder struct{ p Payment }

func NewPayment() *PaymentBuilder { return &PaymentBuilder{} }

func (b *PaymentBuilder) On(d Date) *PaymentBuilder {
	b.p.on = d
	return b
}

func (b *PaymentBuilder) Amount(a money.USCents) *PaymentBuilder {
	b.p.amount = a
	return b
}

// FromPriorYear marks the payment as last year's overpayment applied forward.
func (b *PaymentBuilder) FromPriorYear() *PaymentBuilder {
	b.p.priorYear = true
	return b
}

// Build requires a date for anything but an overpayment applied forward, and
// refuses a negative amount.
func (b *PaymentBuilder) Build() (Payment, error) {
	if b.p.on.IsZero() && !b.p.priorYear {
		return Payment{}, errors.New("estimated: a payment needs a date")
	}
	if b.p.amount < 0 {
		return Payment{}, fmt.Errorf("estimated: a payment of %v is negative", b.p.amount)
	}
	return b.p, nil
}

// ResidencePeriod is a dated stretch of residence in one jurisdiction.
type ResidencePeriod struct {
	state    string
	from, to Date
}

func (r ResidencePeriod) State() string { return r.state }
func (r ResidencePeriod) From() Date    { return r.from }
func (r ResidencePeriod) To() Date      { return r.to }

// ResidencePeriodBuilder builds a [ResidencePeriod].
type ResidencePeriodBuilder struct{ r ResidencePeriod }

// NewResidencePeriod starts a period of residence in a state, named by its
// postal code.
func NewResidencePeriod(state string) *ResidencePeriodBuilder {
	return &ResidencePeriodBuilder{r: ResidencePeriod{state: state}}
}

func (b *ResidencePeriodBuilder) From(d Date) *ResidencePeriodBuilder {
	b.r.from = d
	return b
}

func (b *ResidencePeriodBuilder) To(d Date) *ResidencePeriodBuilder {
	b.r.to = d
	return b
}

func (b *ResidencePeriodBuilder) Build() (ResidencePeriod, error) {
	r := b.r
	switch {
	case len(r.state) != 2:
		return ResidencePeriod{}, fmt.Errorf("estimated: %q is not a postal code", r.state)
	case r.from.IsZero() || r.to.IsZero():
		return ResidencePeriod{}, errors.New("estimated: a residence period needs both dates")
	case r.to.Before(r.from):
		return ResidencePeriod{}, fmt.Errorf("estimated: residence from %v ends before it starts, on %v", r.from, r.to)
	}
	return r, nil
}

// Person is one filer on the return, for the waivers that turn on age,
// retirement or disability.
type Person struct {
	age            int
	retiredAfter62 bool
	disabled       bool
}

// Age is the person's age at the end of the tax year.
func (p Person) Age() int { return p.age }

// RetiredAfter62 reports retirement after reaching 62 in this year or last.
func (p Person) RetiredAfter62() bool { return p.retiredAfter62 }

// Disabled reports becoming disabled in this year or last.
func (p Person) Disabled() bool { return p.disabled }

// PersonBuilder builds a [Person].
type PersonBuilder struct{ p Person }

func NewPerson(age int) *PersonBuilder { return &PersonBuilder{p: Person{age: age}} }

func (b *PersonBuilder) RetiredAfter62() *PersonBuilder {
	b.p.retiredAfter62 = true
	return b
}

func (b *PersonBuilder) Disabled() *PersonBuilder {
	b.p.disabled = true
	return b
}

func (b *PersonBuilder) Build() (Person, error) {
	if b.p.age < 0 || b.p.age > 130 {
		return Person{}, fmt.Errorf("estimated: an age of %d is not plausible", b.p.age)
	}
	if b.p.retiredAfter62 && b.p.age < 62 {
		return Person{}, fmt.Errorf("estimated: a person aged %d cannot have retired after 62", b.p.age)
	}
	return b.p, nil
}

// Elections are the methods the filer chooses.
type Elections struct {
	annualised            bool
	withholdingAsWithheld bool
}

// Annualised reports the method that measures each installment by the income
// earned up to a period end rather than by an even share of the year:
// Schedule AI federally, FTB 5805 Part III in California, REV-1630 Exception 2
// in Pennsylvania, which uses the income to date without annualising it. It
// needs [Facts.Periods].
func (e Elections) Annualised() bool { return e.annualised }

// WithholdingAsWithheld reports crediting withholding on the dates it was
// withheld, rather than spread across the installments as the rules
// otherwise deem it.
func (e Elections) WithholdingAsWithheld() bool { return e.withholdingAsWithheld }

// ElectionsBuilder builds [Elections].
type ElectionsBuilder struct{ e Elections }

func NewElections() *ElectionsBuilder { return &ElectionsBuilder{} }

func (b *ElectionsBuilder) Annualised() *ElectionsBuilder {
	b.e.annualised = true
	return b
}

func (b *ElectionsBuilder) WithholdingAsWithheld() *ElectionsBuilder {
	b.e.withholdingAsWithheld = true
	return b
}

func (b *ElectionsBuilder) Build() (Elections, error) { return b.e, nil }

// WaiverKind is a ground for waiving the estimated tax penalty.
type WaiverKind int

const (
	// WaiverNone is an answer: the filer asks for no waiver.
	WaiverNone WaiverKind = iota

	// WaiverCasualty is a casualty, disaster or other unusual circumstance
	// that would make the penalty inequitable. 26 U.S.C. § 6654(e)(3)(A).
	WaiverCasualty

	// WaiverRetiredOrDisabled is retirement after 62 or disability, in this
	// year or last, with reasonable cause. 26 U.S.C. § 6654(e)(3)(B).
	WaiverRetiredOrDisabled
)

func (k WaiverKind) String() string {
	switch k {
	case WaiverCasualty:
		return "casualty, disaster or unusual circumstance"
	case WaiverRetiredOrDisabled:
		return "retirement after 62 or disability, with reasonable cause"
	default:
		return "no waiver"
	}
}

// WaiverCause is what a waiver request rests on, as the person gives it.
type WaiverCause int

const (
	CauseUnspecified WaiverCause = iota
	CauseCasualty
	CauseDisaster
	CauseRetirement
	CauseDisability
)

func (c WaiverCause) String() string {
	switch c {
	case CauseCasualty:
		return "a casualty"
	case CauseDisaster:
		return "a disaster"
	case CauseRetirement:
		return "retirement"
	case CauseDisability:
		return "disability"
	default:
		return "unspecified cause"
	}
}

// kind is the waiver a cause asks for.
func (c WaiverCause) kind() WaiverKind {
	switch c {
	case CauseCasualty, CauseDisaster:
		return WaiverCasualty
	case CauseRetirement, CauseDisability:
		return WaiverRetiredOrDisabled
	default:
		return WaiverNone
	}
}

// Positions are judgement calls the filer has answered.
type Positions struct {
	waiver         WaiverKind
	waiverAnswered bool
	waiverOn       Date
	cause          WaiverCause
	born           Date
}

// Waiver is the waiver the filer asks for, and whether they answered at all.
func (p Positions) Waiver() (WaiverKind, bool) { return p.waiver, p.waiverAnswered }

// WaiverOn is the day of what the waiver rests on: the casualty or
// disaster, the retirement, or the disability. The zero Date when not given.
func (p Positions) WaiverOn() Date { return p.waiverOn }

// WaiverCause is what the waiver rests on, when the person said.
func (p Positions) WaiverCause() WaiverCause { return p.cause }

// Born is the date of birth of the person a retirement waiver is for, which
// decides whether they had reached the age the rules name. The zero Date
// when not given.
func (p Positions) Born() Date { return p.born }

// PositionsBuilder builds [Positions].
type PositionsBuilder struct{ p Positions }

func NewPositions() *PositionsBuilder { return &PositionsBuilder{} }

// Waiver answers the waiver question. WaiverNone is an answer.
func (b *PositionsBuilder) Waiver(k WaiverKind) *PositionsBuilder {
	b.p.waiver, b.p.waiverAnswered = k, true
	return b
}

// WaiverOn records the day of what the waiver rests on.
func (b *PositionsBuilder) WaiverOn(d Date) *PositionsBuilder {
	b.p.waiverOn = d
	return b
}

// WaiverFor answers the waiver question with what it rests on and when,
// which sets the waiver kind.
func (b *PositionsBuilder) WaiverFor(c WaiverCause, on Date) *PositionsBuilder {
	b.p.waiver, b.p.waiverAnswered, b.p.cause, b.p.waiverOn = c.kind(), true, c, on
	return b
}

// Born records the date of birth of the person a retirement waiver is for.
func (b *PositionsBuilder) Born(d Date) *PositionsBuilder {
	b.p.born = d
	return b
}

// Build refuses an unknown waiver, and a date with no waiver asked for.
func (b *PositionsBuilder) Build() (Positions, error) {
	if b.p.waiver < WaiverNone || b.p.waiver > WaiverRetiredOrDisabled {
		return Positions{}, fmt.Errorf("estimated: waiver kind %d is not one this package knows", b.p.waiver)
	}
	if !b.p.waiverOn.IsZero() && b.p.waiver == WaiverNone {
		return Positions{}, errors.New("estimated: a waiver date with no waiver asked for")
	}
	if b.p.cause < CauseUnspecified || b.p.cause > CauseDisability {
		return Positions{}, fmt.Errorf("estimated: waiver cause %d is not one this package knows", b.p.cause)
	}
	return b.p, nil
}

// Period is one annualisation period for the annualised income installment
// method: the day it ends, the income from the start of the year to then, and
// the jurisdiction's tax after credits on that income annualised.
//
// The tax is supplied rather than computed here, because computing it needs
// the jurisdiction's rate tables, deductions and credits for the year, and this
// package holds none of them. Federally it is Schedule AI line 19; in
// California, FTB 5805 Part III line 14c. A jurisdiction that computes the
// period's tax itself from the income, as Pennsylvania does at a flat rate,
// reads [Period.Income] and ignores it.
type Period struct {
	end           Date
	income        money.USCents
	annualisedTax money.USCents
}

func (p Period) End() Date                    { return p.end }
func (p Period) Income() money.USCents        { return p.income }
func (p Period) AnnualisedTax() money.USCents { return p.annualisedTax }

// PeriodBuilder builds a [Period].
type PeriodBuilder struct{ p Period }

func NewPeriod() *PeriodBuilder { return &PeriodBuilder{} }

func (b *PeriodBuilder) End(d Date) *PeriodBuilder {
	b.p.end = d
	return b
}

func (b *PeriodBuilder) Income(a money.USCents) *PeriodBuilder {
	b.p.income = a
	return b
}

func (b *PeriodBuilder) AnnualisedTax(a money.USCents) *PeriodBuilder {
	b.p.annualisedTax = a
	return b
}

func (b *PeriodBuilder) Build() (Period, error) {
	if b.p.end.IsZero() {
		return Period{}, errors.New("estimated: a period needs its end date")
	}
	if b.p.annualisedTax < 0 {
		return Period{}, fmt.Errorf("estimated: annualised tax of %v is negative", b.p.annualisedTax)
	}
	return b.p, nil
}

// ResidentAllYear reports whether one of periods puts the filer in j from
// January 1 to December 31 of year. Two periods that meet do not add up to a
// year: the residence questions several states ask are about one residence
// held all year.
func ResidentAllYear(periods []ResidencePeriod, j Jurisdiction, year int) bool {
	jan1, dec31 := Date{year, time.January, 1}, Date{year, time.December, 31}
	for _, p := range periods {
		if p.state == string(j) && p.from == jan1 && p.to == dec31 {
			return true
		}
	}
	return false
}

// Facts are what a jurisdiction's estimated tax rules read about a household
// for one tax year.
//
// A fact only one jurisdiction reads is not here. It lives in a type that
// jurisdiction's package defines, carried by [Facts.State].
type Facts struct {
	jurisdiction   Jurisdiction
	taxYear        int
	status         filing.Status
	residence      []ResidencePeriod
	priorResidence []ResidencePeriod
	thisYear       Figures
	priorYear      PriorReturn
	hasPriorYear   bool
	withholding    []Withheld
	payments       []Payment
	returnFiled    Date
	firstMet       Date
	people         []Person
	farmShare      money.Rate
	fishShare      money.Rate
	elections      Elections
	positions      Positions
	periods        []Period
	state          any
}

func (f Facts) Jurisdiction() Jurisdiction  { return f.jurisdiction }
func (f Facts) TaxYear() int                { return f.taxYear }
func (f Facts) FilingStatus() filing.Status { return f.status }

// Residence is where the filer lived this year, in dated periods.
func (f Facts) Residence() []ResidencePeriod { return append([]ResidencePeriod(nil), f.residence...) }

// PriorResidence is where the filer lived last year.
func (f Facts) PriorResidence() []ResidencePeriod {
	return append([]ResidencePeriod(nil), f.priorResidence...)
}

// ThisYear is this year's figures, as this jurisdiction measures them.
func (f Facts) ThisYear() Figures { return f.thisYear }

// PriorYear is last year's return, and false when none is on record.
func (f Facts) PriorYear() (PriorReturn, bool) { return f.priorYear, f.hasPriorYear }

// Withholding is this year's withholding for this jurisdiction, by date.
func (f Facts) Withholding() []Withheld { return append([]Withheld(nil), f.withholding...) }

// Payments are this year's estimated payments, made or planned.
func (f Facts) Payments() []Payment { return append([]Payment(nil), f.payments...) }

// ReturnFiled is the day this year's return was filed with the whole balance
// paid, or the zero Date if it has not been. It decides the waiver of the
// January installment for filing early, and the penalty stops on it: every
// shortfall still outstanding is paid that day.
func (f Facts) ReturnFiled() Date { return f.returnFiled }

// RequirementFirstMet is the day the filer could first expect to have to make
// estimated payments for the year, or the zero Date if the caller did not say.
// Several states give a filer who first meets the requirement after a due date
// fewer installments, or excuse the earlier ones; the federal rules do not.
func (f Facts) RequirementFirstMet() Date { return f.firstMet }

// People are the filers on the return.
func (f Facts) People() []Person { return append([]Person(nil), f.people...) }

// FarmShare and FishShare are the share of gross income from farming and from
// fishing, in basis points.
func (f Facts) FarmShare() money.Rate { return f.farmShare }
func (f Facts) FishShare() money.Rate { return f.fishShare }

// Elections are the methods the filer chose.
func (f Facts) Elections() Elections { return f.elections }

// Positions are the judgement calls the filer answered.
func (f Facts) Positions() Positions { return f.positions }

// Periods are the annualisation periods, for the annualised method.
func (f Facts) Periods() []Period { return append([]Period(nil), f.periods...) }

// State is the jurisdiction's own extras, or nil. Its type is defined in that
// jurisdiction's package, such as californiataxcalc.EstimatedFacts.
func (f Facts) State() any { return f.state }

// WithheldTotal is the sum of this year's withholding.
func (f Facts) WithheldTotal() money.USCents {
	var t money.USCents
	for _, w := range f.withholding {
		t += w.amount
	}
	return t
}

// FarmerOrFisher reports whether at least two-thirds of gross income is from
// farming and fishing together, which puts a filer under separate rules
// federally and in most states.
func (f Facts) FarmerOrFisher() bool { return int64(f.farmShare+f.fishShare)*3 >= 20_000 }

// FactsBuilder builds [Facts].
type FactsBuilder struct {
	f           Facts
	hasThisYear bool
}

// NewFacts starts the facts for one jurisdiction and tax year.
func NewFacts(j Jurisdiction, taxYear int) *FactsBuilder {
	return &FactsBuilder{f: Facts{jurisdiction: j, taxYear: taxYear}}
}

func (b *FactsBuilder) FilingStatus(s filing.Status) *FactsBuilder {
	b.f.status = s
	return b
}

func (b *FactsBuilder) Residence(p ...ResidencePeriod) *FactsBuilder {
	b.f.residence = append(b.f.residence, p...)
	return b
}

func (b *FactsBuilder) PriorResidence(p ...ResidencePeriod) *FactsBuilder {
	b.f.priorResidence = append(b.f.priorResidence, p...)
	return b
}

func (b *FactsBuilder) ThisYear(f Figures) *FactsBuilder {
	b.f.thisYear, b.hasThisYear = f, true
	return b
}

func (b *FactsBuilder) PriorYear(p PriorReturn) *FactsBuilder {
	b.f.priorYear, b.f.hasPriorYear = p, true
	return b
}

func (b *FactsBuilder) Withheld(w ...Withheld) *FactsBuilder {
	b.f.withholding = append(b.f.withholding, w...)
	return b
}

func (b *FactsBuilder) Paid(p ...Payment) *FactsBuilder {
	b.f.payments = append(b.f.payments, p...)
	return b
}

func (b *FactsBuilder) ReturnFiled(d Date) *FactsBuilder {
	b.f.returnFiled = d
	return b
}

// RequirementFirstMet records the day the filer could first expect to have to
// make estimated payments.
func (b *FactsBuilder) RequirementFirstMet(d Date) *FactsBuilder {
	b.f.firstMet = d
	return b
}

func (b *FactsBuilder) People(p ...Person) *FactsBuilder {
	b.f.people = append(b.f.people, p...)
	return b
}

func (b *FactsBuilder) FarmShare(r money.Rate) *FactsBuilder {
	b.f.farmShare = r
	return b
}

func (b *FactsBuilder) FishShare(r money.Rate) *FactsBuilder {
	b.f.fishShare = r
	return b
}

func (b *FactsBuilder) Elections(e Elections) *FactsBuilder {
	b.f.elections = e
	return b
}

func (b *FactsBuilder) Positions(p Positions) *FactsBuilder {
	b.f.positions = p
	return b
}

func (b *FactsBuilder) Periods(p ...Period) *FactsBuilder {
	b.f.periods = append(b.f.periods, p...)
	return b
}

// State attaches the jurisdiction's own extras.
func (b *FactsBuilder) State(extras any) *FactsBuilder {
	b.f.state = extras
	return b
}

// Build validates the facts as a whole and returns a value nothing can change.
//
// It checks what is true of every jurisdiction: a jurisdiction and year, a
// filing status, this year's figures, withholding inside the tax year,
// payments no earlier than it, residence inside it, shares that add to no more
// than the whole, and four periods for the annualised method. Whether a fact
// suits a particular jurisdiction is that jurisdiction's check.
func (b *FactsBuilder) Build() (Facts, error) {
	f := b.f
	if len(f.jurisdiction) != 2 {
		return Facts{}, fmt.Errorf("estimated: %q is not a jurisdiction code", string(f.jurisdiction))
	}
	if f.taxYear < 1913 || f.taxYear > 9999 {
		return Facts{}, fmt.Errorf("estimated: %d is not a tax year", f.taxYear)
	}
	if f.status == filing.Unspecified {
		return Facts{}, errors.New("estimated: no filing status given")
	}
	if !b.hasThisYear {
		return Facts{}, errors.New("estimated: this year's figures are required")
	}

	jan1 := Date{f.taxYear, time.January, 1}
	dec31 := Date{f.taxYear, time.December, 31}
	inYear := func(d Date) bool { return !d.Before(jan1) && !d.After(dec31) }

	for _, w := range f.withholding {
		if !inYear(w.on) {
			return Facts{}, fmt.Errorf("estimated: withholding on %v is outside tax year %d", w.on, f.taxYear)
		}
	}
	for _, p := range f.payments {
		if !p.priorYear && p.on.Before(jan1) {
			return Facts{}, fmt.Errorf("estimated: a payment on %v is before tax year %d began", p.on, f.taxYear)
		}
	}
	for _, r := range f.residence {
		if !inYear(r.from) || !inYear(r.to) {
			return Facts{}, fmt.Errorf("estimated: residence %v to %v is outside tax year %d", r.from, r.to, f.taxYear)
		}
	}
	for _, r := range f.priorResidence {
		if r.from.Year() != f.taxYear-1 || r.to.Year() != f.taxYear-1 {
			return Facts{}, fmt.Errorf("estimated: prior residence %v to %v is outside %d", r.from, r.to, f.taxYear-1)
		}
	}
	if !f.firstMet.IsZero() && !inYear(f.firstMet) {
		return Facts{}, fmt.Errorf("estimated: the requirement cannot first be met on %v, outside tax year %d", f.firstMet, f.taxYear)
	}
	if !f.returnFiled.IsZero() && f.returnFiled.Year() <= f.taxYear {
		return Facts{}, fmt.Errorf("estimated: a %d return cannot be filed on %v", f.taxYear, f.returnFiled)
	}
	if f.farmShare < 0 || f.fishShare < 0 || f.farmShare+f.fishShare > 10_000 {
		return Facts{}, fmt.Errorf("estimated: farming %v and fishing %v are not shares of one income", f.farmShare, f.fishShare)
	}
	if f.elections.annualised && len(f.periods) != 4 {
		return Facts{}, fmt.Errorf("estimated: the annualised method needs four periods, not %d", len(f.periods))
	}
	for i := 1; i < len(f.periods); i++ {
		if !f.periods[i].end.After(f.periods[i-1].end) {
			return Facts{}, errors.New("estimated: annualisation periods must end in order")
		}
	}

	f.residence = append([]ResidencePeriod(nil), f.residence...)
	f.priorResidence = append([]ResidencePeriod(nil), f.priorResidence...)
	f.withholding = append([]Withheld(nil), f.withholding...)
	f.payments = append([]Payment(nil), f.payments...)
	f.people = append([]Person(nil), f.people...)
	f.periods = append([]Period(nil), f.periods...)
	return f, nil
}
