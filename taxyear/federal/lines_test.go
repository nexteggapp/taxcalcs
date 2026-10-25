package federal

import (
	"testing"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear/model"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
	factsv1 "github.com/nexteggapp/taxprotos/go/facts/v1"
	form1040 "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1040"
	form1099misc "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_misc"
	form1099s "github.com/nexteggapp/taxprotos/go/us/federal/v1/form_1099_s"
)

func date(y, m, d int) *commonv1.Date {
	return &commonv1.Date{Year: int32(y), Month: int32(m), Day: int32(d)}
}

func usd(v float64) *commonv1.Money {
	return &commonv1.Money{Currency: "USD", Minor: int64(v * 100)}
}

func testContext(t *testing.T, year int) *model.Context {
	t.Helper()
	ctx, err := model.NewContext(year, date(year, 12, 31), nil)
	if err != nil {
		t.Fatalf("model.NewContext failed: %v", err)
	}
	return ctx
}

func TestIsHomeSale(t *testing.T) {
	for _, tc := range []struct {
		name string
		m    *scenariosv1.OtherIncomeModel
		want bool
	}{
		{
			name: "nil model",
			m:    nil,
			want: false,
		},
		{
			name: "explicit Kind HOME_SALE",
			m: &scenariosv1.OtherIncomeModel{
				Id:   "m1",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
			},
			want: true,
		},
		{
			name: "unspecified Kind with Form 1099-S",
			m: &scenariosv1.OtherIncomeModel{
				Id:   "m2",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNSPECIFIED,
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p1",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:   "f1",
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099S{Form_1099S: &form1099s.Form1099S{}},
							}},
						},
					},
				}},
			},
			want: true,
		},
		{
			name: "unspecified Kind with OneOff HomeSale",
			m: &scenariosv1.OtherIncomeModel{
				Id:   "m3",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNSPECIFIED,
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p2",
					Part: &scenariosv1.OtherIncomePart_OneOff_{
						OneOff: &scenariosv1.OtherIncomePart_OneOff{
							Event: &scenariosv1.OtherIncomePart_OneOff_HomeSale_{
								HomeSale: &scenariosv1.OtherIncomePart_OneOff_HomeSale{},
							},
						},
					},
				}},
			},
			want: true,
		},
		{
			name: "other kind GAMBLING",
			m: &scenariosv1.OtherIncomeModel{
				Id:   "m4",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_GAMBLING,
			},
			want: false,
		},
		{
			name: "unspecified Kind with Form 1099-MISC",
			m: &scenariosv1.OtherIncomeModel{
				Id:   "m5",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNSPECIFIED,
				Parts: []*scenariosv1.OtherIncomePart{{
					Id: "p3",
					Part: &scenariosv1.OtherIncomePart_FromForms_{
						FromForms: &scenariosv1.OtherIncomePart_FromForms{
							Forms: []*scenariosv1.OtherIncomePart_FromForms_Form{{
								Id:   "f2",
								Form: &scenariosv1.OtherIncomePart_FromForms_Form_Form_1099Misc{Form_1099Misc: &form1099misc.Form1099MISC{}},
							}},
						},
					},
				}},
			},
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isHomeSale(tc.m); got != tc.want {
				t.Errorf("isHomeSale() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOtherTotals_AddHomeSale(t *testing.T) {
	var tot otherTotals
	notes := &model.Notes{}
	figs := &scenariosv1.OtherIncomeFigures{
		Gross:           usd(500000),
		Taxable:         usd(30000),
		FederalWithheld: usd(5000),
	}

	ok := tot.add(scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE, figs, notes)
	if !ok {
		t.Errorf("otherTotals.add() returned false, want true")
	}
	// Must NOT add taxable gain to Schedule 1 other income (it belongs on Schedule D)
	if tot.other != 0 {
		t.Errorf("tot.other = %v, want 0", tot.other)
	}
	// Must NOT add a LeaveOut note (it is no longer left out)
	if leftOut := notes.LeftOut(); len(leftOut) > 0 {
		t.Errorf("notes.LeftOut() = %v, want empty", leftOut)
	}
	// Must record federal withholding from Form 1099-S
	if tot.withheld != 5000_00 {
		t.Errorf("tot.withheld = %v, want 5000_00", tot.withheld)
	}
}

func TestInvestments_HomeSaleRouting(t *testing.T) {
	ctx := testContext(t, 2026)
	r := &Return{
		status:  1, // single
		summary: &scenariosv1.ScenarioSummary{},
		form: &factsv1.FederalReturn{
			Form_1040: &form1040.Form1040{TaxYear: 2026},
			ScheduleD: &form1040.ScheduleD{TaxYear: 2026},
		},
	}
	c := &calc{
		ctx: ctx,
		r:   r,
		inputs: &scenariosv1.ScenarioInputs{
			Model: &scenariosv1.TaxYearModel{
				OtherIncome: []*scenariosv1.OtherIncomeModel{{
					Id:   "home-sale-1",
					Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
				}},
			},
		},
		proj: &scenariosv1.YearProjection{
			OtherIncome: []*scenariosv1.OtherIncomeProjection{{
				Id: "home-sale-1",
				Items: []*scenariosv1.Item{{
					Id: "item-1",
					Figures: &scenariosv1.Item_OtherIncome{
						OtherIncome: &scenariosv1.OtherIncomeFigures{
							Gross:   usd(600000),
							Taxable: usd(45000),
						},
					},
				}},
			}},
		},
	}

	if err := c.investments(); err != nil {
		t.Fatalf("investments() error = %v", err)
	}

	// Schedule D should receive the taxable gain as net long-term capital gain
	sd := r.form.GetScheduleD()
	if got := sd.GetNetLongTermGainOrLoss().GetMinor(); got != 45000_00 {
		t.Errorf("Schedule D NetLongTermGainOrLoss = %d, want 45000_00", got)
	}
	if got := sd.GetNetShortTermGainOrLoss().GetMinor(); got != 0 {
		t.Errorf("Schedule D NetShortTermGainOrLoss = %d, want 0", got)
	}
	// Form 1040 line 7 should receive the capital gain
	f := r.form.GetForm_1040()
	if got := f.GetCapitalGainOrLoss().GetMinor(); got != 45000_00 {
		t.Errorf("Form 1040 CapitalGainOrLoss = %d, want 45000_00", got)
	}
	// Total income accumulator should reflect the gain
	if c.income != 45000_00 {
		t.Errorf("c.income = %v, want 45000_00", c.income)
	}
}

func TestInvestments_HomeSaleNetting(t *testing.T) {
	ctx := testContext(t, 2026)
	r := &Return{
		status:  1, // single
		summary: &scenariosv1.ScenarioSummary{},
		form: &factsv1.FederalReturn{
			Form_1040: &form1040.Form1040{TaxYear: 2026},
			ScheduleD: &form1040.ScheduleD{TaxYear: 2026},
		},
	}
	c := &calc{
		ctx: ctx,
		r:   r,
		inputs: &scenariosv1.ScenarioInputs{
			Model: &scenariosv1.TaxYearModel{
				OtherIncome: []*scenariosv1.OtherIncomeModel{{
					Id:   "home-sale-netting",
					Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
				}},
			},
		},
		proj: &scenariosv1.YearProjection{
			// Broker with $10,000 short-term gain (Box A) and $15,000 long-term loss (Box D)
			Brokers: []*scenariosv1.BrokerProjection{{
				Id: "broker-1",
				Year: &scenariosv1.BrokerTotals{
					Boxes: []*scenariosv1.Form8949Totals{
						{Box: scenariosv1.Form8949Box_FORM_8949_BOX_A, Gain: usd(10000)},
						{Box: scenariosv1.Form8949Box_FORM_8949_BOX_D, Gain: usd(-15000)},
					},
				},
			}},
			// Home sale with $50,000 long-term gain
			OtherIncome: []*scenariosv1.OtherIncomeProjection{{
				Id: "home-sale-netting",
				Items: []*scenariosv1.Item{{
					Id: "item-1",
					Figures: &scenariosv1.Item_OtherIncome{
						OtherIncome: &scenariosv1.OtherIncomeFigures{
							Gross:   usd(500000),
							Taxable: usd(50000),
						},
					},
				}},
			}},
		},
		facts: &factsv1.TaxYearFacts{
			// Prior-year capital loss carryovers: $5,000 short-term, $10,000 long-term
			Carryovers: []*factsv1.Carryover{
				{Kind: factsv1.CarryoverKind_CARRYOVER_KIND_SHORT_TERM_CAPITAL_LOSS, Amount: usd(5000)},
				{Kind: factsv1.CarryoverKind_CARRYOVER_KIND_LONG_TERM_CAPITAL_LOSS, Amount: usd(10000)},
			},
		},
	}

	if err := c.investments(); err != nil {
		t.Fatalf("investments() error = %v", err)
	}

	// Net calculations:
	// Short-term: $10,000 gain - $5,000 carryover = $5,000 net short-term gain
	// Long-term: $50,000 (home sale) - $15,000 (broker loss) - $10,000 (carryover) = $25,000 net long-term gain
	// Combined gain: $5,000 + $25,000 = $30,000
	sd := r.form.GetScheduleD()
	if got := sd.GetNetShortTermGainOrLoss().GetMinor(); got != 5000_00 {
		t.Errorf("NetShortTermGainOrLoss = %d, want 5000_00", got)
	}
	if got := sd.GetNetLongTermGainOrLoss().GetMinor(); got != 25000_00 {
		t.Errorf("NetLongTermGainOrLoss = %d, want 25000_00", got)
	}
	if got := sd.GetNetGainOrLoss().GetMinor(); got != 30000_00 {
		t.Errorf("NetGainOrLoss = %d, want 30000_00", got)
	}
	f := r.form.GetForm_1040()
	if got := f.GetCapitalGainOrLoss().GetMinor(); got != 30000_00 {
		t.Errorf("Form 1040 CapitalGainOrLoss = %d, want 30000_00", got)
	}
}

func TestOther_Schedule1Exclusion(t *testing.T) {
	ctx := testContext(t, 2026)
	r := &Return{
		status:  1, // single
		summary: &scenariosv1.ScenarioSummary{},
		form: &factsv1.FederalReturn{
			Form_1040:  &form1040.Form1040{TaxYear: 2026},
			Schedule_1: &form1040.Schedule1{TaxYear: 2026},
			ScheduleD:  &form1040.ScheduleD{TaxYear: 2026},
		},
		notes: model.Notes{},
	}
	c := &calc{
		ctx: ctx,
		r:   r,
		inputs: &scenariosv1.ScenarioInputs{
			Model: &scenariosv1.TaxYearModel{
				OtherIncome: []*scenariosv1.OtherIncomeModel{
					{
						Id:   "home-sale",
						Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
					},
					{
						Id:   "gambling",
						Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_GAMBLING,
					},
					{
						Id:   "unemployment",
						Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_UNEMPLOYMENT,
					},
				},
			},
		},
		proj: &scenariosv1.YearProjection{
			OtherIncome: []*scenariosv1.OtherIncomeProjection{
				{
					Id: "home-sale",
					Items: []*scenariosv1.Item{{
						Id: "item-home",
						Figures: &scenariosv1.Item_OtherIncome{
							OtherIncome: &scenariosv1.OtherIncomeFigures{Gross: usd(700000), Taxable: usd(50000), FederalWithheld: usd(3000)},
						},
					}},
				},
				{
					Id: "gambling",
					Items: []*scenariosv1.Item{{
						Id: "item-gambling",
						Figures: &scenariosv1.Item_OtherIncome{
							OtherIncome: &scenariosv1.OtherIncomeFigures{Gross: usd(8000)},
						},
					}},
				},
				{
					Id: "unemployment",
					Items: []*scenariosv1.Item{{
						Id: "item-unemployment",
						Figures: &scenariosv1.Item_OtherIncome{
							OtherIncome: &scenariosv1.OtherIncomeFigures{Gross: usd(4000)},
						},
					}},
				},
			},
		},
	}

	if err := c.other(); err != nil {
		t.Fatalf("other() error = %v", err)
	}

	sch1 := r.form.GetSchedule_1()
	// Schedule 1 other income should ONLY contain gambling ($8,000), NOT home sale ($50,000)
	if got := sch1.GetTotalOtherIncome().GetMinor(); got != 8000_00 {
		t.Errorf("Schedule 1 TotalOtherIncome = %d, want 8000_00", got)
	}
	if got := sch1.GetUnemploymentCompensation().GetMinor(); got != 4000_00 {
		t.Errorf("Schedule 1 UnemploymentCompensation = %d, want 4000_00", got)
	}
	// Total additional income should be gambling ($8,000) + unemployment ($4,000) = $12,000
	if got := sch1.GetTotalAdditionalIncome().GetMinor(); got != 12000_00 {
		t.Errorf("Schedule 1 TotalAdditionalIncome = %d, want 12000_00", got)
	}
	// Summary should contain all three sources with their respective taxable amounts
	sums := r.summary.GetOtherIncome()
	if len(sums) != 3 {
		t.Fatalf("len(summary.OtherIncome) = %d, want 3", len(sums))
	}
	byID := map[string]*scenariosv1.OtherIncomeSummary{}
	for _, s := range sums {
		byID[s.GetId()] = s
	}
	if got := byID["home-sale"].GetTaxable().GetMinor(); got != 50000_00 {
		t.Errorf("summary home-sale Taxable = %d, want 50000_00", got)
	}
	if got := byID["home-sale"].GetWithheld().GetMinor(); got != 3000_00 {
		t.Errorf("summary home-sale Withheld = %d, want 3000_00", got)
	}
	if got := byID["gambling"].GetTaxable().GetMinor(); got != 8000_00 {
		t.Errorf("summary gambling Taxable = %d, want 8000_00", got)
	}
}

func TestCompute_HomeSalePreferentialTax(t *testing.T) {
	ctx := testContext(t, 2026)

	// Scenario 1: $60,000 wages alone
	inputsWagesOnly := &scenariosv1.ScenarioInputs{
		Facts: &factsv1.TaxYearFacts{
			People:    []*factsv1.Person{{Id: "you", Relationship: factsv1.Relationship_RELATIONSHIP_YOU}},
			Elections: []*factsv1.Election{{Id: "fs", Choice: &factsv1.Election_FilingStatus{FilingStatus: form1040.FilingStatus_FILING_STATUS_SINGLE}}},
		},
		Model: &scenariosv1.TaxYearModel{},
	}
	projWagesOnly := &scenariosv1.YearProjection{
		Employers: []*scenariosv1.EmployerProjection{{
			Id: "emp-1",
			Year: &scenariosv1.PayTotals{
				Boxes: &scenariosv1.W2Boxes{Wages: usd(60000)},
			},
		}},
	}
	retWagesOnly, err := Compute(ctx, projWagesOnly, inputsWagesOnly)
	if err != nil {
		t.Fatalf("Compute(wages only) failed: %v", err)
	}
	taxWagesOnly := retWagesOnly.form.GetForm_1040().GetTotalTax().GetMinor()

	// Scenario 2: $60,000 wages + $50,000 home sale long-term capital gain
	inputsHomeSale := &scenariosv1.ScenarioInputs{
		Facts: &factsv1.TaxYearFacts{
			People:    []*factsv1.Person{{Id: "you", Relationship: factsv1.Relationship_RELATIONSHIP_YOU}},
			Elections: []*factsv1.Election{{Id: "fs", Choice: &factsv1.Election_FilingStatus{FilingStatus: form1040.FilingStatus_FILING_STATUS_SINGLE}}},
		},
		Model: &scenariosv1.TaxYearModel{
			OtherIncome: []*scenariosv1.OtherIncomeModel{{
				Id:   "home-sale",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
			}},
		},
	}
	projHomeSale := &scenariosv1.YearProjection{
		Employers: []*scenariosv1.EmployerProjection{{
			Id: "emp-1",
			Year: &scenariosv1.PayTotals{
				Boxes: &scenariosv1.W2Boxes{Wages: usd(60000)},
			},
		}},
		OtherIncome: []*scenariosv1.OtherIncomeProjection{{
			Id: "home-sale",
			Items: []*scenariosv1.Item{{
				Id: "item-1",
				Figures: &scenariosv1.Item_OtherIncome{
					OtherIncome: &scenariosv1.OtherIncomeFigures{Gross: usd(500000), Taxable: usd(50000)},
				},
			}},
		}},
	}
	retHomeSale, err := Compute(ctx, projHomeSale, inputsHomeSale)
	if err != nil {
		t.Fatalf("Compute(home sale) failed: %v", err)
	}
	taxHomeSale := retHomeSale.form.GetForm_1040().GetTotalTax().GetMinor()

	// Scenario 3: $60,000 wages + $50,000 ordinary other income (gambling)
	inputsOrdinary := &scenariosv1.ScenarioInputs{
		Facts: &factsv1.TaxYearFacts{
			People:    []*factsv1.Person{{Id: "you", Relationship: factsv1.Relationship_RELATIONSHIP_YOU}},
			Elections: []*factsv1.Election{{Id: "fs", Choice: &factsv1.Election_FilingStatus{FilingStatus: form1040.FilingStatus_FILING_STATUS_SINGLE}}},
		},
		Model: &scenariosv1.TaxYearModel{
			OtherIncome: []*scenariosv1.OtherIncomeModel{{
				Id:   "gambling",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_GAMBLING,
			}},
		},
	}
	projOrdinary := &scenariosv1.YearProjection{
		Employers: []*scenariosv1.EmployerProjection{{
			Id: "emp-1",
			Year: &scenariosv1.PayTotals{
				Boxes: &scenariosv1.W2Boxes{Wages: usd(60000)},
			},
		}},
		OtherIncome: []*scenariosv1.OtherIncomeProjection{{
			Id: "gambling",
			Items: []*scenariosv1.Item{{
				Id: "item-1",
				Figures: &scenariosv1.Item_OtherIncome{
					OtherIncome: &scenariosv1.OtherIncomeFigures{Gross: usd(50000), Taxable: usd(50000)},
				},
			}},
		}},
	}
	retOrdinary, err := Compute(ctx, projOrdinary, inputsOrdinary)
	if err != nil {
		t.Fatalf("Compute(ordinary) failed: %v", err)
	}
	taxOrdinary := retOrdinary.form.GetForm_1040().GetTotalTax().GetMinor()

	// The home sale capital gain should be taxed at the preferential LTCG rate (15%),
	// NOT at ordinary income marginal tax rates (which reach 22%).
	// Therefore:
	// Tax with home sale capital gain must be strictly less than tax with ordinary other income.
	homeSaleMarginalTax := taxHomeSale - taxWagesOnly
	ordinaryMarginalTax := taxOrdinary - taxWagesOnly

	if homeSaleMarginalTax >= ordinaryMarginalTax {
		t.Errorf("homeSaleMarginalTax (%d) >= ordinaryMarginalTax (%d); preferential capital gains rate was not applied",
			homeSaleMarginalTax, ordinaryMarginalTax)
	}

	// Verify that Form 1040 line 7 and Schedule D received the gain
	if got := retHomeSale.form.GetForm_1040().GetCapitalGainOrLoss().GetMinor(); got != 50000_00 {
		t.Errorf("Form 1040 CapitalGainOrLoss = %d, want 50000_00", got)
	}
	if got := retHomeSale.form.GetScheduleD().GetNetLongTermGainOrLoss().GetMinor(); got != 50000_00 {
		t.Errorf("Schedule D NetLongTermGainOrLoss = %d, want 50000_00", got)
	}
	if got := retHomeSale.form.GetSchedule_1().GetTotalOtherIncome().GetMinor(); got != 0 {
		t.Errorf("Schedule 1 TotalOtherIncome = %d, want 0", got)
	}
}

func TestCompute_HomeSaleWithFederalWithholding(t *testing.T) {
	ctx := testContext(t, 2026)
	inputs := &scenariosv1.ScenarioInputs{
		Facts: &factsv1.TaxYearFacts{
			People:    []*factsv1.Person{{Id: "you", Relationship: factsv1.Relationship_RELATIONSHIP_YOU}},
			Elections: []*factsv1.Election{{Id: "fs", Choice: &factsv1.Election_FilingStatus{FilingStatus: form1040.FilingStatus_FILING_STATUS_SINGLE}}},
		},
		Model: &scenariosv1.TaxYearModel{
			OtherIncome: []*scenariosv1.OtherIncomeModel{{
				Id:   "home-sale-withholding",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
			}},
		},
	}
	proj := &scenariosv1.YearProjection{
		OtherIncome: []*scenariosv1.OtherIncomeProjection{{
			Id: "home-sale-withholding",
			Items: []*scenariosv1.Item{{
				Id: "item-1",
				Figures: &scenariosv1.Item_OtherIncome{
					OtherIncome: &scenariosv1.OtherIncomeFigures{
						Gross:           usd(600000),
						Taxable:         usd(50000),
						FederalWithheld: usd(7500),
					},
				},
			}},
		}},
	}
	ret, err := Compute(ctx, proj, inputs)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	// Federal withholding from Form 1099-S must flow to Form 1040 line 25b (WithheldFrom_1099)
	f := ret.form.GetForm_1040()
	if got := f.GetWithheldFrom_1099().GetMinor(); got != 7500_00 {
		t.Errorf("Form 1040 WithheldFrom_1099 = %d, want 7500_00", got)
	}
	if got := f.GetTotalWithheld().GetMinor(); got != 7500_00 {
		t.Errorf("Form 1040 TotalWithheld = %d, want 7500_00", got)
	}
	// Capital gain must still route to Schedule D / Form 1040 line 7
	if got := f.GetCapitalGainOrLoss().GetMinor(); got != 50000_00 {
		t.Errorf("Form 1040 CapitalGainOrLoss = %d, want 50000_00", got)
	}
	if got := ret.form.GetScheduleD().GetNetLongTermGainOrLoss().GetMinor(); got != 50000_00 {
		t.Errorf("Schedule D NetLongTermGainOrLoss = %d, want 50000_00", got)
	}
}

func TestCompute_FullSection121Exclusion(t *testing.T) {
	ctx := testContext(t, 2026)
	inputs := &scenariosv1.ScenarioInputs{
		Facts: &factsv1.TaxYearFacts{
			People:    []*factsv1.Person{{Id: "you", Relationship: factsv1.Relationship_RELATIONSHIP_YOU}},
			Elections: []*factsv1.Election{{Id: "fs", Choice: &factsv1.Election_FilingStatus{FilingStatus: form1040.FilingStatus_FILING_STATUS_SINGLE}}},
		},
		Model: &scenariosv1.TaxYearModel{
			OtherIncome: []*scenariosv1.OtherIncomeModel{{
				Id:   "home-sale-fully-excluded",
				Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
			}},
		},
	}
	// Sale of $400k, basis $200k, selling expenses $20k -> realized gain $180k.
	// Section 121 exclusion is $250k, so Taxable is $0.
	proj := &scenariosv1.YearProjection{
		OtherIncome: []*scenariosv1.OtherIncomeProjection{{
			Id: "home-sale-fully-excluded",
			Items: []*scenariosv1.Item{{
				Id: "item-1",
				Figures: &scenariosv1.Item_OtherIncome{
					OtherIncome: &scenariosv1.OtherIncomeFigures{
						Gross:    usd(400000),
						Expenses: usd(220000),
						Taxable:  usd(0),
					},
				},
			}},
		}},
	}
	ret, err := Compute(ctx, proj, inputs)
	if err != nil {
		t.Fatalf("Compute failed: %v", err)
	}
	f := ret.form.GetForm_1040()
	if got := f.GetCapitalGainOrLoss().GetMinor(); got != 0 {
		t.Errorf("CapitalGainOrLoss = %d, want 0", got)
	}
	if got := ret.form.GetScheduleD().GetNetLongTermGainOrLoss().GetMinor(); got != 0 {
		t.Errorf("Schedule D NetLongTermGainOrLoss = %d, want 0", got)
	}
	if got := ret.form.GetSchedule_1().GetTotalOtherIncome().GetMinor(); got != 0 {
		t.Errorf("Schedule 1 TotalOtherIncome = %d, want 0", got)
	}
}

func TestInvestments_HomeSaleNondeductibleLoss(t *testing.T) {
	ctx := testContext(t, 2026)
	r := &Return{
		status:  1, // single
		summary: &scenariosv1.ScenarioSummary{},
		form: &factsv1.FederalReturn{
			Form_1040: &form1040.Form1040{TaxYear: 2026},
			ScheduleD: &form1040.ScheduleD{TaxYear: 2026},
		},
	}
	// Home sale sold at a loss: price $300k, basis $350k.
	// Under 26 U.S.C. § 165(c), personal losses are nondeductible, so taxable is $0.
	c := &calc{
		ctx: ctx,
		r:   r,
		inputs: &scenariosv1.ScenarioInputs{
			Model: &scenariosv1.TaxYearModel{
				OtherIncome: []*scenariosv1.OtherIncomeModel{{
					Id:   "home-loss",
					Kind: scenariosv1.OtherIncomeKind_OTHER_INCOME_KIND_HOME_SALE,
				}},
			},
		},
		proj: &scenariosv1.YearProjection{
			OtherIncome: []*scenariosv1.OtherIncomeProjection{{
				Id: "home-loss",
				Items: []*scenariosv1.Item{{
					Id: "item-loss",
					Figures: &scenariosv1.Item_OtherIncome{
						OtherIncome: &scenariosv1.OtherIncomeFigures{
							Gross:    usd(300000),
							Expenses: usd(350000),
							Taxable:  usd(0),
						},
					},
				}},
			}},
		},
	}

	if err := c.investments(); err != nil {
		t.Fatalf("investments() error = %v", err)
	}

	sd := r.form.GetScheduleD()
	if got := sd.GetNetLongTermGainOrLoss().GetMinor(); got != 0 {
		t.Errorf("Schedule D NetLongTermGainOrLoss = %d, want 0", got)
	}
	if got := r.form.GetForm_1040().GetCapitalGainOrLoss().GetMinor(); got != 0 {
		t.Errorf("Form 1040 CapitalGainOrLoss = %d, want 0", got)
	}
}
