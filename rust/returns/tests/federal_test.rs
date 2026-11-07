//! Table-driven tests ported from returns/rules/federal/federal_test.go.
//! (Every return those tests prepare is also a golden vector; see
//! returns_golden.rs.)

// Helpers outside #[test] functions fail the test they serve.
#![allow(clippy::expect_used, clippy::unwrap_used)]

use buffa::MessageField;
use proto::{commonv1, federalv1, returnsv1};
use taxcalcs_returns::rules::federal;

fn usd(dollars: i64) -> commonv1::Money {
    commonv1::Money {
        currency: "USD".into(),
        minor: dollars * 100,
        ..Default::default()
    }
}

fn date(year: i32, month: i32, day: i32) -> commonv1::Date {
    commonv1::Date {
        year,
        month,
        day,
        ..Default::default()
    }
}

fn qty(units: i64, scale: i32) -> commonv1::Quantity {
    commonv1::Quantity {
        units,
        scale,
        ..Default::default()
    }
}

#[track_caller]
fn assert_money_dollars(
    label: &str,
    got: &MessageField<commonv1::Money, impl buffa::ProtoBox<commonv1::Money>>,
    want_dollars: i64,
) {
    let minor = got.as_option().map_or(0, |m| m.minor);
    assert_eq!(
        minor,
        want_dollars * 100,
        "{label} = {minor} cents, want ${want_dollars}.00"
    );
}

fn depreciation_return(
    year: i32,
    businesses: Vec<returnsv1::Business>,
    rentals: Vec<returnsv1::RentalProperty>,
) -> returnsv1::ReturnInputs {
    returnsv1::ReturnInputs {
        schema_version: 1,
        tax_year: year,
        federal: MessageField::some(returnsv1::Federal1040Inputs {
            household: MessageField::some(returnsv1::Household {
                marital_status: returnsv1::MaritalStatus::MARITAL_STATUS_NEVER_MARRIED.into(),
                taxpayer: MessageField::some(returnsv1::Person {
                    id: "taylor".into(),
                    first_name: "Taylor".into(),
                    date_of_birth: MessageField::some(date(1980, 4, 10)),
                    tin: MessageField::some(returnsv1::TaxpayerID {
                        kind: returnsv1::TaxpayerIDKind::TAXPAYER_ID_KIND_SSN.into(),
                        valid_for_employment: Some(true),
                        ..Default::default()
                    }),
                    ..Default::default()
                }),
                ..Default::default()
            }),
            businesses,
            rentals,
            ..Default::default()
        }),
        ..Default::default()
    }
}

fn depreciation_business(
    id: &str,
    receipts: i64,
    assets: Vec<returnsv1::DepreciableAsset>,
) -> returnsv1::Business {
    returnsv1::Business {
        id: id.into(),
        owner: "taylor".into(),
        name: format!("Taylor Logistics {id}"),
        materially_participated: Some(true),
        cash_and_unreported_receipts: MessageField::some(usd(receipts)),
        assets,
        ..Default::default()
    }
}

fn prepare_federal(inputs: &returnsv1::ReturnInputs) -> Box<returnsv1::FederalReturn> {
    let eng = federal::default_engine().expect("DefaultEngine() failed");
    Box::new(
        eng.prepare_federal(inputs)
            .expect("PrepareFederal() failed"),
    )
}

/// onlyForm4562 returns the return's single Form 4562.
#[track_caller]
fn only_form_4562(fed: &returnsv1::FederalReturn) -> &federalv1::Form4562 {
    let fs = &fed.forms.form_4562;
    assert_eq!(fs.len(), 1, "len(Form4562) = {}, want 1", fs.len());
    &fs[0]
}

#[test]
fn load_engine_typechecks_all_modules() {
    let eng = federal::load_engine().expect("LoadEngine() failed");
    assert!(
        !eng.module.expressions.is_empty(),
        "LoadEngine() produced empty module"
    );
}

