package eval

import (
	"math"

	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
)

// The date builtins count days in the proleptic Gregorian calendar, as Go's
// time package does, by Howard Hinnant's days-from-civil algorithm. Day 0 is
// 1970-01-01. The Rust engine uses the same algorithm, so the two agree on
// every date a DateVal can hold, years included that the time package would
// overflow on.

// minDay and maxDay are the first and last days a DateVal's int32 year can
// hold.
var (
	minDay = daysFromCivil(math.MinInt32, 1, 1)
	maxDay = daysFromCivil(math.MaxInt32, 12, 31)
)

// daysFromCivil returns the day number of year-month-day, which must be a
// date.
func daysFromCivil(y, m, d int64) int64 {
	if m <= 2 {
		y--
	}
	era := y
	if era < 0 {
		era -= 399
	}
	era /= 400
	yoe := y - era*400
	mp := m - 3
	if m <= 2 {
		mp = m + 9
	}
	doy := (153*mp+2)/5 + d - 1
	doe := yoe*365 + yoe/4 - yoe/100 + doy
	return era*146097 + doe - 719468
}

// civilFromDays returns the date of day number z, which must be between
// minDay and maxDay.
func civilFromDays(z int64) DateVal {
	z += 719468
	era := z
	if era < 0 {
		era -= 146096
	}
	era /= 146097
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if mp >= 10 {
		m = mp - 9
	}
	if m <= 2 {
		y++
	}
	return DateVal{Year: int32(y), Month: int32(m), Day: int32(d)}
}

// isLeap reports whether year has a February 29th.
func isLeap(y int64) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

// daysInMonth is the number of days in month m, from 1 to 12, of year y.
func daysInMonth(y, m int64) int64 {
	switch m {
	case 2:
		if isLeap(y) {
			return 29
		}
		return 28
	case 4, 6, 9, 11:
		return 30
	default:
		return 31
	}
}

// isDate reports whether year-month-day is a day a DateVal can hold:
// February 30th is not, and neither is a year outside int32.
func isDate(y, m, d int64) bool {
	return y >= math.MinInt32 && y <= math.MaxInt32 &&
		m >= 1 && m <= 12 && d >= 1 && d <= daysInMonth(y, m)
}

// notADate is the error for a day that does not exist, as estimated.NewDate
// reports one.
func notADate(expr *taxlispv1.Expression, name string, y, m, d int64) error {
	return errAt(expr, "%s: %04d-%02d-%02d is not a date", name, y, m, d)
}

// dayOf returns the day number of the Date argument at i of builtin name.
func dayOf(name string, args []Value, i int, expr *taxlispv1.Expression) (int64, error) {
	d, ok := args[i].(DateVal)
	if !ok {
		return 0, errAt(expr, "%s expects Date, got %T", name, args[i])
	}
	y, m, day := int64(d.Year), int64(d.Month), int64(d.Day)
	if !isDate(y, m, day) {
		return 0, notADate(expr, name, y, m, day)
	}
	return daysFromCivil(y, m, day), nil
}

// intArg returns the Int argument at i of builtin name.
func intArg(name string, args []Value, i int, expr *taxlispv1.Expression) (int64, error) {
	v, ok := args[i].(IntVal)
	if !ok {
		return 0, errAt(expr, "%s expects Int, got %T", name, args[i])
	}
	return v.V, nil
}

// dateOfDay returns the date of day number n, or an error when no DateVal
// can hold it.
func dateOfDay(name string, n int64, expr *taxlispv1.Expression) (Value, error) {
	if n < minDay || n > maxDay {
		return nil, errAt(expr, "%s: the result is out of range", name)
	}
	return civilFromDays(n), nil
}

// checkedAdd is a + b, or false when it overflows int64.
func checkedAdd(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}

// weekend reports whether day number n is a Saturday or a Sunday.
func weekend(n int64) bool {
	w := weekday(n)
	return w == 0 || w == 6
}

// weekday is 0 for Sunday through 6 for Saturday, as time.Weekday counts.
// Day 0, 1970-01-01, was a Thursday.
func weekday(n int64) int64 {
	w := (n + 4) % 7
	if w < 0 {
		w += 7
	}
	return w
}

