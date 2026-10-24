package retirement

import (
	"testing"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
)

func months(m int32) int {
	if m <= 0 {
		return 12
	}
	return int(m)
}

// The HSA limit is the year's annual limitation for the coverage, with the
// $1,000 catch-up at 55, a twelfth for each month covered: Rev. Proc.
// 2025-19 § 2.01(1) gives 2026's $4,400 self-only and $8,750 family.
func TestHsaLimit(t *testing.T) {
	self, family := factsv1.HsaCoverage_HSA_COVERAGE_SELF_ONLY, factsv1.HsaCoverage_HSA_COVERAGE_FAMILY
	for _, c := range []struct {
		year     int
		coverage factsv1.HsaCoverage
		months   int32
		age      int
		want     int64
		ok       bool
	}{
		{2026, self, 12, 40, 440_000, true},
		{2026, family, 0, 40, 875_000, true},
		{2026, family, 12, 55, 975_000, true},
		{2026, self, 6, 40, 220_000, true},
		{2025, self, 12, 40, 430_000, true},
		{2025, family, 12, 60, 955_000, true},
		// No HSA-eligible plan: nothing can go in, § 223(c)(1).
		{2026, factsv1.HsaCoverage_HSA_COVERAGE_NONE, 12, 40, 0, true},
		{2026, factsv1.HsaCoverage_HSA_COVERAGE_UNSPECIFIED, 12, 40, 0, false},
		{2027, self, 12, 40, 0, false},
	} {
		got, ok := limitFor(c.year, c.coverage, months(c.months), c.age, true)
		if int64(got) != c.want || ok != c.ok {
			t.Errorf("%d %v for %d months at %d: %d %v, want %d %v", c.year, c.coverage, c.months, c.age, got, ok, c.want, c.ok)
		}
	}
}

// Covered half the year, the limit is half the year's until the last-month
// rule's question is answered yes: then it is the whole year's.
func TestHsaLastMonthRule(t *testing.T) {
	facts := &factsv1.TaxYearFacts{TaxYear: 2026, People: []*factsv1.Person{{Id: "you", Relationship: factsv1.Relationship_RELATIONSHIP_YOU,
		DateOfBirth: &commonv1.Date{Year: 1980, Month: 1, Day: 1}, HsaCoverage: factsv1.HsaCoverage_HSA_COVERAGE_FAMILY, HsaMonths: 6}}}
	limit := func() (money.USCents, *model.Notes) {
		ctx, err := model.NewContext(2026, &commonv1.Date{Year: 2026, Month: 9, Day: 8}, facts)
		if err != nil {
			t.Fatal(err)
		}
		n := &model.Notes{}
		l, _ := HsaLimit(ctx, "you", n)
		return l, n
	}
	if l, n := limit(); l != 437_500 || len(n.Questions()) != 1 {
		t.Errorf("unanswered: %v with %d questions, want $4,375 and the question", l, len(n.Questions()))
	}
	facts.Positions = []*factsv1.Position{{Id: "p", Question: LastMonthRule, Subjects: []string{"you"}, Answer: &factsv1.Position_Reply{Reply: factsv1.Reply_REPLY_YES}}}
	if l, _ := limit(); l != 875_000 {
		t.Errorf("answered yes: %v, want the whole year's $8,750", l)
	}
}
