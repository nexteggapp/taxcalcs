package estimated

import (
	"errors"
	"fmt"
	"sort"

	"github.com/nexteggapp/taxcalcs/money"
)

// CumulativeShares returns the running total due by each installment when
// each takes the given share of annual.
//
// Federally the shares are 25% each; California's are 30%, 40%, 0% and 30%.
// Each running total is computed from annual rather than by adding rounded
// installments, so rounding cannot make the last total miss annual. The
// shares must add to exactly 100%.
func CumulativeShares(annual money.USCents, shares []money.Rate) ([]money.USCents, error) {
	var sum money.Rate
	for _, s := range shares {
		if s < 0 {
			return nil, fmt.Errorf("estimated: share %v is negative", s)
		}
		sum += s
	}
	if sum != money.Percent(100) {
		return nil, fmt.Errorf("estimated: shares add to %v, not 100%%", sum)
	}
	out := make([]money.USCents, len(shares))
	var running money.Rate
	for i, s := range shares {
		running += s
		out[i] = running.Of(annual)
	}
	return out, nil
}

// Increments turns running totals into the amount each step adds.
func Increments(cumulative []money.USCents) []money.USCents {
	out := make([]money.USCents, len(cumulative))
	var prev money.USCents
	for i, c := range cumulative {
		out[i] = c - prev
		prev = c
	}
	return out
}

// Annualised returns each required installment under the annualised income
// installment method.
//
// annualisedTax is the tax after credits on each period's income annualised,
// applicable is the percentage of it due by each installment, and regular is
// each installment under the regular method. This is Form 2210 Schedule AI
// lines 21 to 27, and FTB 5805 Part III lines 16 to 23, which are the same
// computation with different percentages:
//
//   - the annualised amount is the applicable percentage of the period's tax,
//     less the installments already required, and not below zero;
//   - the regular amount is the regular installment plus whatever the regular
//     method would have required earlier and the annualised method did not;
//   - the installment is the smaller of the two.
//
// The second rule is the recapture: an installment the annualised method
// lowered is added back to the next regular amount, so a filer cannot skip it
// for good.
func Annualised(annualisedTax []money.USCents, applicable []money.Rate, regular []money.USCents) ([]money.USCents, error) {
	if len(annualisedTax) != len(applicable) || len(applicable) != len(regular) {
		return nil, errors.New("estimated: annualised tax, percentages and regular installments differ in number")
	}
	out := make([]money.USCents, len(regular))
	var before, carried money.USCents
	for i := range regular {
		ann := applicable[i].Of(annualisedTax[i]) - before
		if ann < 0 {
			ann = 0
		}
		reg := regular[i] + carried
		out[i] = min(ann, reg)
		carried = reg - out[i]
		before += out[i]
	}
	return out, nil
}

// LateStart is the installment a late starter's equal shares begin at: the
// number of groups, in order, that fall on or before met. groups[i] is the
// first day a requirement first met then skips installment i, so Georgia's
// April 1, June 1 and September 1 send a requirement first met on June 1 to
// the third installment. Zero when met is the zero Date.
func LateStart(met Date, groups []Date) int {
	if met.IsZero() {
		return 0
	}
	n := 0
	for _, g := range groups {
		if !met.Before(g) {
			n++
		}
	}
	return n
}

// EqualShares returns the running total due by each of count installments
// when annual is paid in equal parts from installment first on, as several
// states have a filer who first meets the requirement late pay the year's
// amount over the dates that remain.
//
// The totals before first are zero. Each later total is annual × k ÷ the
// installments remaining, rounded half up, so the last is exactly annual and
// an odd cent falls where the rounding puts it. [CumulativeShares] cannot do
// this, because a third is not a whole number of basis points.
func EqualShares(annual money.USCents, count, first int) ([]money.USCents, error) {
	if count <= 0 || first < 0 || first >= count {
		return nil, fmt.Errorf("estimated: equal shares from installment %d of %d", first+1, count)
	}
	if annual < 0 {
		return nil, fmt.Errorf("estimated: %v cannot be shared out", annual)
	}
	n := int64(count - first)
	out := make([]money.USCents, count)
	for k := int64(1); k <= n; k++ {
		out[first+int(k)-1] = money.USCents((int64(annual)*k*2 + n) / (2 * n))
	}
	return out, nil
}

