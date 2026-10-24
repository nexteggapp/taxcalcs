package estimated

import (
	"errors"
	"fmt"
	"strings"

	"github.com/nexteggapp/taxcalcs/money"
)

// The answers a jurisdiction gives. Each has the same shape in every
// jurisdiction, because the questions are the same, and each is built through
// a builder that checks it is internally consistent.

// Basis is what a required annual payment option is a percentage of.
type Basis int

const (
	BasisUnspecified Basis = iota

	// BasisThisYearTax is this year's tax: 90% of it federally and in most
	// states.
	BasisThisYearTax

	// BasisPriorYearTax is last year's tax: 100%, or 110% at higher income.
	BasisPriorYearTax

	// BasisPriorYearIncome is last year's income taxed at this year's rate,
	// which Pennsylvania uses instead of last year's tax.
	BasisPriorYearIncome

	// BasisException is an exception under which no estimated payments are
	// needed: tax less withholding under a threshold, no tax last year, or a
	// jurisdiction's own exemption. Its amount is zero.
	BasisException
)

func (b Basis) String() string {
	switch b {
	case BasisThisYearTax:
		return "this year's tax"
	case BasisPriorYearTax:
		return "last year's tax"
	case BasisPriorYearIncome:
		return "last year's income at this year's rate"
	case BasisException:
		return "an exception"
	default:
		return "unspecified basis"
	}
}

// TargetOption is one way of measuring the required annual payment.
type TargetOption struct {
	basis     Basis
	percent   money.Rate
	amount    money.USCents
	available bool
	whyNot    string
	label     string
}

func (o TargetOption) Basis() Basis          { return o.basis }
func (o TargetOption) Percent() money.Rate   { return o.percent }
func (o TargetOption) Amount() money.USCents { return o.amount }
func (o TargetOption) Available() bool       { return o.available }

// WhyNot says why an unavailable option is unavailable, in plain words.
func (o TargetOption) WhyNot() string { return o.whyNot }

// Label names the option in plain words, as "90% of this year's tax" or
// "Tax less withholding under $1,000".
func (o TargetOption) Label() string { return o.label }

// TargetOptionBuilder builds a [TargetOption].
type TargetOptionBuilder struct {
	o    TargetOption
	free bool // labelled by hand through NewException
}

// NewTargetOption starts an option that is available unless [Unavailable] is
// called.
func NewTargetOption(basis Basis, percent money.Rate, amount money.USCents) *TargetOptionBuilder {
	return &TargetOptionBuilder{o: TargetOption{basis: basis, percent: percent, amount: amount, available: true}}
}

// Unavailable marks the option as not open to this filer, and says why.
func (b *TargetOptionBuilder) Unavailable(why string) *TargetOptionBuilder {
	b.o.available, b.o.whyNot = false, why
	return b
}

// Label replaces the label Build would give the option.
func (b *TargetOptionBuilder) Label(label string) *TargetOptionBuilder {
	b.o.label = label
	return b
}

// NewException starts an exception option with a plain label that names no
// amount or percentage, as "No tax due last year" or "Return filed and paid
// by Jan 31, 2027". An exception that compares an amount with a threshold is
// made with [ExceptionBelow] or [ExceptionAtMost], which check the
// comparison.
func NewException(label string) *TargetOptionBuilder {
	return &TargetOptionBuilder{o: TargetOption{basis: BasisException, available: true, label: label}, free: true}
}

// ExceptionBelow is the exception that applies because what is under limit,
// labelled "<what> under <limit>". It is an error when amount is not under
// limit, so the label is true of the figures it was made from.
func ExceptionBelow(what string, amount, limit money.USCents) (TargetOption, error) {
	if amount >= limit {
		return TargetOption{}, fmt.Errorf("estimated: %s of %v is not under %v", what, amount, limit)
	}
	return (&TargetOptionBuilder{o: TargetOption{basis: BasisException, available: true,
		label: fmt.Sprintf("%s under %s", what, limit.Whole())}}).Build()
}

// ExceptionAtMost is the exception that applies because what is no more than
// limit, labelled "<what> of <limit> or less". It is an error when amount is
// over limit.
func ExceptionAtMost(what string, amount, limit money.USCents) (TargetOption, error) {
	if amount > limit {
		return TargetOption{}, fmt.Errorf("estimated: %s of %v is over %v", what, amount, limit)
	}
	return (&TargetOptionBuilder{o: TargetOption{basis: BasisException, available: true,
		label: fmt.Sprintf("%s of %s or less", what, limit.Whole())}}).Build()
}