// dateBuiltins are the builtins that do date arithmetic for estimated tax:
// what estimated.Date and estimated.Calendar do for due dates and penalty
// periods.
var dateBuiltins = []BuiltinVal{
	{
		Name: "make-date",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			var ymd [3]int64
			for i := range ymd {
				v, err := intArg("make-date", args, i, expr)
				if err != nil {
					return nil, err
				}
				ymd[i] = v
			}
			if !isDate(ymd[0], ymd[1], ymd[2]) {
				return nil, notADate(expr, "make-date", ymd[0], ymd[1], ymd[2])
			}
			return DateVal{Year: int32(ymd[0]), Month: int32(ymd[1]), Day: int32(ymd[2])}, nil
		},
	},
	{
		Name: "date-add-days",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			day, err := dayOf("date-add-days", args, 0, expr)
			if err != nil {
				return nil, err
			}
			n, err := intArg("date-add-days", args, 1, expr)
			if err != nil {
				return nil, err
			}
			sum, ok := checkedAdd(day, n)
			if !ok {
				return nil, errAt(expr, "date-add-days: the result is out of range")
			}
			return dateOfDay("date-add-days", sum, expr)
		},
	},
	{
		Name: "date-add-months",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			if _, err := dayOf("date-add-months", args, 0, expr); err != nil {
				return nil, err
			}
			d := args[0].(DateVal)
			n, err := intArg("date-add-months", args, 1, expr)
			if err != nil {
				return nil, err
			}
			months, ok := checkedAdd(int64(d.Year)*12+int64(d.Month)-1, n)
			if !ok {
				return nil, errAt(expr, "date-add-months: the result is out of range")
			}
			y, m := months/12, months%12
			if m < 0 {
				y, m = y-1, m+12
			}
			if y < math.MinInt32 || y > math.MaxInt32 {
				return nil, errAt(expr, "date-add-months: the result is out of range")
			}
			day := min(int64(d.Day), daysInMonth(y, m+1))
			return DateVal{Year: int32(y), Month: int32(m + 1), Day: int32(day)}, nil
		},
	},
	{
		Name: "days-between",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			from, err := dayOf("days-between", args, 0, expr)
			if err != nil {
				return nil, err
			}
			to, err := dayOf("days-between", args, 1, expr)
			if err != nil {
				return nil, err
			}
			return IntVal{V: to - from}, nil
		},
	},
	{
		Name: "date-weekday",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			day, err := dayOf("date-weekday", args, 0, expr)
			if err != nil {
				return nil, err
			}
			return IntVal{V: weekday(day)}, nil
		},
	},
	{
		Name: "month-end",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			y, err := intArg("month-end", args, 0, expr)
			if err != nil {
				return nil, err
			}
			m, err := intArg("month-end", args, 1, expr)
			if err != nil {
				return nil, err
			}
			if !isDate(y, m, 1) {
				return nil, errAt(expr, "month-end: %04d-%02d is not a month", y, m)
			}
			return DateVal{Year: int32(y), Month: int32(m), Day: int32(daysInMonth(y, m))}, nil
		},
	},
	{
		Name: "days-in-year",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			y, err := intArg("days-in-year", args, 0, expr)
			if err != nil {
				return nil, err
			}
			if isLeap(y) {
				return IntVal{V: 366}, nil
			}
			return IntVal{V: 365}, nil
		},
	},
	{
		Name: "next-business-day",
		Fn: func(_ *Evaluator, args []Value, expr *taxlispv1.Expression) (Value, error) {
			day, err := dayOf("next-business-day", args, 0, expr)
			if err != nil {
				return nil, err
			}
			s, ok := args[1].(SliceVal)
			if !ok {
				return nil, errAt(expr, "next-business-day expects Slice as second argument, got %T", args[1])
			}
			holidays := make(map[int64]bool, len(s.Elems))
			for i := range s.Elems {
				h, err := dayOf("next-business-day", s.Elems, i, expr)
				if err != nil {
					return nil, err
				}
				holidays[h] = true
			}
			for weekend(day) || holidays[day] {
				day++
			}
			return dateOfDay("next-business-day", day, expr)
		},
	},
}

// foldDates is min or max over Date operands.
func foldDates(expr *taxlispv1.Expression, op string, first DateVal, rest []Value) (Value, error) {
	acc := first
	for _, v := range rest {
		d, ok := v.(DateVal)
		if !ok {
			return nil, errAt(expr, "mismatched operand types for %s", op)
		}
		c := compareDate(d, acc)
		if (op == "min" && c < 0) || (op == "max" && c > 0) {
			acc = d
		}
	}
	return acc, nil
}
