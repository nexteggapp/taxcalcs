package payroll

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
)

// ErrNoYear reports that the federal rules carry no withholding figures for
// a tax year. Payroll refuses the year rather than apply another year's
// figures to it.
var ErrNoYear = errors.New("payroll: no withholding figures for this tax year")

// figures is what payroll reads from the federal taxlisp rules for one tax
// year: the § 1(j) brackets and § 63(c) standard deduction of the three
// statuses Publication 15-T has tables for, the § 3121(a)(1) wage base, and
// the § 402(g) limit. The § 414(v) catch-up is asked of the rules by age.
type figures struct {
	year          int
	ev            *eval.Evaluator
	brackets      map[filing.Status][]bracket
	standard      map[filing.Status]money.USCents
	wageBase      money.USCents
	deferralLimit money.USCents
	catchUps      sync.Map // int age -> money.USCents
}

// bracket is one row of a rate schedule: the rate on income over floor.
type bracket struct {
	floor money.USCents
	rate  money.Rate
}

// tax is the rate schedule's tax on taxable income, each band's tax rounded
// to the cent. Less than nothing is nothing.
func (f *figures) tax(status filing.Status, taxable money.USCents) money.USCents {
	taxable = max(0, taxable)
	rows := f.brackets[status]
	var total money.USCents
	for i, b := range rows {
		top := taxable
		if i+1 < len(rows) {
			top = min(top, rows[i+1].floor)
		}
		if top > b.floor {
			total += b.rate.Of(top - b.floor)
		}
	}
	return total
}

// catchUp is the rules' plan-catch-up for a person of age at the end of the
// year.
func (f *figures) catchUp(age int) (money.USCents, error) {
	if v, ok := f.catchUps.Load(age); ok {
		return v.(money.USCents), nil
	}
	v, err := f.ev.Call("plan-catch-up", eval.IntVal{V: int64(f.year)}, eval.IntVal{V: int64(age)})
	if err != nil {
		return 0, fmt.Errorf("payroll: plan-catch-up: %w", err)
	}
	m, ok := v.(eval.MoneyVal)
	if !ok {
		return 0, fmt.Errorf("payroll: plan-catch-up gave %T", v)
	}
	c := money.USCents(m.Cents())
	f.catchUps.Store(age, c)
	return c, nil
}

var (
	loadOnce sync.Once
	byYear   map[int]*figures
	loadErr  error
)

// figuresFor is the year's figures, or [ErrNoYear].
func figuresFor(year int) (*figures, error) {
	loadOnce.Do(func() { byYear, loadErr = load() })
	if loadErr != nil {
		return nil, loadErr
	}
	f, ok := byYear[year]
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrNoYear, year)
	}
	return f, nil
}

// load reads every year in the rules' payroll-tax-years.
func load() (map[int]*figures, error) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		return nil, fmt.Errorf("payroll: loading the federal rules: %w", err)
	}
	ev := eng.Evaluator
	v, ok := ev.Globals.Get("payroll-tax-years")
	if !ok {
		return nil, errors.New("payroll: the federal rules have no payroll-tax-years")
	}
	years, ok := v.(eval.SliceVal)
	if !ok {
		return nil, fmt.Errorf("payroll: payroll-tax-years is %T", v)
	}
	out := map[int]*figures{}
	for _, y := range years.Elems {
		iv, ok := y.(eval.IntVal)
		if !ok {
			return nil, fmt.Errorf("payroll: payroll-tax-years holds %T", y)
		}
		f, err := loadYear(ev, int(iv.V))
		if err != nil {
			return nil, err
		}
		out[f.year] = f
	}
	return out, nil
}

// statuses are the rules' names for the statuses Publication 15-T has tables
// for.
var statuses = map[string]filing.Status{
	"FILING_STATUS_SINGLE":                 filing.Single,
	"FILING_STATUS_MARRIED_FILING_JOINTLY": filing.MarriedFilingJointly,
	"FILING_STATUS_HEAD_OF_HOUSEHOLD":      filing.HeadOfHousehold,
}

func loadYear(ev *eval.Evaluator, year int) (*figures, error) {
	y := eval.IntVal{V: int64(year)}
	f := &figures{year: year, ev: ev, brackets: map[filing.Status][]bracket{}, standard: map[filing.Status]money.USCents{}}

	params, err := ev.Call("year-params", y)
	if err != nil {
		return nil, fmt.Errorf("payroll: year-params %d: %w", year, err)
	}
	p, ok := params.(eval.StructVal)
	if !ok {
		return nil, fmt.Errorf("payroll: year-params %d gave %T", year, params)
	}
	amount := func(field string) (money.USCents, error) {
		m, ok := p.Fields[field].(eval.MoneyVal)
		if !ok {
			return 0, fmt.Errorf("payroll: year-params %d has no %s", year, field)
		}
		return money.USCents(m.Cents()), nil
	}
	for field, status := range map[string]filing.Status{
		"std_ded_single": filing.Single,
		"std_ded_mfj":    filing.MarriedFilingJointly,
		"std_ded_hoh":    filing.HeadOfHousehold,
	} {
		if f.standard[status], err = amount(field); err != nil {
			return nil, err
		}
	}
	if f.wageBase, err = amount("oasdi_wage_base"); err != nil {
		return nil, err
	}

	table, err := ev.Call("bracket-table-for-year", y)
	if err != nil {
		return nil, fmt.Errorf("payroll: bracket-table-for-year %d: %w", year, err)
	}
	t, ok := table.(eval.TableVal)
	if !ok {
		return nil, fmt.Errorf("payroll: bracket-table-for-year %d gave %T", year, table)
	}
	for _, e := range t.Entries {
		var name string
		switch k := e.Key.(type) {
		case eval.SymbolVal:
			name = k.Name
		case eval.ProtoEnumVal:
			name = k.Name
		default:
			return nil, fmt.Errorf("payroll: a %d bracket table key is %T", year, e.Key)
		}
		status, ok := statuses[name]
		if !ok {
			continue
		}
		rows, ok := e.Value.(eval.SliceVal)
		if !ok {
			return nil, fmt.Errorf("payroll: the %d %s brackets are %T", year, name, e.Value)
		}
		for _, r := range rows.Elems {
			s, ok := r.(eval.StructVal)
			if !ok {
				return nil, fmt.Errorf("payroll: a %d %s bracket is %T", year, name, r)
			}
			floor, ok1 := s.Fields["floor"].(eval.MoneyVal)
			rate, ok2 := s.Fields["rate"].(eval.FloatVal)
			if !ok1 || !ok2 {
				return nil, fmt.Errorf("payroll: a %d %s bracket has no floor or rate", year, name)
			}
			f.brackets[status] = append(f.brackets[status], bracket{
				floor: money.USCents(floor.Cents()),
				rate:  money.Rate(math.Round(rate.V * 10_000)),
			})
		}
	}
	for _, s := range statuses {
		if len(f.brackets[s]) == 0 {
			return nil, fmt.Errorf("payroll: the rules have no %d brackets for %s", year, s)
		}
	}

	limit, err := ev.Call("plan-elective-deferral-limit", y)
	if err != nil {
		return nil, fmt.Errorf("payroll: plan-elective-deferral-limit %d: %w", year, err)
	}
	m, ok := limit.(eval.MoneyVal)
	if !ok {
		return nil, fmt.Errorf("payroll: plan-elective-deferral-limit %d gave %T", year, limit)
	}
	f.deferralLimit = money.USCents(m.Cents())
	return f, nil
}