// defaultLabel names an option by its basis and percentage.
func defaultLabel(o TargetOption) string {
	switch o.basis {
	case BasisThisYearTax:
		return fmt.Sprintf("%v of this year's tax", o.percent)
	case BasisPriorYearTax:
		return fmt.Sprintf("%v of last year's tax", o.percent)
	case BasisPriorYearIncome:
		return "Last year's income at this year's rate"
	}
	return ""
}

// Build checks the option, and labels it by its basis and percentage when no
// label was given. An exception is always available, at no amount and no
// percentage, and a label given to [NewException] names no amount.
func (b *TargetOptionBuilder) Build() (TargetOption, error) {
	o := b.o
	if o.label == "" {
		o.label = defaultLabel(o)
	}
	switch {
	case o.basis == BasisException && (o.amount != 0 || o.percent != 0 || !o.available):
		return TargetOption{}, errors.New("estimated: an exception is available, at no amount and no percentage")
	case o.basis == BasisException && o.label == "":
		return TargetOption{}, errors.New("estimated: an exception needs a label")
	case b.free && strings.ContainsAny(o.label, "$%"):
		return TargetOption{}, fmt.Errorf("estimated: exception %q names a figure; use ExceptionBelow or ExceptionAtMost", o.label)
	}
	switch {
	case o.basis == BasisUnspecified:
		return TargetOption{}, errors.New("estimated: an option needs a basis")
	case o.percent < 0 || o.amount < 0:
		return TargetOption{}, errors.New("estimated: an option's percentage and amount cannot be negative")
	case !o.available && o.whyNot == "":
		return TargetOption{}, errors.New("estimated: an unavailable option must say why")
	case !o.available && o.amount != 0:
		return TargetOption{}, fmt.Errorf("estimated: the unavailable %v option carries %v; an option that isn't open has no amount", o.basis, o.amount)
	case o.available && o.whyNot != "":
		return TargetOption{}, errors.New("estimated: an available option has no reason to be unavailable")
	}
	return o, nil
}

// Requirement is whether a household has to make estimated payments, and what
// total keeps it clear of the penalty.
type Requirement struct {
	required bool
	why      string
	target   money.USCents
	options  []TargetOption
	chosen   int
	exempt   bool
	noTarget bool
}

// Required reports whether estimated payments are needed on top of the
// withholding.
func (r Requirement) Required() bool { return r.required }

// Why says, in plain words, why payments are or are not required.
func (r Requirement) Why() string { return r.why }

// Target is the required annual payment: what withholding and estimated
// payments together must reach to avoid the penalty. It is zero when an
// exception is the target, and when no option can be measured.
func (r Requirement) Target() money.USCents { return r.target }

// Exempt reports that an exception means no penalty can arise, whatever is
// paid. The exception is then the chosen option.
func (r Requirement) Exempt() bool { return r.exempt }

// TargetKnown reports whether any option could be measured. It is false when
// this year's tax was not given and no other option is available, and then
// there is no target, no schedule and no penalty to show.
func (r Requirement) TargetKnown() bool { return !r.noTarget }

// Options are every way of measuring the target, including those this filer
// cannot use and why.
func (r Requirement) Options() []TargetOption { return append([]TargetOption(nil), r.options...) }

// Chosen is the option the target came from: exactly one whenever
// [Requirement.TargetKnown], an exception included, and none otherwise.
func (r Requirement) Chosen() (TargetOption, bool) {
	if r.chosen < 0 {
		return TargetOption{}, false
	}
	return r.options[r.chosen], true
}

// RequirementBuilder builds a [Requirement].
type RequirementBuilder struct {
	r         Requirement
	chosen    *Basis
	exempt    bool
	exception TargetOption
	noTarget  bool
}

func NewRequirement() *RequirementBuilder { return &RequirementBuilder{r: Requirement{chosen: -1}} }

func (b *RequirementBuilder) Required(required bool, why string) *RequirementBuilder {
	b.r.required, b.r.why = required, why
	return b
}

func (b *RequirementBuilder) Option(o TargetOption) *RequirementBuilder {
	b.r.options = append(b.r.options, o)
	return b
}

// Choose sets the target from the available option with this basis.
func (b *RequirementBuilder) Choose(basis Basis) *RequirementBuilder {
	b.chosen = &basis
	return b
}

