package eval_test

import (
	"testing"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/taxlisp/eval"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	"github.com/nexteggapp/taxcalcs/taxlisp/types"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	formssa1099pb "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_ssa_1099"
	formw2pb "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_w2"
)

func mustCompileAndEval(t *testing.T, src string) *eval.Evaluator {
	t.Helper()
	mf, err := parser.ParseFile("test.tlisp", src)
	if err != nil {
		t.Fatalf("ParseFile failed: %v", err)
	}
	checker := types.NewChecker(nil)
	if err := checker.CheckModule(mf); err != nil {
		t.Fatalf("CheckModule failed: %v", err)
	}
	ev := eval.NewEvaluator(nil, nil)
	if _, err := ev.EvalModule(mf); err != nil {
		t.Fatalf("EvalModule failed: %v", err)
	}
	return ev
}

func usdDollars(dollars int64) *commonv1.Money {
	return &commonv1.Money{Currency: "USD", Minor: dollars * 100}
}

func TestSSBenefitsWorksheetAndQDCGTWorksheet(t *testing.T) {
	src := `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)
(use-proto 'nexteggapp.taxprotos.us.federal.v1 'fed)

; --- Social Security Benefits Worksheet (Form 1040 Lines 6a & 6b) ---

(= ss-tier1-base (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY #usd"32000.00"
    'fed/FILING_STATUS_SINGLE                 #usd"25000.00"
    'fed/FILING_STATUS_HEAD_OF_HOUSEHOLD      #usd"25000.00"
    'fed/FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE #usd"25000.00"
    'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY   #usd"0.00"))

(= ss-tier2-spread (Table fed/FilingStatus Money)
  (table
    'fed/FILING_STATUS_MARRIED_FILING_JOINTLY #usd"12000.00"
    'fed/FILING_STATUS_SINGLE                 #usd"9000.00"
    'fed/FILING_STATUS_HEAD_OF_HOUSEHOLD      #usd"9000.00"
    'fed/FILING_STATUS_QUALIFYING_SURVIVING_SPOUSE #usd"9000.00"
    'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY   #usd"0.00"))

(def sum-ssa-net-benefits ((docs (Slice ret/FormSSA1099Document))) Money
  (sum (fn ((d ret/FormSSA1099Document)) Money
         (. d 'form.net_benefits))
       docs))

(def compute-ss-benefits-worksheet
  ((status fed/FilingStatus)
   (lived-apart-all-year Bool)
   (ssa-docs (Slice ret/FormSSA1099Document))
   (other-income Money)
   (tax-exempt-interest Money)
   (adjustments-excluding-student-loan Money))
  ret/SSBenefitsWorksheet

  (withs (line1  (round-dollar (clamp-zero (sum-ssa-net-benefits ssa-docs)))
          line2  (round-dollar (* line1 #qty"0.50"))
          line3  other-income
          line4  tax-exempt-interest
          line5  (+ line2 line3 line4)
          line6  adjustments-excluding-student-loan
          line7  (- line5 line6)
          mfs-zero (and (is status 'fed/FILING_STATUS_MARRIED_FILING_SEPARATELY)
                        (not lived-apart-all-year))
          line8  (if mfs-zero
                     #usd"0.00"
                     (lookup-or ss-tier1-base status #usd"25000.00"))
          line9  (clamp-zero (- line7 line8))
          line10 (if mfs-zero
                     #usd"0.00"
                     (lookup-or ss-tier2-spread status #usd"9000.00"))
          line11 (clamp-zero (- line9 line10))
          line12 (min line9 line10)
          line13 (round-dollar (* line12 #qty"0.50"))
          line14 (min line2 line13)
          line15 (round-dollar (* line11 #qty"0.85"))
          line16 (+ line14 line15)
          line17 (round-dollar (* line1 #qty"0.85"))
          line18 (if (<= line7 line8)
                     #usd"0.00"
                     (min line16 line17)))
    (make-proto 'ret/SSBenefitsWorksheet
      :line_1_total_benefits line1
      :line_2_half_of_benefits line2
      :line_3_other_income line3
      :line_4_tax_exempt_interest line4
      :line_5_gross_provisional_income line5
      :line_6_adjustments line6
      :line_7_provisional_income line7
      :line_8_base_amount line8
      :line_9_excess_over_base line9
      :line_10_tier_1_width line10
      :line_11_excess_over_adjusted_base line11
      :line_12_tier_1_amount line12
      :line_13_half_of_tier_1 line13
      :line_14_taxable_from_tier_1 line14
      :line_15_eighty_five_percent_of_tier_2 line15
      :line_16_combined_tiers line16
      :line_17_eighty_five_percent_of_benefits line17
      :line_18_taxable_benefits line18)))

; --- Qualified Dividends and Capital Gain Tax Worksheet (QDCGT) ---

(defstruct Bracket ()
  (floor Money)
  (rate  Quantity))

(= ordinary-brackets-2025-mfj (Slice Bracket)
  [(Bracket #usd"0.00"      #qty"0.10")
   (Bracket #usd"23850.00"  #qty"0.12")
   (Bracket #usd"96950.00"  #qty"0.22")
   (Bracket #usd"206700.00" #qty"0.24")
   (Bracket #usd"394600.00" #qty"0.32")
   (Bracket #usd"501050.00" #qty"0.35")
   (Bracket #usd"751600.00" #qty"0.37")])

(def compute-bracket-tax ((income Money) (brackets (Slice Bracket))) Money
  (let n Int (len brackets)
    (round-dollar
      (fold (fn ((acc Money) (idx Int)) Money
              (withs (b     (elt brackets idx)
                      floor (. b 'floor)
                      rate  (. b 'rate)
                      ceil  (if (< (+ idx 1) n)
                                (. (elt brackets (+ idx 1)) 'floor)
                                income)
                      slice-amt (clamp-zero (- (min income ceil) floor)))
                (+ acc (* slice-amt rate))))
            #usd"0.00"
            [0 1 2 3 4 5 6]))))

(def compute-qdcgt-worksheet-mfj-2025
  ((taxable-income Money)
   (qualified-dividends Money)
   (net-ltcg-and-cap-gain-dist Money))
  ret/QDCGTWorksheet
  (withs (line1  taxable-income
          line2  qualified-dividends
          line3  (clamp-zero net-ltcg-and-cap-gain-dist)
          line4  (+ line2 line3)
          line5  (clamp-zero (- line1 line4))
          line6  #usd"96700.00"
          line7  (min line1 line6)
          line8  (min line5 line7)
          line9  (- line7 line8)
          line10 (min line1 line4)
          line11 line9
          line12 (- line10 line11)
          line13 #usd"600050.00"
          line14 (min line1 line13)
          line15 (+ line5 line9)
          line16 (clamp-zero (- line14 line15))
          line17 (min line12 line16)
          line18 (round-dollar (* line17 #qty"0.15"))
          line19 (+ line9 line17)
          line20 (- line10 line19)
          line21 (round-dollar (* line20 #qty"0.20"))
          line22 (compute-bracket-tax line5 ordinary-brackets-2025-mfj)
          line23 (+ line18 line21 line22)
          line24 (compute-bracket-tax line1 ordinary-brackets-2025-mfj)
          line25 (min line23 line24))
    (make-proto 'ret/QDCGTWorksheet
      :line_1_taxable_income line1
      :line_2_qualified_dividends line2
      :line_3_net_capital_gain line3
      :line_4_total_preferential_income line4
      :line_5_ordinary_taxable_income line5
      :line_6_zero_rate_threshold line6
      :line_7_smaller_of_taxable_or_zero_threshold line7
      :line_8_ordinary_in_zero_band line8
      :line_9_taxed_at_zero_percent line9
      :line_10_smaller_of_taxable_or_preferential line10
      :line_11_zero_rate_amount line11
      :line_12_remaining_preferential line12
      :line_13_fifteen_rate_threshold line13
      :line_14_smaller_of_taxable_or_fifteen_threshold line14
      :line_15_ordinary_plus_zero_band line15
      :line_16_fifteen_band_capacity line16
      :line_17_taxed_at_fifteen_percent line17
      :line_18_tax_at_fifteen_percent line18
      :line_19_zero_and_fifteen_preferential line19
      :line_20_taxed_at_twenty_percent line20
      :line_21_tax_at_twenty_percent line21
      :line_22_tax_on_ordinary_income line22
      :line_23_combined_preferential_and_ordinary_tax line23
      :line_24_regular_tax_on_all_taxable_income line24
      :line_25_tax_on_all_taxable_income line25)))
`
	ev := mustCompileAndEval(t, src)

	// 1. Test SSBenefitsWorksheet:
	// MFJ filer with $30,000 net SSA benefits, $35,000 other income, $0 tax-exempt interest, $0 adjustments.
	ssaDoc := &returnsv1.FormSSA1099Document{
		Form: &formssa1099pb.FormSSA1099{
			NetBenefits: usdDollars(30000),
		},
	}
	res, err := ev.Call(
		"compute-ss-benefits-worksheet",
		eval.SymbolVal{Package: "fed", Name: "FILING_STATUS_MARRIED_FILING_JOINTLY"},
		eval.BoolVal{V: false},
		eval.SliceVal{Elems: []eval.Value{eval.WrapProto(ssaDoc)}},
		eval.NewMoneyCents(35000_00),
		eval.NewMoneyCents(0),
		eval.NewMoneyCents(0),
	)
	if err != nil {
		t.Fatalf("compute-ss-benefits-worksheet failed: %v", err)
	}
	ws := res.(eval.ProtoMessageVal).Proto().(*returnsv1.SSBenefitsWorksheet)
	if ws.GetLine_7ProvisionalIncome().GetMinor() != 50000_00 {
		t.Errorf("got line_7_provisional_income %v, want $50,000", ws.GetLine_7ProvisionalIncome())
	}
	if ws.GetLine_14TaxableFromTier_1().GetMinor() != 6000_00 {
		t.Errorf("got line_14_taxable_from_tier_1 %v, want $6,000", ws.GetLine_14TaxableFromTier_1())
	}
	if ws.GetLine_15EightyFivePercentOfTier_2().GetMinor() != 5100_00 {
		t.Errorf("got line_15_eighty_five_percent_of_tier_2 %v, want $5,100", ws.GetLine_15EightyFivePercentOfTier_2())
	}
	if ws.GetLine_18TaxableBenefits().GetMinor() != 11100_00 {
		t.Errorf("got line_18_taxable_benefits %v, want $11,100", ws.GetLine_18TaxableBenefits())
	}

	// 2. Test QDCGTWorksheet:
	// MFJ filer in 2025 with $150,000 taxable income, $10,000 qualified dividends, $20,000 net LTCG.
	qRes, err := ev.Call(
		"compute-qdcgt-worksheet-mfj-2025",
		eval.NewMoneyCents(150000_00),
		eval.NewMoneyCents(10000_00),
		eval.NewMoneyCents(20000_00),
	)
	if err != nil {
		t.Fatalf("compute-qdcgt-worksheet-mfj-2025 failed: %v", err)
	}
	qws := qRes.(eval.ProtoMessageVal).Proto().(*returnsv1.QDCGTWorksheet)
	if qws.GetLine_9TaxedAtZeroPercent().GetMinor() != 0 {
		t.Errorf("got line_9_taxed_at_zero_percent %v, want $0", qws.GetLine_9TaxedAtZeroPercent())
	}
	if qws.GetLine_17TaxedAtFifteenPercent().GetMinor() != 30000_00 {
		t.Errorf("got line_17_taxed_at_fifteen_percent %v, want $30,000", qws.GetLine_17TaxedAtFifteenPercent())
	}
	if qws.GetLine_18TaxAtFifteenPercent().GetMinor() != 4500_00 {
		t.Errorf("got line_18_tax_at_fifteen_percent %v, want $4,500", qws.GetLine_18TaxAtFifteenPercent())
	}
	if qws.GetLine_22TaxOnOrdinaryIncome().GetMinor() != 16228_00 {
		t.Errorf("got line_22_tax_on_ordinary_income %v, want $16,228", qws.GetLine_22TaxOnOrdinaryIncome())
	}
	if qws.GetLine_24RegularTaxOnAllTaxableIncome().GetMinor() != 22828_00 {
		t.Errorf("got line_24_regular_tax_on_all_taxable_income %v, want $22,828", qws.GetLine_24RegularTaxOnAllTaxableIncome())
	}
	if qws.GetLine_25TaxOnAllTaxableIncome().GetMinor() != 20728_00 {
		t.Errorf("got line_25_tax_on_all_taxable_income %v, want $20,728", qws.GetLine_25TaxOnAllTaxableIncome())
	}
}

