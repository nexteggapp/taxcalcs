package californiataxcalc

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/nexteggapp/taxcalcs/money"
)

// Worksheet I of the FTB 5805 instructions, "Prorated Tax for Form 540NR
// Filers", prorates a nonresident's or part-year resident's annualised tax
// to California. It is part of the annualised income installment method:
// FTB 5805 Part III line 12 for a Form 540NR filer is Worksheet I line P, and
// the credits on Part III line 13 are prorated by its line N. The regular
// method has no proration; Part II takes the tax from Form 540NR as filed.
//
// The instructions don't say how many decimal places lines C, H and N carry,
// or how the amounts round. The ratios here are exact and each amount is
// rounded to the cent, half up, and the rules report that as unsettled.

// worksheetIAnnualisation is line E, the same as Part III line 2: 4, 2.4, 1.5
// and 1.
var worksheetIAnnualisation = []*big.Rat{big.NewRat(4, 1), big.NewRat(12, 5), big.NewRat(3, 2), big.NewRat(1, 1)}

// WorksheetIColumn is one period's figures for Worksheet I and the Part III
// lines that follow it, each as the Form 540NR filer works it out for the
// period.
type WorksheetIColumn struct {
	taxOnTotal         money.USCents // line A: Part III line 10
	totalTaxable       money.USCents // line B: Part III line 9
	californiaAGI      money.USCents // line D: Schedule CA (540NR) Part IV line 1 for the period
	annualisedTotalAGI money.USCents // line G: Part III line 3
	deduction          money.USCents // line I: Part III line 8
	exemptionCredits   money.USCents // line M: Part III line 11
	proratedCredits    money.USCents // Form 540NR lines 51 to 53, prorated by line N
	otherCredits       money.USCents // the rest of Part III line 13, as refigured
	otherTax           money.USCents // Part III line 14b
}

func (c WorksheetIColumn) TaxOnTotal() money.USCents         { return c.taxOnTotal }
func (c WorksheetIColumn) TotalTaxable() money.USCents       { return c.totalTaxable }
func (c WorksheetIColumn) CaliforniaAGI() money.USCents      { return c.californiaAGI }
func (c WorksheetIColumn) AnnualisedTotalAGI() money.USCents { return c.annualisedTotalAGI }
func (c WorksheetIColumn) Deduction() money.USCents          { return c.deduction }
func (c WorksheetIColumn) ExemptionCredits() money.USCents   { return c.exemptionCredits }
func (c WorksheetIColumn) ProratedCredits() money.USCents    { return c.proratedCredits }
func (c WorksheetIColumn) OtherCredits() money.USCents       { return c.otherCredits }
func (c WorksheetIColumn) OtherTax() money.USCents           { return c.otherTax }

// WorksheetIColumnBuilder builds a [WorksheetIColumn].
type WorksheetIColumnBuilder struct{ c WorksheetIColumn }

func NewWorksheetIColumn() *WorksheetIColumnBuilder { return &WorksheetIColumnBuilder{} }

// TaxOnTotal is line A: the tax on the annualised total taxable income, Part
// III line 10.
func (b *WorksheetIColumnBuilder) TaxOnTotal(a money.USCents) *WorksheetIColumnBuilder {
	b.c.taxOnTotal = a
	return b
}

// TotalTaxable is line B: the annualised total taxable income, Part III line
// 9.
func (b *WorksheetIColumnBuilder) TotalTaxable(a money.USCents) *WorksheetIColumnBuilder {
	b.c.totalTaxable = a
	return b
}

// CaliforniaAGI is line D: California AGI for the period, from Schedule CA
// (540NR) Part IV line 1, before annualising.
func (b *WorksheetIColumnBuilder) CaliforniaAGI(a money.USCents) *WorksheetIColumnBuilder {
	b.c.californiaAGI = a
	return b
}

// AnnualisedTotalAGI is line G: the annualised total AGI, Part III line 3,
// from all sources.
func (b *WorksheetIColumnBuilder) AnnualisedTotalAGI(a money.USCents) *WorksheetIColumnBuilder {
	b.c.annualisedTotalAGI = a
	return b
}

// Deduction is line I: Part III line 8, the larger of the annualised
// itemised deductions and the standard deduction.
func (b *WorksheetIColumnBuilder) Deduction(a money.USCents) *WorksheetIColumnBuilder {
	b.c.deduction = a
	return b
}

// ExemptionCredits is line M: Part III line 11.
func (b *WorksheetIColumnBuilder) ExemptionCredits(a money.USCents) *WorksheetIColumnBuilder {
	b.c.exemptionCredits = a
	return b
}

// ProratedCredits are the credits on Form 540NR lines 51 to 53, which Part
// III line 13 prorates by line N.
func (b *WorksheetIColumnBuilder) ProratedCredits(a money.USCents) *WorksheetIColumnBuilder {
	b.c.proratedCredits = a
	return b
}

// OtherCredits is the rest of Part III line 13, Form 540NR lines 54 to 62 as
// refigured for the period.
func (b *WorksheetIColumnBuilder) OtherCredits(a money.USCents) *WorksheetIColumnBuilder {
	b.c.otherCredits = a
	return b
}