// Exempt makes the exception the target: no penalty can arise, so the
// target is zero. The exception is added to the options and chosen.
func (b *RequirementBuilder) Exempt(exception TargetOption) *RequirementBuilder {
	b.exempt, b.exception = true, exception
	return b
}

// NoTarget sets no target, because no option can be measured: this year's
// tax was not given and every other option is unavailable.
func (b *RequirementBuilder) NoTarget() *RequirementBuilder {
	b.noTarget = true
	return b
}

// Build checks the target is the chosen option's amount, and that the chosen
// option is available and is the smallest available one: the rules give the
// filer the lesser of their options, never a larger one.
func (b *RequirementBuilder) Build() (Requirement, error) {
	r := b.r
	r.options = append([]TargetOption(nil), r.options...)
	if r.why == "" {
		return Requirement{}, errors.New("estimated: a requirement must say why")
	}
	if b.noTarget {
		switch {
		case b.exempt || b.chosen != nil:
			return Requirement{}, errors.New("estimated: a requirement with no target has no chosen option and no exemption")
		case r.required:
			return Requirement{}, errors.New("estimated: a requirement with no target cannot say payments are required")
		}
		for _, o := range r.options {
			if o.available {
				return Requirement{}, fmt.Errorf("estimated: %v is available, so there is a target", o.basis)
			}
		}
		r.noTarget = true
		return r, nil
	}
	for _, o := range r.options {
		if o.basis == BasisException {
			return Requirement{}, errors.New("estimated: an exception is added with Exempt, which chooses it")
		}
	}
	if b.exempt {
		switch {
		case b.chosen != nil:
			return Requirement{}, errors.New("estimated: an exempt requirement chooses its exception and nothing else")
		case r.required:
			return Requirement{}, errors.New("estimated: an exempt household is not required to pay")
		case b.exception.basis != BasisException || b.exception.label == "":
			return Requirement{}, errors.New("estimated: an exemption needs an exception made by NewException, ExceptionBelow or ExceptionAtMost")
		}
		r.options = append(r.options, b.exception)
		r.chosen, r.exempt, r.target = len(r.options)-1, true, 0
		return r, nil
	}
	if b.chosen == nil {
		return Requirement{}, errors.New("estimated: a requirement needs a chosen option or an exemption")
	}
	for i, o := range r.options {
		if o.basis == *b.chosen {
			r.chosen = i
		}
	}
	if r.chosen < 0 || !r.options[r.chosen].available {
		return Requirement{}, fmt.Errorf("estimated: the chosen option, %v, is not an available option", *b.chosen)
	}
	r.target = r.options[r.chosen].amount
	for _, o := range r.options {
		if o.available && o.amount < r.target {
			return Requirement{}, fmt.Errorf("estimated: %v gives %v, less than the chosen %v", o.basis, o.amount, r.target)
		}
	}
	return r, nil
}

// Status is where an installment stands.
type Status int

const (
	StatusUnspecified Status = iota

	// StatusMet is a due date by which everything needed had been credited.
	StatusMet

	// StatusShort is a due date by which less than was needed had been
	// credited.
	StatusShort

	// StatusNothingDue is a due date by which nothing is needed.
	StatusNothingDue

	// StatusExcused is an installment on which no penalty arises, whatever
	// was paid: the January installment for a return filed and paid by the end
	// of January, or an installment an exception covers, as Pennsylvania's
	// REV-1630 Exceptions 1 and 2. An excused installment can still show a
	// shortfall, which then bears no penalty.
	StatusExcused
)

// StatusOf is the status every jurisdiction gives a due date: nothing due
// when nothing is needed by it, met when what was credited by it reaches
// what was needed, excused when it is not met but an exception covers it,
// and short otherwise.
func StatusOf(neededBy, creditedBy money.USCents, excused bool) Status {
	switch {
	case neededBy <= 0:
		return StatusNothingDue
	case creditedBy >= neededBy:
		return StatusMet
	case excused:
		return StatusExcused
	default:
		return StatusShort
	}
}

func (s Status) String() string {
	switch s {
	case StatusMet:
		return "met"
	case StatusShort:
		return "short"
	case StatusNothingDue:
		return "nothing due"
	case StatusExcused:
		return "excused"
	default:
		return "unspecified status"
	}
}

// Installment is one due date: what must have been paid by it, what had been
// credited by it, and what was short.
type Installment struct {
	number     int
	due        Date
	neededBy   money.USCents
	creditedBy money.USCents
	shortfall  money.USCents
	excused    bool
	status     Status
}

