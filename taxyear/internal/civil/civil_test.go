package civil

import (
	"testing"
	"time"
)

func TestStringMatchesFormat(t *testing.T) {
	for _, d := range []Date{Must(2026, time.January, 5), Must(2026, time.December, 31), Must(999, time.October, 10), {}} {
		want := ""
		if !d.IsZero() {
			want = d.t.Format("2006-01-02")
		}
		if got := d.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	}
}
