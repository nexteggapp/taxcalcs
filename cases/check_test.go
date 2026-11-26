package cases

import (
	"os"
	"testing"

	casesv1 "github.com/nexteggapp/taxcalcs/cases/v1"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

// TestOutcomes checks how Check grades a right figure, a wrong one, and each
// of them marked as a known gap, on one Kentucky case.
func TestOutcomes(t *testing.T) {
	eng, err := federal.DefaultEngine()
	if err != nil {
		t.Fatal(err)
	}
	forms, err := Forms()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("ky/2025/ky-single-wages.textproto")
	if err != nil {
		t.Fatal(err)
	}
	base := &casesv1.TestCase{}
	if err := prototext.Unmarshal(raw, base); err != nil {
		t.Fatal(err)
	}
	gap := &casesv1.KnownGap{Reason: "test"}
	for _, tc := range []struct {
		name  string
		cents int64
		gap   bool
		want  Outcome
	}{
		{"right", 186900, false, Pass},
		{"wrong", 186800, false, Fail},
		{"wrong, known gap", 186800, true, Gap},
		{"right, known gap", 186900, true, GapClosed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := proto.Clone(base).(*casesv1.TestCase)
			e := &casesv1.Expected{Form: "ky-form-740", Line: "12", Value: &casesv1.Expected_Cents{Cents: tc.cents}}
			if tc.gap {
				e.KnownGap = gap
			}
			c.Expected = []*casesv1.Expected{e}
			results, err := Check(eng, forms, c)
			if err != nil {
				t.Fatal(err)
			}
			if got := results[0].Outcome; got != tc.want {
				t.Errorf("outcome %v, want %v", got, tc.want)
			}
		})
	}
	t.Run("no such line", func(t *testing.T) {
		c := proto.Clone(base).(*casesv1.TestCase)
		c.Expected = []*casesv1.Expected{{Form: "ky-form-740", Line: "999", Value: &casesv1.Expected_Cents{}}}
		if _, err := Check(eng, forms, c); err == nil {
			t.Error("no error for a line the form does not have")
		}
	})
}