// Number is the installment's position, from 1.
func (i Installment) Number() int { return i.number }

// Due is the due date, already moved for weekends and holidays.
func (i Installment) Due() Date { return i.due }

// NeededBy is the running total required by the due date.
func (i Installment) NeededBy() money.USCents { return i.neededBy }

// CreditedBy is the running total of withholding and payments credited by the
// due date.
func (i Installment) CreditedBy() money.USCents { return i.creditedBy }

// Shortfall is this installment's own underpayment on its due date, as Form
// 2210 line 17 and FTB 5805 Worksheet II line 8 compute it. An earlier
// installment's underpayment is not repeated here.
func (i Installment) Shortfall() money.USCents { return i.shortfall }

// Status is [StatusOf] the running totals.
func (i Installment) Status() Status { return i.status }

// InstallmentBuilder builds an [Installment].
type InstallmentBuilder struct{ i Installment }

func NewInstallment(number int, due Date) *InstallmentBuilder {
	return &InstallmentBuilder{i: Installment{number: number, due: due}}
}

func (b *InstallmentBuilder) NeededBy(a money.USCents) *InstallmentBuilder {
	b.i.neededBy = a
	return b
}

func (b *InstallmentBuilder) CreditedBy(a money.USCents) *InstallmentBuilder {
	b.i.creditedBy = a
	return b
}

func (b *InstallmentBuilder) Shortfall(a money.USCents) *InstallmentBuilder {
	b.i.shortfall = a
	return b
}

// Excused records that an exception covers the installment, so no penalty
// arises on it whatever was paid.
func (b *InstallmentBuilder) Excused() *InstallmentBuilder {
	b.i.excused = true
	return b
}

// Build checks the amounts are not negative, sets the status by [StatusOf],
// and checks the shortfall agrees with it: only a short or excused
// installment has one.
func (b *InstallmentBuilder) Build() (Installment, error) {
	i := b.i
	i.status = StatusOf(i.neededBy, i.creditedBy, i.excused)
	switch {
	case i.number < 1:
		return Installment{}, fmt.Errorf("estimated: installment number %d", i.number)
	case i.due.IsZero():
		return Installment{}, errors.New("estimated: an installment needs a due date")
	case i.neededBy < 0 || i.creditedBy < 0 || i.shortfall < 0:
		return Installment{}, errors.New("estimated: an installment's amounts cannot be negative")
	case i.shortfall > 0 && i.status != StatusShort && i.status != StatusExcused:
		return Installment{}, fmt.Errorf("estimated: installment %d is %v with a shortfall of %v", i.number, i.status, i.shortfall)
	}
	return i, nil
}

// PartKind is what kind of charge a penalty part is.
type PartKind int

const (
	PartUnspecified PartKind = iota

	// PartInterest is interest on the unpaid amount for the days or months
	// it stayed unpaid.
	PartInterest

	// PartFlat is a flat percentage of the shortfall, as Indiana's 10%.
	PartFlat

	// PartMonthly is a percentage per month or part of a month, as Vermont's
	// and Michigan's penalty on top of interest.
	PartMonthly
)

func (k PartKind) String() string {
	switch k {
	case PartInterest:
		return "interest"
	case PartFlat:
		return "flat"
	case PartMonthly:
		return "monthly"
	default:
		return "unspecified part"
	}
}

// PenaltyPart is one computation on a penalty worksheet: an amount, a span, a
// rate and the charge.
type PenaltyPart struct {
	kind      PartKind
	principal money.USCents
	from, to  Date
	days      int
	months    int
	rate      money.FineRate
	charge    money.USCents
	source    string
}

func (p PenaltyPart) Kind() PartKind { return p.kind }

// Principal is the unpaid amount the charge is on.
func (p PenaltyPart) Principal() money.USCents { return p.principal }

// From and To bound the span. Interest runs for the days after From up to and
// including To.
func (p PenaltyPart) From() Date { return p.from }
func (p PenaltyPart) To() Date   { return p.to }

// Days is the days counted, for interest by the day.
func (p PenaltyPart) Days() int { return p.days }

// Months is the months counted, for a charge by the month.
func (p PenaltyPart) Months() int { return p.months }

// Rate is the annual rate for interest, the monthly rate for a monthly
// charge, or the percentage for a flat one, to the nearest basis point.
func (p PenaltyPart) Rate() money.Rate { return p.rate.Nearest() }