// Each case is a 2025 return holding one asset under test plus a 5-year
// asset placed in service in November 2025 for $20,000. That asset is all of
// 2025's personal property, so 2025 property uses the mid-quarter convention
// (Q4, year 1: 5.00% = $1,000). Rates are from Pub. 946 (2025) Appendix A.
#[test]
fn macrs_table_rates_and_convention_by_placed_in_service_year() {
    let q4_trigger = returnsv1::DepreciableAsset {
        id: "q4-2025".into(),
        date_placed_in_service: MessageField::some(date(2025, 11, 1)),
        cost_basis: MessageField::some(usd(20000)),
        recovery_period_years: MessageField::some(qty(5, 0)),
        opt_out_bonus_depreciation: Some(true),
        ..Default::default()
    };
    let asset = |id: &str, placed: commonv1::Date, cost: i64, years: i64, mid_quarter: bool| {
        returnsv1::DepreciableAsset {
            id: id.into(),
            date_placed_in_service: MessageField::some(placed),
            cost_basis: MessageField::some(usd(cost)),
            recovery_period_years: MessageField::some(qty(years, 0)),
            opt_out_bonus_depreciation: Some(true),
            convention: if mid_quarter {
                returnsv1::DepreciationConvention::DEPRECIATION_CONVENTION_MID_QUARTER.into()
            } else {
                Default::default()
            },
            ..Default::default()
        }
    };
    let cases = [
        // Table A-5, 5-year, year 3: 22.80% (half-year would be 19.20%).
        (
            "mid-quarter 5-year year 3",
            asset("mq-2023", date(2023, 11, 15), 100000, 5, true),
            22800,
        ),
        // Table A-5, 10-year, year 2: 19.50% (half-year would be 18.00%).
        (
            "mid-quarter 10-year year 2",
            asset("mq-10yr-2024", date(2024, 11, 1), 10000, 10, true),
            1950,
        ),
        // A 2024 asset keeps half-year even though 2025 is a mid-quarter
        // year. Table A-1, 5-year, year 2: 32.00%.
        (
            "prior-year asset keeps half-year",
            asset("hy-2024", date(2024, 3, 1), 10000, 5, false),
            3200,
        ),
        // Table A-1, 20-year (150% DB), year 2: 7.219%.
        (
            "20-year declining balance",
            asset("20yr-2024", date(2024, 5, 1), 100000, 20, false),
            7219,
        ),
        // Table A-1, 15-year, year 6: 6.23%.
        (
            "15-year year 6",
            asset("15yr-2020", date(2020, 6, 1), 100000, 15, false),
            6230,
        ),
    ];
    for (name, a, want_macrs) in cases {
        let fed = prepare_federal(&depreciation_return(
            2025,
            vec![depreciation_business(
                "biz-1",
                500000,
                vec![q4_trigger.clone(), a],
            )],
            vec![],
        ));
        let f4562 = only_form_4562(&fed);
        // The earlier asset is on line 17. q4-2025 is a line 19b row:
        // Table A-5, 5-year, year 1: 5.00% of $20,000 = $1,000.
        assert_money_dollars(
            &format!("{name}: Form4562.Line17"),
            &f4562.prior_year_assets_macrs_depreciation,
            want_macrs,
        );
        let rows = &f4562.current_year_macrs_rows;
        assert!(
            rows.len() == 1 && rows[0].convention == "MQ",
            "{name}: Form4562 line 19 rows = {rows:?}, want one MQ row"
        );
        assert_money_dollars(
            &format!("{name}: Form4562.Line19b"),
            &rows[0].depreciation_deduction,
            1000,
        );
        assert_money_dollars(
            &format!("{name}: Form4562.Line22"),
            &f4562.total_depreciation,
            1000 + want_macrs,
        );
    }
}