// MoveDates returns each date moved to the next business day.
func MoveDates(c Calendar, dates []Date) []Date {
	out := make([]Date, len(dates))
	for i, d := range dates {
		out[i] = c.NextBusinessDay(d)
	}
	return out
}

// Credits lists what counts as paid toward the installments, dated, with
// withholding deemed paid on the due dates: shares[i] of the year's total on
// dues[i], whenever it was actually withheld. Estimated payments count on
// their dates. Last year's overpayment applied forward counts on the first
// due date, as Form 2210, FTB 5805 and REV-1630 all treat it.
//
// It does not read [Elections.WithholdingAsWithheld]. Whether that election
// exists is a jurisdiction's rule: federally and in California it does, and
// the rules call [CreditsAsWithheld] when it is made; Pennsylvania has no such
// election; Maryland credits withholding on its dates by default.
func Credits(f Facts, dues []Date, shares []money.Rate) ([]Payment, error) {
	if len(dues) != len(shares) || len(dues) == 0 {
		return nil, errors.New("estimated: one withholding share per due date")
	}
	cum, err := CumulativeShares(f.WithheldTotal(), shares)
	if err != nil {
		return nil, err
	}
	var out []Payment
	for i, a := range Increments(cum) {
		if a > 0 {
			out = append(out, Payment{on: dues[i], amount: a})
		}
	}
	return withPayments(out, f, dues), nil
}

// CreditsAsWithheld is [Credits] with withholding counted on the dates it was
// withheld.
func CreditsAsWithheld(f Facts, dues []Date) ([]Payment, error) {
	if len(dues) == 0 {
		return nil, errors.New("estimated: no due dates")
	}
	var out []Payment
	for _, w := range f.withholding {
		out = append(out, Payment{on: w.on, amount: w.amount})
	}
	return withPayments(out, f, dues), nil
}

func withPayments(out []Payment, f Facts, dues []Date) []Payment {
	for _, p := range f.payments {
		if p.priorYear {
			p.on = dues[0]
		}
		out = append(out, p)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].on.Before(out[j].on) })
	return out
}

// Piece is part of an installment's shortfall, and the day it was paid.
type Piece struct {
	amount money.USCents
	paidOn Date
}

func (p Piece) Amount() money.USCents { return p.amount }

// PaidOn is the day the piece was paid, and false if it never was.
func (p Piece) PaidOn() (Date, bool) { return p.paidOn, !p.paidOn.IsZero() }

// LedgerEntry is one installment after the credits have been applied to it.
type LedgerEntry struct {
	number     int
	due        Date
	required   money.USCents
	neededBy   money.USCents
	creditedBy money.USCents
	shortfall  money.USCents
	pieces     []Piece
}

func (a LedgerEntry) Number() int               { return a.number }
func (a LedgerEntry) Due() Date                 { return a.due }
func (a LedgerEntry) Required() money.USCents   { return a.required }
func (a LedgerEntry) NeededBy() money.USCents   { return a.neededBy }
func (a LedgerEntry) CreditedBy() money.USCents { return a.creditedBy }

// Shortfall is the installment's underpayment on its due date.
func (a LedgerEntry) Shortfall() money.USCents { return a.shortfall }

// Pieces are the shortfall broken up by the credits that later paid it, in
// order. A piece with no date was never paid.
func (a LedgerEntry) Pieces() []Piece { return append([]Piece(nil), a.pieces...) }