// FineRate is [PenaltyPart.Rate] as published, which differs from it only
// for a rate basis points cannot hold.
func (p PenaltyPart) FineRate() money.FineRate { return p.rate }

func (p PenaltyPart) Charge() money.USCents { return p.charge }

// Source cites the document that set the rate.
func (p PenaltyPart) Source() string { return p.source }

// PenaltyPartBuilder builds a [PenaltyPart].
type PenaltyPartBuilder struct{ p PenaltyPart }

func NewPenaltyPart(kind PartKind, principal money.USCents, rate money.Rate) *PenaltyPartBuilder {
	return &PenaltyPartBuilder{p: PenaltyPart{kind: kind, principal: principal, rate: rate.Fine()}}
}

// FineRate replaces the rate given to [NewPenaltyPart] with one basis points
// cannot hold.
func (b *PenaltyPartBuilder) FineRate(r money.FineRate) *PenaltyPartBuilder {
	b.p.rate = r
	return b
}

func (b *PenaltyPartBuilder) Span(from, to Date) *PenaltyPartBuilder {
	b.p.from, b.p.to = from, to
	return b
}

func (b *PenaltyPartBuilder) Days(n int) *PenaltyPartBuilder {
	b.p.days = n
	return b
}

func (b *PenaltyPartBuilder) Months(n int) *PenaltyPartBuilder {
	b.p.months = n
	return b
}

func (b *PenaltyPartBuilder) Charge(a money.USCents) *PenaltyPartBuilder {
	b.p.charge = a
	return b
}

func (b *PenaltyPartBuilder) Source(cite string) *PenaltyPartBuilder {
	b.p.source = cite
	return b
}

func (b *PenaltyPartBuilder) Build() (PenaltyPart, error) {
	p := b.p
	switch {
	case p.kind == PartUnspecified:
		return PenaltyPart{}, errors.New("estimated: a penalty part needs a kind")
	case p.principal < 0 || p.charge < 0 || p.rate < 0 || p.days < 0 || p.months < 0:
		return PenaltyPart{}, errors.New("estimated: a penalty part's figures cannot be negative")
	case p.source == "":
		return PenaltyPart{}, errors.New("estimated: a penalty part must cite the source of its rate")
	case p.kind == PartInterest && (p.from.IsZero() || p.to.Before(p.from)):
		return PenaltyPart{}, errors.New("estimated: an interest part needs a span that runs forward")
	}
	return p, nil
}

// PenaltyLine is the penalty on one installment's shortfall.
type PenaltyLine struct {
	installment int
	due         Date
	shortfall   money.USCents
	days        int
	parts       []PenaltyPart
	total       money.USCents
}

// Installment is the number of the installment this line is for.
func (l PenaltyLine) Installment() int { return l.installment }
func (l PenaltyLine) Due() Date        { return l.due }

// Shortfall is the installment's underpayment on its due date.
func (l PenaltyLine) Shortfall() money.USCents { return l.shortfall }

// Days is how long the last of the shortfall stayed unpaid, up to the date
// the penalty was figured to.
func (l PenaltyLine) Days() int { return l.days }

// Parts are the computations, one for each amount and rate period.
func (l PenaltyLine) Parts() []PenaltyPart { return append([]PenaltyPart(nil), l.parts...) }

// Total is the sum of the parts.
func (l PenaltyLine) Total() money.USCents { return l.total }

// PenaltyLineBuilder builds a [PenaltyLine].
type PenaltyLineBuilder struct{ l PenaltyLine }

func NewPenaltyLine(installment int, due Date, shortfall money.USCents) *PenaltyLineBuilder {
	return &PenaltyLineBuilder{l: PenaltyLine{installment: installment, due: due, shortfall: shortfall}}
}

func (b *PenaltyLineBuilder) Days(n int) *PenaltyLineBuilder {
	b.l.days = n
	return b
}

func (b *PenaltyLineBuilder) Part(p PenaltyPart) *PenaltyLineBuilder {
	b.l.parts = append(b.l.parts, p)
	return b
}