// OtherTax is Part III line 14b: the alternative minimum tax and behavioural
// health services tax for the period.
func (b *WorksheetIColumnBuilder) OtherTax(a money.USCents) *WorksheetIColumnBuilder {
	b.c.otherTax = a
	return b
}

// Build refuses a negative figure, and a total taxable income or total AGI
// of zero, which lines C, H and N divide by.
func (b *WorksheetIColumnBuilder) Build() (WorksheetIColumn, error) {
	c := b.c
	for _, a := range []money.USCents{c.taxOnTotal, c.totalTaxable, c.californiaAGI, c.annualisedTotalAGI, c.deduction, c.exemptionCredits, c.proratedCredits, c.otherCredits, c.otherTax} {
		if a < 0 {
			return WorksheetIColumn{}, errors.New("californiataxcalc: a Worksheet I figure cannot be negative")
		}
	}
	if c.totalTaxable == 0 || c.annualisedTotalAGI == 0 {
		return WorksheetIColumn{}, errors.New("californiataxcalc: Worksheet I divides by the total taxable income and the total AGI, so neither can be zero")
	}
	return c, nil
}

// WorksheetILines are one column's lines C to P, and Part III lines 13 and
// 14c that follow from them.
type WorksheetILines struct {
	rateC, rateH, rateN *big.Rat
	f, j, k, l, o, p    money.USCents
	line13, line14c     money.USCents
}

// RateC, RateH and RateN are lines C, H and N, exactly.
func (w WorksheetILines) RateC() *big.Rat { return new(big.Rat).Set(w.rateC) }
func (w WorksheetILines) RateH() *big.Rat { return new(big.Rat).Set(w.rateH) }
func (w WorksheetILines) RateN() *big.Rat { return new(big.Rat).Set(w.rateN) }

// F is the annualised California AGI.
func (w WorksheetILines) F() money.USCents { return w.f }

// J is the prorated deduction.
func (w WorksheetILines) J() money.USCents { return w.j }

// K is the annualised California taxable income.
func (w WorksheetILines) K() money.USCents { return w.k }

// L is the annualised California prorated tax before exemption credits.
func (w WorksheetILines) L() money.USCents { return w.l }

// O is the annualised prorated exemption credits.
func (w WorksheetILines) O() money.USCents { return w.o }

// P is the annualised California tax after exemption credits: Part III
// line 12.
func (w WorksheetILines) P() money.USCents { return w.p }

// Line13 is Part III line 13: the prorated credits and the other credits.
func (w WorksheetILines) Line13() money.USCents { return w.line13 }

// Line14c is Part III line 14c: line 12 less line 13, not below zero, plus
// line 14b. It is the annualised tax the applicable percentage is taken of.
func (w WorksheetILines) Line14c() money.USCents { return w.line14c }

// cents rounds an exact amount of cents half up.
func cents(x *big.Rat) money.USCents {
	n, d := new(big.Int).Set(x.Num()), new(big.Int).Set(x.Denom())
	twice := new(big.Int).Mul(n, big.NewInt(2))
	q := new(big.Int).Add(twice, d)
	q.Div(q, new(big.Int).Mul(d, big.NewInt(2)))
	return money.USCents(q.Int64())
}

func rat(a money.USCents) *big.Rat { return new(big.Rat).SetInt64(int64(a)) }

// WorksheetI works lines C to P of the column for the period numbered i,
// from 0, and Part III lines 13 and 14c after them.
func WorksheetI(i int, c WorksheetIColumn) (WorksheetILines, error) {
	if i < 0 || i >= len(worksheetIAnnualisation) {
		return WorksheetILines{}, fmt.Errorf("californiataxcalc: Worksheet I has four columns, not column %d", i+1)
	}
	var w WorksheetILines
	// C. Annualized California tax rate: line A ÷ line B.
	w.rateC = new(big.Rat).Quo(rat(c.taxOnTotal), rat(c.totalTaxable))
	// F. Annualized California AGI: line D × line E.
	w.f = cents(new(big.Rat).Mul(rat(c.californiaAGI), worksheetIAnnualisation[i]))
	// H. Line F ÷ line G, not more than 1.0000.
	w.rateH = new(big.Rat).Quo(rat(w.f), rat(c.annualisedTotalAGI))
	if w.rateH.Cmp(big.NewRat(1, 1)) > 0 {
		w.rateH = big.NewRat(1, 1)
	}
	// J. Line I × line H.
	w.j = cents(new(big.Rat).Mul(rat(c.deduction), w.rateH))
	// K. Line F − line J.
	w.k = w.f - w.j
	// L. Line K × line C.
	w.l = cents(new(big.Rat).Mul(rat(w.k), w.rateC))
	// N. Line K ÷ line B.
	w.rateN = new(big.Rat).Quo(rat(w.k), rat(c.totalTaxable))
	// O. Line M × line N.
	w.o = cents(new(big.Rat).Mul(rat(c.exemptionCredits), w.rateN))
	// P. Line L − line O: Part III line 12.
	w.p = w.l - w.o
	// Part III line 13: lines 51 to 53 prorated by line N, and the rest.
	w.line13 = cents(new(big.Rat).Mul(rat(c.proratedCredits), w.rateN)) + c.otherCredits
	// Part III lines 14a to 14c.
	w.line14c = max(w.p-w.line13, 0) + c.otherTax
	return w, nil
}
