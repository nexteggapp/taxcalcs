package payroll_test

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/taxyear/payroll"
)

func w4(t *testing.T, b *payroll.W4Builder) payroll.W4 {
	t.Helper()
	w, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// Worksheet 1A, worked by hand from Rev. Proc. 2025-32's 2026 brackets and
// standard deductions.
func TestFederalWithholding(t *testing.T) {
	for _, c := range []struct {
		name    string
		w       *payroll.W4Builder
		taxable money.USCents
		periods int
		want    money.USCents
	}{
		// $140,000 a year less $16,100 is $123,900: $1,240 + $4,560 +
		// $12,166 + 24% of $18,200, $4,368, is $22,334; a 24th is $930.58.
		{"single", payroll.NewW4(filing.Single), 583_333, 24, 93_058},
		// The same wages less $32,200 is $107,800: $2,480 + $9,120 + 22% of
		// $7,000, $1,540, is $13,140; a 24th is $547.50.
		{"married filing jointly", payroll.NewW4(filing.MarriedFilingJointly), 583_333, 24, 54_750},
		// Step 3's $2,000 a year is $83.33 a paycheck off; Step 4(c) adds $50.
		{"credits and extra", payroll.NewW4(filing.Single).Dependents(2000 * money.Dollar).Extra(50 * money.Dollar), 583_333, 24, 93_058 - 8_333 + 5_000},
		// Step 2: twice $139,999.92 less $16,100 is $263,899.84, whose tax
		// through the 35% band is $61,134.19; halved, $30,567.10, a 24th
		// $1,273.63.
		{"step 2", payroll.NewW4(filing.Single).MultipleJobs(), 583_333, 24, 127_363},
		{"exempt", payroll.NewW4(filing.Single).Exempt(), 583_333, 24, 0},
		{"under the deduction", payroll.NewW4(filing.Single), 50_000, 24, 0},
	} {
		got, rule, err := payroll.FederalWithholding(2026, w4(t, c.w), c.taxable, c.periods)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("%s: %v, want %v (%s)", c.name, got, c.want, rule.Applied())
		}
	}
	if _, _, err := payroll.FederalWithholding(2031, w4(t, payroll.NewW4(filing.Single)), 100, 24); err == nil {
		t.Error("a year with no tables gave no error")
	}
}

func TestFlatSupplemental(t *testing.T) {
	if got := payroll.FlatSupplemental(10_000*money.Dollar, 0); got != 2_200*money.Dollar {
		t.Errorf("22%% of $10,000 is %v", got)
	}
	// $995,000 paid before: $5,000 at 22% and $5,000 at 37%.
	if got := payroll.FlatSupplemental(10_000*money.Dollar, 995_000*money.Dollar); got != 1_100*money.Dollar+1_850*money.Dollar {
		t.Errorf("across $1 million: %v", got)
	}
}

func TestSocialSecurityAndMedicare(t *testing.T) {
	ss, err := payroll.SocialSecurity(2026, 10_000*money.Dollar, 180_000*money.Dollar)
	if err != nil {
		t.Fatal(err)
	}
	if ss != money.Rate(620).Of(4_500*money.Dollar) {
		t.Errorf("Social Security near the base: %v, want 6.2%% of $4,500", ss)
	}
	if got := payroll.Medicare(10_000*money.Dollar, 195_000*money.Dollar); got != 145*money.Dollar+45*money.Dollar {
		t.Errorf("Medicare across $200,000: %v, want $145 and 0.9%% of $5,000", got)
	}
}

func TestPlanLimit(t *testing.T) {
	for _, c := range []struct {
		year, age     int
		base, catchUp money.USCents
	}{
		{2025, 40, 23_500 * money.Dollar, 0},
		{2025, 50, 23_500 * money.Dollar, 7_500 * money.Dollar},
		{2025, 61, 23_500 * money.Dollar, 11_250 * money.Dollar},
		{2026, 40, 24_500 * money.Dollar, 0},
		{2026, 50, 24_500 * money.Dollar, 8_000 * money.Dollar},
		{2026, 61, 24_500 * money.Dollar, 11_250 * money.Dollar},
		{2026, 64, 24_500 * money.Dollar, 8_000 * money.Dollar},
	} {
		b, k, err := payroll.PlanLimit(c.year, c.age)
		if err != nil || b != c.base || k != c.catchUp {
			t.Errorf("%d age %d: %v + %v (%v), want %v + %v", c.year, c.age, b, k, err, c.base, c.catchUp)
		}
	}
	if _, _, err := payroll.PlanLimit(2024, 40); err == nil {
		t.Error("2024: no error, want one for a year with no figures")
	}
}