// Build totals the parts and checks no part charges interest on more than
// the shortfall.
func (b *PenaltyLineBuilder) Build() (PenaltyLine, error) {
	l := b.l
	l.parts = append([]PenaltyPart(nil), l.parts...)
	if l.installment < 1 || l.due.IsZero() {
		return PenaltyLine{}, errors.New("estimated: a penalty line needs its installment and due date")
	}
	if l.shortfall <= 0 {
		return PenaltyLine{}, errors.New("estimated: a penalty line needs a shortfall")
	}
	l.total = 0
	for _, p := range l.parts {
		if p.principal > l.shortfall {
			return PenaltyLine{}, fmt.Errorf("estimated: a part charges on %v, more than the shortfall of %v", p.principal, l.shortfall)
		}
		l.total += p.charge
	}
	return l, nil
}

// Penalty is the underpayment penalty, installment by installment.
type Penalty struct {
	lines      []PenaltyLine
	total      money.USCents
	waiver     WaiverKind
	asOf       Date
	upperBound bool
	reason     string
	through    Date
	outcome    WaiverOutcome

	// what the reason was derived from, kept for ToBuilder
	req      Requirement
	schedule []Installment
	context  string
}

// Reason is one or two plain sentences saying what decided the penalty: the
// exception that applies, that every due date was met, or how many were
// short. Build derives it from the requirement, the schedule and the lines,
// so it states only what those figures show.
func (p Penalty) Reason() string { return p.reason }

// Total is the penalty before any waiver.
func (p Penalty) Total() money.USCents { return p.total }

// Lines are one per installment that was short.
func (p Penalty) Lines() []PenaltyLine { return append([]PenaltyLine(nil), p.lines...) }

// Waiver is the waiver the filer asks for. The tax authority decides it, so
// [Total] does not subtract it.
func (p Penalty) Waiver() WaiverKind { return p.waiver }

// AsOf is the date the penalty was figured to.
func (p Penalty) AsOf() Date { return p.asOf }

// UpperBound reports that the penalty is the most it can be rather than what
// it is: this year's tax was not given, so it is figured against a target
// that can only be too high, and it is less if this year's tax turns out
// lower. It is false when the interest stops at [Penalty.RatedThrough],
// because the penalty then leaves days out and is no maximum.
func (p Penalty) UpperBound() bool { return p.upperBound && p.through.IsZero() }

// RatedThrough is the last day a published rate covers, when the penalty
// stops there with shortfalls still running; the zero Date when it runs to
// its end. Days after it bear more interest at a rate not yet published.
func (p Penalty) RatedThrough() Date { return p.through }

// WaiverOutcome is what the rules make of the waiver asked for, and false
// when none was asked for or no penalty arises.
func (p Penalty) WaiverOutcome() (WaiverOutcome, bool) {
	return p.outcome, p.outcome.state != waiverUnset
}

// IfWaived is the penalty if an eligible waiver is granted: the charges of
// the kinds it leaves, [WaiverOutcome.Kept], and nothing when it covers them
// all. For a waiver refused or undecided, or none asked for, it is the
// penalty.
func (p Penalty) IfWaived() money.USCents {
	if !p.outcome.Eligible() {
		return p.total
	}
	var left money.USCents
	for _, l := range p.lines {
		for _, part := range l.parts {
			for _, k := range p.outcome.kept {
				if part.kind == k {
					left += part.charge
				}
			}
		}
	}
	return left
}

// ShortDueDates is how many due dates in the schedule the penalty was
// figured from are short: what was credited by them falls short of what
// was needed. The reason's count is this number.
func (p Penalty) ShortDueDates() int {
	short, _ := shortAndExcused(p.schedule)
	return short
}

// Summary is the penalty in one sentence a person reads: "No penalty.",
// "About $109.", "At most $109, and less if this year's tax turns out
// lower.", "About $109 through Dec 31, 2026, then more at a rate not yet
// published.", or, for a shortfall that only runs past the last published
// rate, "No penalty through Jan 15, 2027; after that it accrues at an
// unpublished rate."
func (p Penalty) Summary() string {
	switch {
	case p.total == 0 && p.through.IsZero():
		return "No penalty."
	case p.total == 0:
		return fmt.Sprintf("No penalty through %s; after that it accrues at an unpublished rate.", shortDate(p.through))
	case !p.through.IsZero():
		return fmt.Sprintf("About %s through %s, then more at a rate not yet published.", p.total.Whole(), shortDate(p.through))
	case p.upperBound:
		return fmt.Sprintf("At most %s, and less if this year's tax turns out lower.", p.total.Whole())
	default:
		return fmt.Sprintf("About %s.", p.total.Whole())
	}
}