func TestUnionsGenericsDatesAndProtoWith(t *testing.T) {
	src := `
(use-proto 'nexteggapp.taxcalcs.returns.v1 'ret)

(deftype (Option T) (Union T Nil))

(def (unwrap-or T) ((x (Option T)) (fallback T)) T
  (if (nil? x) fallback x))

(def describe-tin ((tin ret/TaxpayerID)) String
  (typecase (v (. tin 'value))
    String v
    ret/Withheld "WITHHELD"
    Nil "NONE"))

(def born-before-cutoff? ((dob Date) (cutoff Date)) Bool
  (< dob cutoff))

(def patch-w2-wages ((w2 ret/W2Document) (new-wages Money)) ret/W2Document
  (let updated-form ret/W2Document
    (proto-with w2
      :form (proto-with (. w2 'form)
              :wages_tips_other_compensation new-wages))
    updated-form))
`
	ev := mustCompileAndEval(t, src)

	// Test TaxpayerID oneof variants:
	tinSSN := &returnsv1.TaxpayerID{Value: &returnsv1.TaxpayerID_Number{Number: "123456789"}}
	tinWithheld := &returnsv1.TaxpayerID{Value: &returnsv1.TaxpayerID_Withheld{Withheld: &returnsv1.Withheld{}}}
	tinEmpty := &returnsv1.TaxpayerID{}

	for _, tc := range []struct {
		tin  *returnsv1.TaxpayerID
		want string
	}{
		{tinSSN, "123456789"},
		{tinWithheld, "WITHHELD"},
		{tinEmpty, "NONE"},
	} {
		got, err := ev.Call("describe-tin", eval.WrapProto(tc.tin))
		if err != nil {
			t.Fatalf("describe-tin failed: %v", err)
		}
		if got.(eval.StringVal).V != tc.want {
			t.Errorf("describe-tin(%v) = %q, want %q", tc.tin, got, tc.want)
		}
	}

	// Test Date comparison:
	bornBefore, err := ev.Call(
		"born-before-cutoff?",
		eval.DateVal{Year: 1959, Month: 12, Day: 31},
		eval.DateVal{Year: 1960, Month: 1, Day: 2},
	)
	if err != nil || !bornBefore.(eval.BoolVal).V {
		t.Errorf("expected 1959-12-31 < 1960-01-02 to be true, got %v (err %v)", bornBefore, err)
	}

	// Test proto-with immutable update:
	origW2 := &returnsv1.W2Document{
		Id: "w2-1",
		Form: &formw2pb.FormW2{
			WagesTipsOtherCompensation: usdDollars(100000),
		},
	}
	patchedVal, err := ev.Call("patch-w2-wages", eval.WrapProto(origW2), eval.NewMoneyCents(125000_00))
	if err != nil {
		t.Fatalf("patch-w2-wages failed: %v", err)
	}
	patchedW2 := patchedVal.(eval.ProtoMessageVal).Proto().(*returnsv1.W2Document)
	if origW2.GetForm().GetWagesTipsOtherCompensation().GetMinor() != 100000_00 {
		t.Errorf("original W2 was mutated!")
	}
	if patchedW2.GetForm().GetWagesTipsOtherCompensation().GetMinor() != 125000_00 {
		t.Errorf("patched W2 wages = %v, want $125,000", patchedW2.GetForm().GetWagesTipsOtherCompensation())
	}
}
