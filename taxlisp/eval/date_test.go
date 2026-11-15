package eval_test

import (
	"strings"
	"testing"
	"time"

	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	"github.com/nexteggapp/taxcalcs/taxlisp/types"
)

// dateCase is an expression and the value it prints as, or the end of the
// error it fails with. An unchecked case skips the typechecker, so that the
// evaluator's own error for an ill-typed call is what is tested. The Rust
// engine's tests (rust/taxlisp/tests/date_test.rs) run the same cases.
type dateCase struct {
	src       string
	want      string
	err       string
	unchecked bool
}

var dateCases = []dateCase{
	// make-date, and the days that do not exist.
	{src: `(make-date 2026 4 15)`, want: `#date"2026-04-15"`},
	{src: `(make-date 2024 2 29)`, want: `#date"2024-02-29"`},
	{src: `(make-date 2000 2 29)`, want: `#date"2000-02-29"`},
	{src: `(make-date -1 1 1)`, want: `#date"-001-01-01"`},
	{src: `(make-date 2026 2 29)`, err: `make-date: 2026-02-29 is not a date`},
	{src: `(make-date 1900 2 29)`, err: `make-date: 1900-02-29 is not a date`},
	{src: `(make-date 2026 2 30)`, err: `make-date: 2026-02-30 is not a date`},
	{src: `(make-date 2026 4 31)`, err: `make-date: 2026-04-31 is not a date`},
	{src: `(make-date 2026 13 1)`, err: `make-date: 2026-13-01 is not a date`},
	{src: `(make-date 2026 0 1)`, err: `make-date: 2026-00-01 is not a date`},
	{src: `(make-date 2026 4 0)`, err: `make-date: 2026-04-00 is not a date`},
	{src: `(make-date 2147483648 1 1)`, err: `make-date: 2147483648-01-01 is not a date`},
	{src: `(make-date "2026" 1 1)`, err: `make-date expects Int, got eval.StringVal`, unchecked: true},

	// date-add-days across month, year and leap day.
	{src: `(date-add-days #date"2026-01-31" 1)`, want: `#date"2026-02-01"`},
	{src: `(date-add-days #date"2024-02-28" 1)`, want: `#date"2024-02-29"`},
	{src: `(date-add-days #date"2026-02-28" 1)`, want: `#date"2026-03-01"`},
	{src: `(date-add-days #date"2026-12-31" 1)`, want: `#date"2027-01-01"`},
	{src: `(date-add-days #date"2026-03-01" -1)`, want: `#date"2026-02-28"`},
	{src: `(date-add-days #date"2026-04-15" 56)`, want: `#date"2026-06-10"`},
	{src: `(date-add-days #date"2026-04-15" 0)`, want: `#date"2026-04-15"`},
	{src: `(date-add-days (make-proto 'nexteggapp.taxprotos.common.v1/Date :year 2025) 1)`, err: `date-add-days: 2025-00-00 is not a date`},
	{src: `(date-add-days (make-date 2147483647 12 31) 1)`, err: `date-add-days: the result is out of range`},
	{src: `(date-add-days #date"2026-01-01" 9223372036854775807)`, err: `date-add-days: the result is out of range`},
	{src: `(date-add-days 1 1)`, err: `date-add-days expects Date, got eval.IntVal`, unchecked: true},
	{src: `(date-add-days #date"2026-01-01" 1.5)`, err: `date-add-days expects Int, got eval.FloatVal`, unchecked: true},

	// date-add-months clamps to the month's last day.
	{src: `(date-add-months #date"2026-04-15" 1)`, want: `#date"2026-05-15"`},
	{src: `(date-add-months #date"2026-01-31" 1)`, want: `#date"2026-02-28"`},
	{src: `(date-add-months #date"2024-01-31" 1)`, want: `#date"2024-02-29"`},
	{src: `(date-add-months #date"2026-11-30" 3)`, want: `#date"2027-02-28"`},
	{src: `(date-add-months #date"2026-03-31" -1)`, want: `#date"2026-02-28"`},
	{src: `(date-add-months #date"2026-01-15" -1)`, want: `#date"2025-12-15"`},
	{src: `(date-add-months #date"2026-05-31" -24)`, want: `#date"2024-05-31"`},
	{src: `(date-add-months (make-date 2147483647 12 1) 1)`, err: `date-add-months: the result is out of range`},
	{src: `(date-add-months #date"2026-01-01" 9223372036854775807)`, err: `date-add-months: the result is out of range`},

	// days-between counts as Form 2210 and FTB 5805 do.
	{src: `(days-between #date"2026-04-15" #date"2026-06-10")`, want: `56`},
	{src: `(days-between #date"2026-06-10" #date"2026-04-15")`, want: `-56`},
	{src: `(days-between #date"2026-04-15" #date"2026-04-15")`, want: `0`},
	{src: `(days-between #date"2025-12-31" #date"2026-12-31")`, want: `365`},
	{src: `(days-between #date"2023-12-31" #date"2024-12-31")`, want: `366`},
	{src: `(days-between #date"2025-07-01" #date"2026-04-15")`, want: `288`},
	{src: `(days-between #date"2026-01-01" (make-proto 'nexteggapp.taxprotos.common.v1/Date))`, err: `days-between: 0000-00-00 is not a date`},

	// date-weekday is 0 for Sunday through 6 for Saturday.
	{src: `(date-weekday #date"1970-01-01")`, want: `4`},
	{src: `(date-weekday #date"1969-12-31")`, want: `3`},
	{src: `(date-weekday #date"2026-04-15")`, want: `3`},
	{src: `(date-weekday #date"2026-11-14")`, want: `6`},
	{src: `(date-weekday #date"2026-11-15")`, want: `0`},
	{src: `(date-weekday #date"1600-03-01")`, want: `3`},

	// month-end.
	{src: `(month-end 2026 2)`, want: `#date"2026-02-28"`},
	{src: `(month-end 2024 2)`, want: `#date"2024-02-29"`},
	{src: `(month-end 1900 2)`, want: `#date"1900-02-28"`},
	{src: `(month-end 2000 2)`, want: `#date"2000-02-29"`},
	{src: `(month-end 2026 4)`, want: `#date"2026-04-30"`},
	{src: `(month-end 2026 12)`, want: `#date"2026-12-31"`},
	{src: `(month-end 2026 13)`, err: `month-end: 2026-13 is not a month`},
	{src: `(month-end 2026 0)`, err: `month-end: 2026-00 is not a month`},

	// days-in-year.
	{src: `(slice (days-in-year 2026) (days-in-year 2024) (days-in-year 1900) (days-in-year 2000) (days-in-year -4))`, want: `[365 366 365 366 366]`},

	// next-business-day skips weekends and the holidays given.
	{src: `(next-business-day #date"2026-04-15" ((inst empty-slice Date)))`, want: `#date"2026-04-15"`},
	{src: `(next-business-day #date"2026-11-14" ((inst empty-slice Date)))`, want: `#date"2026-11-16"`},
	{src: `(next-business-day #date"2026-11-15" ((inst empty-slice Date)))`, want: `#date"2026-11-16"`},
	{src: `(next-business-day #date"2026-04-15" [#date"2026-04-15"])`, want: `#date"2026-04-16"`},
	{src: `(next-business-day #date"2028-01-15" [#date"2028-01-17"])`, want: `#date"2028-01-18"`},
	{src: `(next-business-day #date"2027-04-15" [#date"2027-04-15" #date"2027-04-16" #date"2027-04-19"])`, want: `#date"2027-04-20"`},
	{src: `(next-business-day #date"2026-04-15" [(make-proto 'nexteggapp.taxprotos.common.v1/Date)])`, err: `next-business-day: 0000-00-00 is not a date`},
	{src: `(next-business-day (make-date 2147483647 12 31) [(make-date 2147483647 12 31)])`, err: `next-business-day: the result is out of range`},
	{src: `(next-business-day #date"2026-04-15" 1)`, err: `next-business-day expects Slice as second argument, got eval.IntVal`, unchecked: true},

	// min and max over dates.
	{src: `(min #date"2026-04-15" #date"2026-01-15" #date"2026-06-15")`, want: `#date"2026-01-15"`},
	{src: `(max #date"2026-04-15" #date"2026-01-15" #date"2026-06-15")`, want: `#date"2026-06-15"`},
	{src: `(min #date"2026-04-15" 1)`, err: `min requires all arguments to have type Date, got Int`},
	{src: `(+ #date"2026-04-15" #date"2026-04-15")`, err: `+ not supported on Date`},
	{src: `(min #date"2026-04-15" 1)`, err: `mismatched operand types for min`, unchecked: true},
	{src: `(+ #date"2026-04-15" #date"2026-04-15")`, err: `unsupported type eval.DateVal for +`, unchecked: true},
}