// Apply applies credits to the required installments in the order Form 2210
// and FTB 5805 do.
//
// A credit pays the oldest unpaid shortfall first, whichever installment it
// was meant for. What is left over is held toward the next installment.
// Credits dated on a due date count toward that installment, after paying
// anything older. This is Form 2210 Part III Section A, lines 10 to 18, and
// the payment application rule in the instructions for Section B: a credit
// that pays part of a shortfall splits it into a piece paid that day and a
// piece still outstanding.
//
// Installment i of the result is for dues[i]. Credits after the last due date
// pay off what is outstanding; any excess is dropped.
func Apply(dues []Date, required []money.USCents, credits []Payment) ([]LedgerEntry, error) {
	if len(dues) != len(required) || len(dues) == 0 {
		return nil, errors.New("estimated: one required amount per due date")
	}
	for i := 1; i < len(dues); i++ {
		if !dues[i].After(dues[i-1]) {
			return nil, errors.New("estimated: due dates must be in order")
		}
	}
	cs := append([]Payment(nil), credits...)
	for _, c := range cs {
		if c.on.IsZero() || c.amount < 0 {
			return nil, errors.New("estimated: every credit needs a date and an amount that is not negative")
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].on.Before(cs[j].on) })

	out := make([]LedgerEntry, len(dues))
	var pool money.USCents // credited and not yet needed
	var credited, needed money.USCents
	outstanding := func(i int) money.USCents {
		var paid money.USCents
		for _, p := range out[i].pieces {
			if !p.paidOn.IsZero() {
				paid += p.amount
			}
		}
		return out[i].shortfall - paid
	}
	// pay applies a credit to the oldest outstanding shortfalls among the
	// first n installments and returns what is left of it.
	pay := func(n int, c Payment) money.USCents {
		left := c.amount
		for i := 0; i < n && left > 0; i++ {
			owed := outstanding(i)
			if owed <= 0 {
				continue
			}
			paid := min(owed, left)
			out[i].pieces = append(out[i].pieces, Piece{amount: paid, paidOn: c.on})
			left -= paid
		}
		return left
	}

	next := 0
	for i, due := range dues {
		for next < len(cs) && !cs[next].on.After(due) {
			credited += cs[next].amount
			pool += pay(i, cs[next])
			next++
		}
		needed += required[i]
		covered := min(pool, required[i])
		pool -= covered
		out[i] = LedgerEntry{
			number:     i + 1,
			due:        due,
			required:   required[i],
			neededBy:   needed,
			creditedBy: credited,
			shortfall:  required[i] - covered,
		}
	}
	for ; next < len(cs); next++ {
		pay(len(dues), cs[next])
	}
	for i := range out {
		if owed := outstanding(i); owed > 0 {
			out[i].pieces = append(out[i].pieces, Piece{amount: owed})
		}
	}
	return out, nil
}

// SimplePenalty is the penalty as simple daily interest on each piece of each
// shortfall, from the installment's due date to the day the piece was paid or
// to stop, whichever is earlier.
//
// unrated is the first day the series has no rate for, when any piece runs
// into it, and the zero Date otherwise. Those days are not charged, and the
// caller must report it.
//
// r and schedule are what the penalty was figured from, for its reason.
func SimplePenalty(ledger []LedgerEntry, stop Date, s RateSeries, waiver WaiverKind, r Requirement, schedule []Installment) (Penalty, Date, error) {
	pb := NewPenalty(stop).Waiver(waiver).Because(r, schedule)
	var unrated Date
	for _, a := range ledger {
		if a.shortfall <= 0 {
			continue
		}
		lb := NewPenaltyLine(a.number, a.due, a.shortfall)
		last := a.due
		for _, p := range a.pieces {
			to := stop
			if !p.paidOn.IsZero() {
				to = earlier(p.paidOn, stop)
			}
			if !to.After(a.due) {
				continue
			}
			last = later(last, to)
			parts, gap, err := SimpleDaily(p.amount, a.due, to, s)
			if err != nil {
				return Penalty{}, Date{}, err
			}
			if !gap.IsZero() && (unrated.IsZero() || gap.Before(unrated)) {
				unrated = gap
			}
			for _, part := range parts {
				lb.Part(part)
			}
		}
		line, err := lb.Days(last.DaysSince(a.due)).Build()
		if err != nil {
			return Penalty{}, Date{}, err
		}
		pb.Line(line)
	}
	if !unrated.IsZero() {
		pb.RatedThrough(unrated.AddDays(-1))
	}
	p, err := pb.Build()
	return p, unrated, err
}