// shortDate is "Dec 31, 2026".
func shortDate(d Date) string { return fmt.Sprintf("%s %d, %d", d.month.String()[:3], d.day, d.year) }

// PenaltyBuilder builds a [Penalty].
type PenaltyBuilder struct {
	p        Penalty
	req      Requirement
	schedule []Installment
	because  bool
	context  string
}

func NewPenalty(asOf Date) *PenaltyBuilder { return &PenaltyBuilder{p: Penalty{asOf: asOf}} }

// Because records the requirement and the schedule the penalty was figured
// from, which Build reads for [Penalty.Reason]. Every penalty needs it.
func (b *PenaltyBuilder) Because(r Requirement, schedule []Installment) *PenaltyBuilder {
	b.req, b.schedule, b.because = r, append([]Installment(nil), schedule...), true
	return b
}

func (b *PenaltyBuilder) Line(l PenaltyLine) *PenaltyBuilder {
	b.p.lines = append(b.p.lines, l)
	return b
}

func (b *PenaltyBuilder) Waiver(k WaiverKind) *PenaltyBuilder {
	b.p.waiver = k
	return b
}

// ToBuilder starts a builder holding everything in p, for a rule that
// computes a penalty with a helper and then adds to it.
func (p Penalty) ToBuilder() *PenaltyBuilder {
	return &PenaltyBuilder{
		p:   Penalty{lines: append([]PenaltyLine(nil), p.lines...), waiver: p.waiver, asOf: p.asOf, upperBound: p.upperBound, through: p.through, outcome: p.outcome},
		req: p.req, schedule: append([]Installment(nil), p.schedule...), because: true, context: p.context,
	}
}

// Context adds a second sentence to the derived reason: the reason
// "Payments fell short on two due dates." with the sentence "The January
// payment still counts after a move." reads "Payments fell short on two due
// dates. The January payment still counts after a move." It is for a rule
// of the jurisdiction's that bears on the reason, and names no amount or
// percentage: those come only from the figures the reason is derived from.
func (b *PenaltyBuilder) Context(sentence string) *PenaltyBuilder {
	b.context = sentence
	return b
}

// ExceptionApplies is the sentence a penalty's reason gives when an
// exception is the target: its label, as a sentence.
func ExceptionApplies(o TargetOption) string { return o.label + "." }

// penaltyReason says what decided the penalty, from the requirement, the
// schedule and the lines.
func penaltyReason(r Requirement, schedule []Installment, p Penalty) (string, error) {
	if r.exempt {
		if p.total != 0 {
			return "", fmt.Errorf("estimated: an exception applies, yet the penalty is %v", p.total)
		}
		return ExceptionApplies(r.options[r.chosen]), nil
	}
	short, excused := shortAndExcused(schedule)
	switch {
	case short == 1:
		return "Payments fall short on one due date.", nil
	case short > 1:
		return fmt.Sprintf("Payments fall short on %s due dates.", count(short)), nil
	case p.total > 0:
		return "A penalty is charged on what was paid late.", nil
	case excused == 0:
		return "Your payments meet every due date.", nil
	default:
		return "The due dates that fall short are excused.", nil
	}
}

// shortAndExcused counts the installments that are short, and those that
// are short but excused.
func shortAndExcused(schedule []Installment) (short, excused int) {
	for _, i := range schedule {
		switch i.status {
		case StatusShort:
			short++
		case StatusExcused:
			excused++
		}
	}
	return short, excused
}