func evalDateCase(tc dateCase) (string, error) {
	mf, err := parser.ParseFile("test.tlisp", tc.src)
	if err != nil {
		return "", err
	}
	if !tc.unchecked {
		if err := types.NewChecker(nil).CheckModule(mf); err != nil {
			return "", err
		}
	}
	v, err := eval.NewEvaluator(nil, nil).EvalModule(mf)
	if err != nil {
		return "", err
	}
	return v.String(), nil
}

func TestDateBuiltins(t *testing.T) {
	for _, tc := range dateCases {
		got, err := evalDateCase(tc)
		switch {
		case tc.err != "":
			if err == nil || !strings.HasSuffix(err.Error(), tc.err) {
				t.Errorf("%s: got %q, err %v; want error %q", tc.src, got, err, tc.err)
			}
		case err != nil || got != tc.want:
			t.Errorf("%s = %q, err %v; want %q", tc.src, got, err, tc.want)
		}
	}
}

// TestDateBuiltinsMatchTime checks the builtins against Go's time package,
// which estimated.Date uses, on every day from 1600 to 2400.
func TestDateBuiltinsMatchTime(t *testing.T) {
	ev := eval.NewEvaluator(nil, nil)
	call := func(name string, args ...eval.Value) eval.Value {
		t.Helper()
		v, err := ev.Call(name, args...)
		if err != nil {
			t.Fatalf("%s%v: %v", name, args, err)
		}
		return v
	}
	dateOf := func(tm time.Time) eval.DateVal {
		return eval.DateVal{Year: int32(tm.Year()), Month: int32(tm.Month()), Day: int32(tm.Day())}
	}
	start := time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2400, 12, 31, 0, 0, 0, 0, time.UTC)
	first := dateOf(start)
	n := int64(0)
	for tm := start; !tm.After(end); tm = tm.AddDate(0, 0, 1) {
		d := dateOf(tm)
		if got := call("date-add-days", first, eval.IntVal{V: n}); got != d {
			t.Fatalf("date-add-days %v %d = %v, want %v", first, n, got, d)
		}
		if got := call("days-between", first, d).(eval.IntVal).V; got != n {
			t.Fatalf("days-between %v %v = %d, want %d", first, d, got, n)
		}
		if got := call("date-weekday", d).(eval.IntVal).V; got != int64(tm.Weekday()) {
			t.Fatalf("date-weekday %v = %d, want %d", d, got, tm.Weekday())
		}
		if tm.Day() == 1 {
			last := tm.AddDate(0, 1, -1)
			if got := call("month-end", eval.IntVal{V: int64(tm.Year())}, eval.IntVal{V: int64(tm.Month())}); got != dateOf(last) {
				t.Fatalf("month-end %d %d = %v, want %v", tm.Year(), tm.Month(), got, dateOf(last))
			}
		}
		if tm.Month() == 1 && tm.Day() == 1 {
			want := int64(time.Date(tm.Year(), 12, 31, 0, 0, 0, 0, time.UTC).YearDay())
			if got := call("days-in-year", eval.IntVal{V: int64(tm.Year())}).(eval.IntVal).V; got != want {
				t.Fatalf("days-in-year %d = %d, want %d", tm.Year(), got, want)
			}
		}
		n++
	}
}