// Residential rental property, 27.5 years, Pub. 946 (2025) Table A-6.
#[test]
fn mid_month_real_property_final_years_and_schedule_e_tax_year() {
    let cases = [
        // Year 28, placed in January: 1.970%.
        ("final full-rate year", 2025, date(1998, 1, 15), 5910),
        // Year 29, placed in February: 0 (fully recovered in year 28).
        ("after recovery ends", 2025, date(1997, 2, 10), 0),
        // Year 29, placed in August: 0.455%.
        ("final partial year", 2025, date(1997, 8, 10), 1365),
        // A 2026 rental placed in service in January 2026, year 1: 3.485%.
        ("2026 return", 2026, date(2026, 1, 15), 10455),
    ];
    for (name, year, placed, want) in cases {
        let rental = returnsv1::RentalProperty {
            id: "rent-1".into(),
            owner: "taylor".into(),
            description: "12 Elm St".into(),
            property_type: returnsv1::RentalPropertyType::RENTAL_PROPERTY_TYPE_SINGLE_FAMILY.into(),
            fair_rental_days: 365,
            active_participation: Some(true),
            rents_received: MessageField::some(usd(30000)),
            assets: vec![returnsv1::DepreciableAsset {
                id: "bldg".into(),
                date_placed_in_service: MessageField::some(placed.clone()),
                cost_basis: MessageField::some(usd(300000)),
                recovery_period_years: MessageField::some(qty(275, 1)),
                ..Default::default()
            }],
            ..Default::default()
        };
        let fed = prepare_federal(&depreciation_return(year, vec![], vec![rental]));
        let props = &fed.forms.schedule_e.properties;
        assert_eq!(props.len(), 1, "{name}: len(ScheduleE.Properties)");
        assert_money_dollars(
            &format!("{name}: ScheduleE.Depreciation"),
            &props[0].depreciation_or_depletion,
            want,
        );
        // A building placed in service in an earlier year needs no Form 4562.
        if placed.year != year {
            assert!(
                fed.forms.form_4562.is_empty(),
                "{name}: len(Form4562) = {}, want 0",
                fed.forms.form_4562.len()
            );
            continue;
        }
        let f4562 = only_form_4562(&fed);
        assert_eq!(f4562.business_or_activity_name, "12 Elm St", "{name}");
        let rows = &f4562.current_year_macrs_rows;
        assert_eq!(rows.len(), 1, "{name}: len(Form4562.CurrentYearMacrsRows)");
        let r = &rows[0];
        assert!(
            r.classification == "Residential rental property"
                && r.recovery_period == "27.5 yrs."
                && r.convention == "MM"
                && r.method == "S/L",
            "{name}: Form4562 line 19i row = {r:?}, want residential rental, 27.5 yrs., MM, S/L"
        );
        assert_money_dollars(
            &format!("{name}: Form4562.Line19i.Basis"),
            &r.basis_for_depreciation,
            300000,
        );
        assert_money_dollars(
            &format!("{name}: Form4562.Line19i.Deduction"),
            &r.depreciation_deduction,
            want,
        );
        assert_money_dollars(
            &format!("{name}: Form4562.Line22"),
            &f4562.total_depreciation,
            want,
        );
    }
}

#[test]
fn passenger_auto_limits_by_placed_in_service_year() {
    let car =
        |placed: commonv1::Date, prior: Option<i64>, opt_out: bool| returnsv1::DepreciableAsset {
            id: "car".into(),
            date_placed_in_service: MessageField::some(placed),
            cost_basis: MessageField::some(usd(80000)),
            recovery_period_years: MessageField::some(qty(5, 0)),
            opt_out_bonus_depreciation: opt_out.then_some(true),
            prior_depreciation: prior
                .map_or_else(MessageField::none, |p| MessageField::some(usd(p))),
            listed_property_kind:
                returnsv1::ListedPropertyKind::LISTED_PROPERTY_KIND_PASSENGER_AUTO.into(),
            ..Default::default()
        };
    let cases = [
        // 2025 car, no bonus: Rev. Proc. 2025-16 Table 2, 1st year $12,200
        // (MACRS 20% of $80,000 = $16,000).
        (
            "2025 car without bonus",
            car(date(2025, 4, 1), None, true),
            12200,
        ),
        // 2023 car in its 3rd year: Rev. Proc. 2023-14, $11,700
        // (MACRS 19.20% of $80,000 = $15,360).
        (
            "2023 car in year 3",
            car(date(2023, 4, 1), Some(31700), false),
            11700,
        ),
    ];
    for (name, asset, want) in cases {
        let fed = prepare_federal(&depreciation_return(
            2025,
            vec![depreciation_business("biz-1", 100000, vec![asset])],
            vec![],
        ));
        // Listed property: Part V, line 28 to line 21.
        let f4562 = only_form_4562(&fed);
        assert_money_dollars(
            &format!("{name}: Form4562.Line28"),
            &f4562.total_listed_property_depreciation,
            want,
        );
        assert_money_dollars(
            &format!("{name}: Form4562.Line22"),
            &f4562.total_depreciation,
            want,
        );
    }
}

#[test]
fn section_179_no_figures_for_2024() {
    let eng = federal::default_engine().expect("DefaultEngine() failed");
    let err = eng
        .prepare_federal(&depreciation_return(
            2024,
            vec![depreciation_business(
                "biz-1",
                100000,
                vec![returnsv1::DepreciableAsset {
                    id: "equip".into(),
                    date_placed_in_service: MessageField::some(date(2024, 3, 1)),
                    cost_basis: MessageField::some(usd(10000)),
                    recovery_period_years: MessageField::some(qty(5, 0)),
                    section_179_elected: MessageField::some(usd(10000)),
                    ..Default::default()
                }],
            )],
            vec![],
        ))
        .expect_err("PrepareFederal(2024 with § 179) succeeded");
    assert!(
        err.to_string().contains("out of bounds"),
        "PrepareFederal(2024 with § 179) error = {err}, want an out-of-bounds error"
    );
}