func count(n int) string {
	if n >= 0 && n < 10 {
		return [...]string{"no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}[n]
	}
	return fmt.Sprint(n)
}

// WaiverOutcome records what the rules make of the waiver asked for. Every
// penalty where a waiver was asked for and a penalty arises needs one.
func (b *PenaltyBuilder) WaiverOutcome(o WaiverOutcome) *PenaltyBuilder {
	b.p.outcome = o
	return b
}

// RatedThrough records that the interest stops at d, the last day a
// published rate covers, with shortfalls still running after it.
func (b *PenaltyBuilder) RatedThrough(d Date) *PenaltyBuilder {
	b.p.through = d
	return b
}

// UpperBound marks the penalty as the most it can be.
func (b *PenaltyBuilder) UpperBound() *PenaltyBuilder {
	b.p.upperBound = true
	return b
}

// Build totals the lines and checks there is one per installment at most.
func (b *PenaltyBuilder) Build() (Penalty, error) {
	p := b.p
	p.lines = append([]PenaltyLine(nil), p.lines...)
	if p.asOf.IsZero() {
		return Penalty{}, errors.New("estimated: a penalty needs the date it is figured to")
	}
	seen := map[int]bool{}
	p.total = 0
	for _, l := range p.lines {
		if seen[l.installment] {
			return Penalty{}, fmt.Errorf("estimated: installment %d has two penalty lines", l.installment)
		}
		seen[l.installment] = true
		p.total += l.total
	}
	if !b.because {
		return Penalty{}, errors.New("estimated: a penalty needs the requirement and schedule it was figured from")
	}
	if p.outcome.state != waiverUnset {
		if err := p.outcome.check(); err != nil {
			return Penalty{}, err
		}
	}
	reason, err := penaltyReason(b.req, b.schedule, p)
	if err != nil {
		return Penalty{}, err
	}
	if b.context != "" {
		body := strings.TrimSuffix(b.context, ".")
		if body == "" || !strings.HasSuffix(b.context, ".") || strings.ContainsAny(body, "$%.?!;") || strings.ToUpper(body[:1]) != body[:1] {
			return Penalty{}, fmt.Errorf("estimated: the penalty's context %q must be one sentence with no figure", b.context)
		}
		reason += " " + b.context
	}
	p.reason, p.req, p.schedule, p.context = reason, b.req, b.schedule, b.context
	return p, nil
}

// Explanation is how a jurisdiction's estimated tax and its penalty work, as a
// tax professional would put it to a client. It is written by hand for each
// jurisdiction and checked against that jurisdiction's research file.
type Explanation struct {
	summary       string
	body          []string
	versusFederal string
	sources       []string
}

// Summary is one or two sentences.
func (e Explanation) Summary() string { return e.summary }

// Body is a few short paragraphs.
func (e Explanation) Body() []string { return append([]string(nil), e.body...) }

// VersusFederal says how the jurisdiction differs from the federal rules. It
// is empty for the federal explanation itself.
func (e Explanation) VersusFederal() string { return e.versusFederal }

// Sources cite the documents the explanation rests on.
func (e Explanation) Sources() []string { return append([]string(nil), e.sources...) }

// ExplanationBuilder builds an [Explanation].
type ExplanationBuilder struct{ e Explanation }

func NewExplanation(summary string) *ExplanationBuilder {
	return &ExplanationBuilder{e: Explanation{summary: summary}}
}

func (b *ExplanationBuilder) Paragraph(p string) *ExplanationBuilder {
	b.e.body = append(b.e.body, p)
	return b
}

func (b *ExplanationBuilder) VersusFederal(s string) *ExplanationBuilder {
	b.e.versusFederal = s
	return b
}

func (b *ExplanationBuilder) Source(cite string) *ExplanationBuilder {
	b.e.sources = append(b.e.sources, cite)
	return b
}

func (b *ExplanationBuilder) Build() (Explanation, error) {
	e := b.e
	e.body = append([]string(nil), e.body...)
	e.sources = append([]string(nil), e.sources...)
	switch {
	case e.summary == "":
		return Explanation{}, errors.New("estimated: an explanation needs a summary")
	case len(e.body) == 0:
		return Explanation{}, errors.New("estimated: an explanation needs a body")
	case len(e.sources) == 0:
		return Explanation{}, errors.New("estimated: an explanation must cite its sources")
	}
	for _, p := range e.body {
		if p == "" {
			return Explanation{}, errors.New("estimated: an explanation has an empty paragraph")
		}
	}
	return e, nil
}

// Leaves words what is left after a subtraction, for a sentence: "leaves
// $742" when something is due, and "leaves a $136 refund" when the
// subtraction goes below zero. A refund is never written with a minus sign.
func Leaves(left money.USCents) string {
	if left < 0 {
		return "leaves a " + (-left).Whole() + " refund"
	}
	return "leaves " + left.Whole()
}

// Left is [Leaves] in the past tense, for last year: "left $742" or "left a
// $136 refund".
func Left(left money.USCents) string {
	if left < 0 {
		return "left a " + (-left).Whole() + " refund"
	}
	return "left " + left.Whole()
}

// WithoutThisYearTax is the effect of asking for this year's tax: while
// another option sets the target, "Without it, the target can only come
// from last year's tax."; with none open, "Without it, there's no target."
func WithoutThisYearTax(otherOpen bool) string {
	if otherOpen {
		return "Without it, the target can only come from last year's tax."
	}
	return "Without it, there's no target."
}