// Column is one installment's column on a penalty form that charges what is
// short on each due date until it is paid or the next due date comes, as
// Nebraska's Form 2210N, Missouri's MO-2210 and Mississippi's Form 80-320 do.
// What is still short at the next due date is that column's to charge.
type Column struct {
	number   int
	due, end Date
	credited money.USCents
	under    money.USCents
	pieces   []Piece
}

// Number counts from 1.
func (c Column) Number() int { return c.number }
func (c Column) Due() Date   { return c.due }

// End is the next due date, or the last day charged for the final column,
// whichever is earlier: the day a piece still unpaid is charged to.
func (c Column) End() Date { return c.end }

// CreditedBy is everything credited on or before the due date.
func (c Column) CreditedBy() money.USCents { return c.credited }

// Under is what the running total required by the due date exceeds what was
// credited by it. Zero or less is nothing short.
func (c Column) Under() money.USCents { return c.under }

// Pieces are what was under, split by the credits after the due date and
// before [Column.End] that paid it, in order. A piece with no date was still
// unpaid at End.
func (c Column) Pieces() []Piece { return append([]Piece(nil), c.pieces...) }

// Through is the last day charged: the day the last piece was paid, End if
// a piece was never paid, or the due date when nothing was under.
func (c Column) Through() Date {
	last := c.due
	for _, p := range c.pieces {
		if p.paidOn.IsZero() {
			return c.end
		}
		last = p.paidOn
	}
	return last
}

// Columns splits each installment's running underpayment into the pieces a
// column-by-column penalty form charges.
//
// cumulative[i] is the total required by dues[i]. A credit counts toward a
// column when it is dated on or before the due date. Each credit after the
// due date and before the column's end pays off up to what is left, in date
// order; what remains runs to the end. A column ends at the next due date,
// the final one at last, and no column ends after stop. Columns whose due date
// is not before stop are left out.
//
// Unlike [Apply], a credit is not used up by the column it pays: every
// column measures the credits on its own.
func Columns(dues []Date, cumulative []money.USCents, credits []Payment, last, stop Date) ([]Column, error) {
	if len(dues) != len(cumulative) || len(dues) == 0 {
		return nil, errors.New("estimated: one running total per due date")
	}
	for i := 1; i < len(dues); i++ {
		if !dues[i].After(dues[i-1]) {
			return nil, errors.New("estimated: due dates must be in order")
		}
	}
	cs := append([]Payment(nil), credits...)
	for _, c := range cs {
		if c.on.IsZero() || c.amount < 0 {
			return nil, errors.New("estimated: every credit needs a date and an amount that is not negative")
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].on.Before(cs[j].on) })

	var out []Column
	for k, due := range dues {
		if !due.Before(stop) {
			break
		}
		end := last
		if k+1 < len(dues) {
			end = dues[k+1]
		}
		end = earlier(end, stop)
		col := Column{number: k + 1, due: due, end: end}
		for _, c := range cs {
			if !c.on.After(due) {
				col.credited += c.amount
			}
		}
		col.under = cumulative[k] - col.credited
		left := col.under
		for _, c := range cs {
			if left <= 0 || !c.on.After(due) || !c.on.Before(end) {
				continue
			}
			paid := min(left, c.amount)
			col.pieces = append(col.pieces, Piece{amount: paid, paidOn: c.on})
			left -= paid
		}
		if left > 0 {
			col.pieces = append(col.pieces, Piece{amount: left})
		}
		out = append(out, col)
	}
	return out, nil
}
