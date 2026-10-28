package types_test

import (
	"strings"
	"testing"

	_ "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	"github.com/nexteggapp/taxcalcs/taxlisp/types"
)

func checkSource(t *testing.T, src string) (*types.Checker, error) {
	t.Helper()
	mf, err := parser.ParseFile("test.tlisp", src)
	if err != nil {
		return nil, err
	}
	c := types.NewChecker(nil)
	return c, c.CheckModule(mf)
}

func TestValidProgramsTypecheck(t *testing.T) {
	src := `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

(deftype (Option T) (Union T Nil))

(defstruct Bracket ()
  (floor Money)
  (rate Quantity))

(defstruct (Chosen T) (T)
  (value T)
  (label String))

(= std-deduction-2025 (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_SINGLE #usd"15750.00"
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY #usd"31500.00"))

(def (unwrap-or T) ((opt (Option T)) (fallback T)) T
  (if (nil? opt)
      fallback
      opt))

(def (sum-by T) ((f (Fn (T) Money)) (items (Slice T))) Money
  (fold (fn ((acc Money) (item T)) Money (+ acc (f item)))
        #usd"0.00"
        items))

(def describe-tin ((tin ret/TaxpayerID)) String
  (typecase (v (. tin 'value))
    String v
    ret/Withheld "withheld"
    Nil "unset"))

(def total-w2-wages ((docs ret/Documents)) Money
  (round-dollar
    (sum-by (fn ((w2 ret/W2Document)) Money
              (. w2 'form.wages_tips_other_compensation))
            (. docs 'w2s))))

(def build-1040 ((in ret/ReturnInputs)) fed/Form1040
  (withs (wages (total-w2-wages (. in 'federal.documents))
          std (lookup-or std-deduction-2025 'fed/FILING_STATUS_SINGLE #usd"0.00")
          taxable (clamp-zero (- wages std)))
    (make-proto 'fed/Form1040
      :filing_status 'fed/FILING_STATUS_SINGLE
      :w2_wages wages
      :adjusted_gross_income wages
      :deduction std
      :taxable_income taxable)))
`
	if _, err := checkSource(t, src); err != nil {
		t.Fatalf("expected valid program to typecheck, got: %v", err)
	}
}

func TestTypeErrorsRejected(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		wantSub string
	}{
		{
			name:    "money plus int",
			src:     `(def bad ((m Money) (n Int)) Money (+ m n))`,
			wantSub: "+ requires all arguments to have type Money, got Int",
		},
		{
			name:    "wrong return type",
			src:     `(def bad ((m Money)) Int m)`,
			wantSub: "body has type Money, want Int",
		},
		{
			name: "non-exhaustive typecase",
			src: `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(def bad ((tin ret/TaxpayerID)) String
  (typecase (v (. tin 'value))
    String v
    Nil "none"))
`,
			wantSub: "non-exhaustive typecase",
		},
		{
			name: "unknown protobuf field",
			src: `
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(def bad ((w Money)) fed/Form1040
  (make-proto 'fed/Form1040 :non_existent_line w))
`,
			wantSub: "has no field \"non_existent_line\"",
		},
		{
			name: "wrong protobuf field type",
			src: `
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)
(def bad () fed/Form1040
  (make-proto 'fed/Form1040 :taxable_income 42))
`,
			wantSub: "expects Money, got Int",
		},
		{
			name: "missing struct field",
			src: `
(defstruct Pair () (a Money) (b Money))
(def bad () Pair (make-struct Pair :a #usd"1.00"))
`,
			wantSub: "missing required field :b",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := checkSource(t, tc.src)
			if err == nil {
				t.Fatalf("expected type error containing %q, got nil", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("got error %q, want substring %q", err.Error(), tc.wantSub)
			}
		})
	}
}
