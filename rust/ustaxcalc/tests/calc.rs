//! The calculator tests, ported from calc_test.go, capital_test.go,
//! additional_test.go, ira_test.go, itemised_test.go, tables_test.go and
//! example_test.go.
//!
//! Not ported: the tests that read the Go package's testdata extracts to
//! check a figure or phrase is in its source (TestTablesMatchRevenueProcedure,
//! TestTables2025MatchSources, TestAgainstIRSTaxTable,
//! TestSafeHarbourMatchesPublication505 and the *AreInTheirSources tests).
//! TestCapitalLossLimitIsScheduleDs is ported with Schedule D line 21's
//! $3,000 and $1,500 written in.

#![allow(clippy::unwrap_used)]

use estimated::{Month, must_date};
use money::{DOLLAR, Rate, USCents, dollars, percent};
use taxcalcs_ustaxcalc as us;
use us::{
    Conditions, Election, Error, FilingStatus, HEAD_OF_HOUSEHOLD, IraContributionBuilder,
    MARRIED_FILING_JOINTLY, MARRIED_FILING_SEPARATELY, PositionKind, Required, SINGLE,
    SURVIVING_SPOUSE,
};

const D: USCents = DOLLAR;

fn has(rs: &[Required], k: PositionKind) -> bool {
    rs.iter().any(|r| r.position() == k)
}

/// Rev. Proc. 2025-32 states each band twice: as a rate on the excess over a
/// threshold, and as a base amount that the bands below it come to.
#[test]
fn ladder_against_published_base_amounts() {
    let cases: [(FilingStatus, &[(i64, USCents)]); 4] = [
        (
            SINGLE,
            &[
                (12_400, dollars(1_240)),
                (50_400, dollars(5_800)),
                (105_700, dollars(17_966)),
                (201_775, dollars(41_024)),
                (256_225, dollars(58_448)),
                (640_600, USCents(19_297_925)),
            ],
        ),
        (
            MARRIED_FILING_JOINTLY,
            &[
                (24_800, dollars(2_480)),
                (100_800, dollars(11_600)),
                (211_400, dollars(35_932)),
                (403_550, dollars(82_048)),
                (512_450, dollars(116_896)),
                (768_700, USCents(20_658_350)),
            ],
        ),
        (
            HEAD_OF_HOUSEHOLD,
            &[
                (17_700, dollars(1_770)),
                (67_450, dollars(7_740)),
                (105_700, dollars(16_155)),
                (201_750, dollars(39_207)),
                (256_200, dollars(56_631)),
                (640_600, dollars(191_171)),
            ],
        ),
        (
            MARRIED_FILING_SEPARATELY,
            &[(256_225, dollars(58_448)), (384_350, USCents(10_329_175))],
        ),
    ];
    for (status, points) in cases {
        for &(at, base) in points {
            let got = us::ordinary_income_tax(2026, status, dollars(at)).unwrap();
            assert_eq!(got.value().total, base, "{status} tax on ${at}");
        }
    }
}

#[test]
fn ladder_worked_example() {
    let got = us::ordinary_income_tax(2026, SINGLE, D * 192_275)
        .unwrap()
        .value();
    assert_eq!(got.total, dollars(38_744));
    assert_eq!(got.marginal, percent(24));
    assert_eq!(got.room_in_bracket, dollars(9_500));
    for (rate, income, tax) in [
        (10, 12_400, 1_240),
        (12, 38_000, 4_560),
        (22, 55_300, 12_166),
        (24, 86_575, 20_778),
    ] {
        let band = got.bands.iter().find(|b| b.rate == percent(rate)).unwrap();
        assert_eq!((band.income, band.tax), (dollars(income), dollars(tax)));
    }
    let summed: USCents = got.bands.iter().map(|b| b.tax).sum();
    assert_eq!(summed, got.total);
}

#[test]
fn ladder_edges() {
    for (name, income, want) in [
        ("no income", USCents(0), USCents(0)),
        (
            "a deduction larger than income",
            dollars(-5_000),
            USCents(0),
        ),
        ("one cent", USCents(1), USCents(0)),
        ("the first dollar", dollars(1), USCents(10)),
    ] {
        let got = us::ordinary_income_tax(2026, SINGLE, income).unwrap();
        assert_eq!(got.value().total, want, "{name}");
    }
}

#[test]
fn gains_stack_on_ordinary_income() {
    let gain = dollars(10_000);
    for (name, ordinary, zero, fifteen, twenty) in [
        ("below the zero rate amount", 20_000, 10_000, 0, 0),
        ("straddling the zero rate amount", 10_000, 10_000, 0, 0),
        ("split across the zero rate amount", 45_000, 4_450, 5_550, 0),
        ("entirely at 15%", 100_000, 0, 10_000, 0),
        ("split across the 15% amount", 540_000, 0, 5_500, 4_500),
        ("entirely at 20%", 600_000, 0, 0, 10_000),
    ] {
        let g = us::long_term_gains_tax(2026, SINGLE, dollars(ordinary), gain)
            .unwrap()
            .value();
        assert_eq!(
            (g.at_zero, g.at_15, g.at_20),
            (dollars(zero), dollars(fifteen), dollars(twenty)),
            "{name}"
        );
        assert_eq!(g.at_zero + g.at_15 + g.at_20, gain);
        assert_eq!(g.total, percent(15).of(g.at_15) + percent(20).of(g.at_20));
    }
}

#[test]
fn gains_worked_example() {
    let got = us::long_term_gains_tax(2026, SINGLE, D * 192_275, dollars(8_420))
        .unwrap()
        .value();
    assert_eq!(got.total, dollars(1_263));
    assert_eq!(got.at_15, dollars(8_420));
}

#[test]
fn gains_require_a_lot_position() {
    let got = us::long_term_gains_tax(2026, SINGLE, dollars(50_000), dollars(1_000)).unwrap();
    assert!(got.needs());
    assert_eq!(got.required()[0].position(), PositionKind::LotSelection);
}

#[test]
fn self_employment_wage_base_interaction() {
    let profit = dollars(57_273);
    let s = us::self_employment_tax(2026, SINGLE, profit, USCents(0), USCents(0))
        .unwrap()
        .value();
    assert_eq!(s.net_earnings, USCents(5_289_162));
    assert_eq!(s.social_security, Rate(1240).of(s.net_earnings));
    assert_eq!(s.additional_medicare, USCents(0));
    assert_eq!(s.above_the_line_deduction, percent(50).of(s.total));

    let s = us::self_employment_tax(2026, SINGLE, profit, dollars(200_000), dollars(200_000))
        .unwrap()
        .value();
    assert_eq!(s.social_security, USCents(0));
    assert_eq!(s.medicare, Rate(290).of(s.net_earnings));
    assert_eq!(s.additional_medicare, Rate(90).of(s.net_earnings));

    let s = us::self_employment_tax(2026, SINGLE, profit, dollars(150_000), dollars(150_000))
        .unwrap()
        .value();
    assert_eq!(s.social_security, Rate(1240).of(dollars(34_500)));

    let s = us::self_employment_tax(2026, SINGLE, dollars(400), USCents(0), USCents(0))
        .unwrap()
        .value();
    assert_eq!(s.total, USCents(0));
}

#[test]
fn safe_harbour_picks_the_lesser_test() {
    let current = dollars(48_169);
    let h = |status, prior, agi, filed| {
        us::required_annual_payment(2026, status, current, dollars(prior), dollars(agi), filed)
            .unwrap()
            .value()
    };
    let got = h(SINGLE, 39_400, 172_800, true);
    assert_eq!(got.prior_year_rate, percent(110));
    assert_eq!(got.prior_year_test, dollars(43_340));
    assert_eq!(got.required, got.prior_year_test);

    let got = h(SINGLE, 39_400, 140_000, true);
    assert_eq!(got.prior_year_rate, percent(100));
    assert_eq!(got.required, dollars(39_400));

    assert_eq!(
        h(MARRIED_FILING_SEPARATELY, 39_400, 80_000, true).prior_year_rate,
        percent(110)
    );
    assert_eq!(h(SINGLE, 2_000, 30_000, true).required, dollars(2_000));
    let got = h(SINGLE, 0, 0, false);
    assert_eq!(got.required, got.current_year_test);
}

#[test]
fn instalments_worked_example() {
    let got = us::instalments(2026, dollars(43_340));
    let want = [
        (must_date(2026, Month::APRIL, 15), 10_835),
        (must_date(2026, Month::JUNE, 15), 21_670),
        (must_date(2026, Month::SEPTEMBER, 15), 32_505),
        (must_date(2027, Month::JANUARY, 15), 43_340),
    ];
    assert_eq!(got.len(), want.len());
    for (g, (due, target)) in got.iter().zip(want) {
        assert_eq!((g.due, g.cumulative_target), (due, dollars(target)));
    }
}

#[test]
fn withholding_is_credited_evenly() {
    let withheld = dollars(41_808);
    for (q, want) in [(1, 10_452), (2, 20_904), (3, 31_356), (4, 41_808)] {
        assert_eq!(us::withholding_credited_by(withheld, q), dollars(want));
    }
    assert_eq!(us::withholding_credited_by(withheld, 5), USCents(0));
}

#[test]
fn standard_deduction_aged_and_blind() {
    let aged = Conditions {
        aged_65_or_older: true,
        ..Conditions::default()
    };
    for (name, status, cond, want) in [
        ("single, neither", SINGLE, Conditions::default(), 16_100),
        ("single, aged", SINGLE, aged, 18_150),
        (
            "single, aged and blind",
            SINGLE,
            Conditions {
                blind: true,
                ..aged
            },
            20_200,
        ),
        ("joint, one aged", MARRIED_FILING_JOINTLY, aged, 33_850),
        (
            "joint, both aged",
            MARRIED_FILING_JOINTLY,
            Conditions {
                spouse_aged_65_or_older: true,
                ..aged
            },
            35_500,
        ),
        ("surviving spouse, aged", SURVIVING_SPOUSE, aged, 33_850),
        ("head of household, aged", HEAD_OF_HOUSEHOLD, aged, 26_200),
        (
            "separate, spouse aged",
            MARRIED_FILING_SEPARATELY,
            Conditions {
                spouse_aged_65_or_older: true,
                ..Conditions::default()
            },
            16_100,
        ),
    ] {
        let got = us::standard_deduction(2026, status, cond).unwrap();
        assert_eq!(got.value(), dollars(want), "{name}");
    }
}

#[test]
fn deduction_taken_always_asks_for_the_election() {
    for (itemised, amount, item) in [
        (0, 16_100, false),
        (10_000, 16_100, false),
        (20_000, 20_000, true),
    ] {
        let got = us::deduction_taken(
            2026,
            SINGLE,
            Conditions::default(),
            dollars(itemised),
            Election::Larger,
        )
        .unwrap();
        let v = got.value();
        assert_eq!((v.amount, v.itemised), (dollars(amount), item));
        assert_eq!(v.standard, dollars(16_100));
        assert!(got.needs());
    }
}

#[test]
fn every_result_cites_its_rule() {
    let ladder = us::ordinary_income_tax(2026, SINGLE, dollars(80_000)).unwrap();
    let seca =
        us::self_employment_tax(2026, SINGLE, dollars(50_000), USCents(0), USCents(0)).unwrap();
    let harbour = us::required_annual_payment(
        2026,
        SINGLE,
        dollars(20_000),
        dollars(18_000),
        dollars(90_000),
        true,
    )
    .unwrap();
    for applied in [ladder.applied(), seca.applied(), harbour.applied()] {
        assert!(!applied.is_empty());
        for r in applied {
            assert!(!r.cite().is_empty() && !r.applied().is_empty());
        }
    }
}

#[test]
fn capital_loss_limit_is_schedule_ds() {
    for year in [2025, 2026] {
        for (status, limit) in [
            (SINGLE, 3_000),
            (MARRIED_FILING_JOINTLY, 3_000),
            (MARRIED_FILING_SEPARATELY, 1_500),
        ] {
            let g = us::new_capital_gains()
                .short_term(-dollars(10_000))
                .long_term(dollars(2_000))
                .build()
                .unwrap();
            let n = us::net_capital_gains(year, status, g).unwrap().value();
            assert_eq!(n.combined(), -dollars(8_000));
            assert_eq!(n.in_income(), -dollars(limit));
            assert_eq!(n.net_capital_gain(), USCents(0));
        }
    }
}

#[test]
fn net_capital_gains() {
    let d = dollars;
    let cases = [
        (
            us::new_capital_gains()
                .short_term(-d(5_000))
                .long_term(d(12_000))
                .distributions(d(1_000))
                .build(),
            (-d(5_000), d(13_000), d(8_000), d(8_000)),
        ),
        (
            us::new_capital_gains()
                .short_term(d(4_000))
                .long_term(d(2_000))
                .build(),
            (d(4_000), d(2_000), d(6_000), d(2_000)),
        ),
        (
            us::new_capital_gains()
                .short_term(d(3_000))
                .long_term(-d(1_000))
                .build(),
            (d(3_000), -d(1_000), d(2_000), USCents(0)),
        ),
        (
            us::new_capital_gains()
                .short_term(d(1_000))
                .short_term_carryover(d(1_500))
                .long_term(d(6_000))
                .long_term_carryover(d(500))
                .build(),
            (-d(500), d(5_500), d(5_000), d(5_000)),
        ),
        (
            us::new_capital_gains().short_term(-d(1_200)).build(),
            (-d(1_200), USCents(0), -d(1_200), USCents(0)),
        ),
    ];
    for (g, want) in cases {
        let res = us::net_capital_gains(2026, SINGLE, g.unwrap()).unwrap();
        let n = res.value();
        assert_eq!(
            (
                n.short_term(),
                n.long_term(),
                n.in_income(),
                n.net_capital_gain()
            ),
            want
        );
        assert!(!res.applied().is_empty());
    }
    assert!(
        us::new_capital_gains()
            .long_term_carryover(USCents(-1))
            .build()
            .is_err()
    );
    assert!(
        us::new_capital_gains()
            .distributions(USCents(-1))
            .build()
            .is_err()
    );
    assert!(us::net_capital_gains(1999, SINGLE, us::CapitalGains::default()).is_err());
}

#[test]
fn tax_on_income_is_the_worksheet() {
    let d = dollars;
    let res = us::tax_on_income(2026, SINGLE, d(60_000), d(5_000), d(10_000)).unwrap();
    let v = res.value();
    assert_eq!(
        (v.ordinary_income(), v.preferential_income()),
        (d(45_000), d(15_000))
    );
    let ladder = us::ordinary_income_tax(2026, SINGLE, d(45_000))
        .unwrap()
        .value();
    let lt = us::long_term_gains_tax(2026, SINGLE, d(45_000), d(15_000))
        .unwrap()
        .value();
    assert_eq!(v.ladder().total, ladder.total);
    assert_eq!(v.gains(), lt);
    assert_eq!(v.total(), ladder.total + lt.total);
    assert!(has(&res.required(), PositionKind::LotSelection));

    let capped = us::tax_on_income(2026, SINGLE, d(10_000), d(20_000), USCents(0)).unwrap();
    let v = capped.value();
    assert_eq!(
        (v.preferential_income(), v.ordinary_income(), v.total()),
        (d(10_000), USCents(0), USCents(0))
    );
    assert!(!has(&capped.required(), PositionKind::LotSelection));
}

#[test]
fn tax_on_income_takes_line_24_when_it_is_smaller() {
    let table = us::table_for(2026, SINGLE).unwrap();
    let i = table
        .ordinary
        .iter()
        .position(|b| b.rate == percent(12))
        .unwrap();
    let top12 = table.ordinary[i + 1].over;
    assert!(top12 > table.max_zero_rate_amount);
    let ordinary = table.max_zero_rate_amount + dollars(50);
    let dividends = dollars(500).min(top12 - ordinary);
    let v = us::tax_on_income(2026, SINGLE, ordinary + dividends, dividends, USCents(0))
        .unwrap()
        .value();
    assert_eq!(v.total(), v.all_ordinary());
    assert!(v.all_ordinary() < v.ladder().total + v.gains().total);
}

#[test]
fn aged_65_or_older() {
    for ((y, m, d), want) in [
        ((1961, Month::JANUARY, 1), true),
        ((1961, Month::JANUARY, 2), false),
        ((1960, Month::DECEMBER, 31), true),
        ((1990, Month::JUNE, 1), false),
    ] {
        assert_eq!(us::aged_65_or_older(2025, must_date(y, m, d)), want);
    }
}

fn dist(taxable: USCents, code: &str) -> us::RetirementDistribution {
    us::new_retirement_distribution(taxable, code).unwrap()
}

#[test]
fn early_distribution_tax() {
    let r =
        us::early_distribution_tax(2026, &[dist(D * 8_000, "1"), dist(D * 5_000, "7")]).unwrap();
    assert_eq!(r.value(), D * 800);
    assert!(!us::early_distribution_tax_other(&[dist(USCents(1), "1")]));
    assert!(us::early_distribution_tax_other(&[dist(USCents(1), "S")]));
    assert!(us::new_retirement_distribution(USCents(1), "").is_err());
}

#[test]
fn state_refund_worksheet() {
    let taxable = |b: &us::StateRefundBuilder| {
        us::state_refund_taxable(2026, b.build().unwrap())
            .unwrap()
            .value()
    };
    let base = || {
        let mut b = us::new_state_refund(D * 10_000, SINGLE);
        b.prior_income_tax(D * 6_513)
            .prior_salt(D * 9_000, D * 9_000)
            .prior_itemized(D * 23_000);
        b
    };
    assert_eq!(taxable(&base()), D * 6_513);
    assert_eq!(taxable(base().prior_itemized(D * 17_750)), D * 2_000);
    assert_eq!(
        taxable(base().prior_itemized(D * 17_750).prior_boxes(1)),
        USCents(0)
    );
    assert_eq!(
        taxable(base().prior_salt(D * 12_000, D * 10_000)),
        D * 4_513
    );
    assert_eq!(taxable(base().prior_itemized(USCents(0))), USCents(0));
    let mut sep = us::new_state_refund(D * 10_000, MARRIED_FILING_SEPARATELY);
    sep.prior_income_tax(D * 6_513)
        .prior_itemized(D * 3_000)
        .spouse_itemized_separately();
    assert_eq!(taxable(&sep), D * 3_000);
}

#[test]
fn distribution_codes() {
    let mut ds = vec![
        dist(D * 1_000, "1"),
        dist(D * 2_000, "2"),
        dist(USCents(0), "G"),
        dist(D * 3_000, "P"),
        dist(D * 4_000, "4G"),
        dist(D * 500, "1P"),
    ];
    assert_eq!(us::distribution_income(&ds), D * 7_000);
    assert_eq!(
        us::early_distribution_tax(2026, &ds).unwrap().value(),
        D * 100
    );
    ds.extend([
        dist(USCents(1), "A"),
        dist(USCents(1), "J7"),
        dist(USCents(1), "A"),
    ]);
    assert_eq!(us::unhandled_distribution_codes(&ds), ["A", "J"]);
}

#[test]
fn rental_loss_allowed() {
    for (status, loss, magi, apart, want) in [
        (SINGLE, 30_000, 90_000, false, 25_000),
        (SINGLE, 30_000, 120_000, false, 15_000),
        (SINGLE, 10_000, 120_000, false, 10_000),
        (MARRIED_FILING_JOINTLY, 30_000, 150_000, false, 0),
        (MARRIED_FILING_SEPARATELY, 30_000, 60_000, true, 7_500),
        (MARRIED_FILING_SEPARATELY, 30_000, 20_000, false, 0),
    ] {
        let r = us::rental_loss_allowed(status, D * loss, D * magi, apart).unwrap();
        assert_eq!(r.value(), D * want, "{status} {loss} {magi} {apart}");
    }
}

fn covered(amount: USCents) -> IraContributionBuilder {
    let mut b = us::new_ira_contribution(amount);
    b.active_participant(true).spouse_active_participant(false);
    b
}

fn ira_deduction(
    status: FilingStatus,
    b: &IraContributionBuilder,
    magi: USCents,
) -> working::Result<USCents> {
    us::ira_deduction(2026, status, b.build().unwrap(), magi).unwrap()
}

#[test]
fn ira_deduction_phases_out() {
    let mut spouse = us::new_ira_contribution(D * 7_500);
    spouse
        .active_participant(false)
        .spouse_active_participant(true);
    let mut nobody = us::new_ira_contribution(D * 9_000);
    nobody.active_participant(false);
    let cases: Vec<(&str, FilingStatus, IraContributionBuilder, i64, i64)> = vec![
        (
            "covered, single, $161,359",
            SINGLE,
            covered(D * 7_500),
            161_359,
            0,
        ),
        (
            "covered, single, at the start",
            SINGLE,
            covered(D * 7_500),
            81_000,
            7_500,
        ),
        (
            "covered, single, $1 over",
            SINGLE,
            covered(D * 7_500),
            81_001,
            7_500,
        ),
        (
            "covered, single, $85,000",
            SINGLE,
            covered(D * 7_500),
            85_000,
            4_500,
        ),
        (
            "covered, single, $90,950",
            SINGLE,
            covered(D * 7_500),
            90_950,
            200,
        ),
        (
            "covered, single, at the end",
            SINGLE,
            covered(D * 7_500),
            91_000,
            0,
        ),
        (
            "covered, single, $2,000 put in",
            SINGLE,
            covered(D * 2_000),
            85_000,
            2_000,
        ),
        (
            "covered, 50 or older",
            SINGLE,
            covered(D * 8_600).catch_up().clone(),
            86_000,
            4_300,
        ),
        (
            "covered, joint",
            MARRIED_FILING_JOINTLY,
            covered(D * 7_500),
            139_000,
            3_750,
        ),
        (
            "spouse covered, joint",
            MARRIED_FILING_JOINTLY,
            spouse,
            247_000,
            3_750,
        ),
        (
            "covered, separate",
            MARRIED_FILING_SEPARATELY,
            covered(D * 7_500),
            5_000,
            3_750,
        ),
        (
            "covered, separate, lived apart",
            MARRIED_FILING_SEPARATELY,
            covered(D * 7_500).lived_apart().clone(),
            85_000,
            4_500,
        ),
        ("not covered", SINGLE, nobody, 500_000, 7_500),
    ];
    for (name, status, b, magi, want) in cases {
        assert_eq!(
            ira_deduction(status, &b, D * magi).value(),
            D * want,
            "{name}"
        );
    }
}

#[test]
fn ira_deduction_asks_about_coverage() {
    let r = ira_deduction(
        MARRIED_FILING_JOINTLY,
        &us::new_ira_contribution(D * 7_500),
        D * 200_000,
    );
    assert_eq!(r.value(), D * 7_500);
    assert_eq!(r.required().len(), 2);
    for q in r.required() {
        assert!(
            q.position() == PositionKind::IraDeductibility
                && !q.question().is_empty()
                && !q.effect().is_empty()
        );
    }
    let c = us::new_ira_contribution(USCents(1)).build().unwrap();
    let err = us::ira_deduction(2025, SINGLE, c, USCents(0)).unwrap_err();
    assert!(err.is(&Error::NoTable), "{err}");
    assert!(us::new_ira_contribution(USCents(-1)).build().is_err());
}

fn ira_limits(
    status: FilingStatus,
    b: &IraContributionBuilder,
    magi: USCents,
) -> working::Result<us::IraLimits> {
    us::ira_contribution_limits(2026, status, b.build().unwrap(), magi).unwrap()
}

#[test]
fn roth_limit_phases_out() {
    let roth = || us::new_ira_contribution(USCents(0));
    let cases: Vec<(&str, FilingStatus, IraContributionBuilder, i64, i64)> = vec![
        ("single, at the start", SINGLE, roth(), 153_000, 7_500),
        ("single, halfway", SINGLE, roth(), 160_500, 3_750),
        (
            "head of household, halfway",
            HEAD_OF_HOUSEHOLD,
            roth(),
            160_500,
            3_750,
        ),
        ("single, $154,234", SINGLE, roth(), 154_234, 6_890),
        ("single, $167,900", SINGLE, roth(), 167_900, 200),
        ("single, at the end", SINGLE, roth(), 168_000, 0),
        (
            "single, 50 or older",
            SINGLE,
            roth().catch_up().clone(),
            160_500,
            4_300,
        ),
        (
            "joint, halfway",
            MARRIED_FILING_JOINTLY,
            roth(),
            247_000,
            3_750,
        ),
        (
            "joint, at the end",
            MARRIED_FILING_JOINTLY,
            roth(),
            252_000,
            0,
        ),
        (
            "separate, halfway",
            MARRIED_FILING_SEPARATELY,
            roth(),
            5_000,
            3_750,
        ),
        (
            "separate, lived apart",
            MARRIED_FILING_SEPARATELY,
            roth().lived_apart().clone(),
            160_500,
            3_750,
        ),
        (
            "surviving spouse, halfway",
            SURVIVING_SPOUSE,
            roth(),
            249_500,
            3_750,
        ),
    ];
    for (name, status, b, magi, want) in cases {
        assert_eq!(
            ira_limits(status, &b, D * magi).value().roth_limit(),
            D * want,
            "{name}"
        );
    }
}

#[test]
fn ira_contributions_share_one_limit() {
    let c = |amount: i64, roth: i64, cov: Option<bool>| {
        let mut b = us::new_ira_contribution(D * amount);
        b.roth(D * roth);
        if let Some(v) = cov {
            b.active_participant(v);
        }
        b
    };
    for (name, b, magi, want) in [
        (
            "both, over",
            c(5_000, 5_000, Some(false)),
            100_000,
            [7_500, 2_500, 5_000, 0, 2_500, 150],
        ),
        (
            "both, within",
            c(3_000, 4_500, Some(false)),
            100_000,
            [7_500, 4_500, 3_000, 0, 0, 0],
        ),
        (
            "traditional, over",
            c(8_000, 0, Some(false)),
            100_000,
            [7_500, 0, 7_500, 500, 0, 30],
        ),
        (
            "traditional, not deductible",
            c(7_500, 0, Some(true)),
            100_000,
            [7_500, 0, 0, 0, 0, 0],
        ),
        (
            "Roth, phased out",
            c(0, 5_000, None),
            160_500,
            [7_500, 3_750, 0, 0, 1_250, 75],
        ),
        (
            "both, phased out",
            c(2_000, 4_000, Some(false)),
            160_500,
            [7_500, 3_750, 2_000, 0, 250, 15],
        ),
    ] {
        let l = ira_limits(SINGLE, &b, D * magi).value();
        let got = [
            l.combined(),
            l.roth_allowed(),
            l.deduction(),
            l.traditional_excess(),
            l.roth_excess(),
            l.excess_tax(),
        ];
        assert_eq!(got, want.map(|w| D * w), "{name}");
    }
}

#[test]
fn roth_changes_no_deduction() {
    let r = ira_limits(
        MARRIED_FILING_JOINTLY,
        us::new_ira_contribution(USCents(0)).roth(D * 7_500),
        D * 200_000,
    );
    assert_eq!(r.value().deduction(), USCents(0));
    assert!(r.required().is_empty());
    let with = ira_limits(
        SINGLE,
        us::new_ira_contribution(D * 4_000)
            .roth(D * 3_000)
            .active_participant(true),
        D * 85_000,
    );
    let without = ira_deduction(
        SINGLE,
        us::new_ira_contribution(D * 4_000).active_participant(true),
        D * 85_000,
    );
    assert_eq!(with.value().deduction(), without.value());
    assert!(
        us::new_ira_contribution(USCents(0))
            .roth(USCents(-1))
            .build()
            .is_err()
    );
    let c = us::new_ira_contribution(USCents(0)).build().unwrap();
    let err = us::ira_contribution_limits(2025, SINGLE, c, USCents(0)).unwrap_err();
    assert!(err.is(&Error::NoTable), "{err}");
}

#[test]
fn itemised_limits() {
    let r = |year, itemised, charity, agi| {
        us::itemised_limits(year, SINGLE, D * itemised, D * charity, D * agi)
            .unwrap()
            .value()
    };
    assert_eq!(r(2026, 60_000, 20_000, 800_000), USCents(5_297_298));
    assert_eq!(r(2026, 60_000, 20_000, 300_000), D * 58_500);
    assert_eq!(r(2026, 30_000, 1_000, 300_000), D * 29_000);
    assert_eq!(r(2025, 60_000, 20_000, 800_000), D * 60_000);
    assert!(us::itemised_limits(2026, SINGLE, USCents(1), USCents(2), USCents(0)).is_err());
}

#[test]
fn unknown_year_is_an_error() {
    for year in [2024, 2027, 1998] {
        let err = us::table_for(year, SINGLE).unwrap_err();
        assert!(err.is(&Error::NoTable), "{year}: {err}");
    }
    let err = us::ordinary_income_tax(2024, SINGLE, dollars(50_000)).unwrap_err();
    assert!(err.is(&Error::NoTable));
}

#[test]
fn unspecified_status_is_an_error() {
    assert!(us::table_for(2026, us::FILING_STATUS_UNSPECIFIED).is_err());
}

#[test]
fn brackets_ascend() {
    for year in us::years() {
        for status in filing::Status::ALL {
            let t = us::table_for(year, status).unwrap();
            assert_eq!(t.ordinary[0].over, USCents(0));
            for w in t.ordinary.windows(2) {
                assert!(w[1].over > w[0].over && w[1].rate > w[0].rate);
            }
            assert!(t.max_zero_rate_amount < t.max_15_rate_amount);
        }
    }
    assert_eq!(us::years(), [2025, 2026]);
}

/// example_test.go's Example: one salaried filer from gross pay to tax
/// owed, with its printed output.
#[test]
fn example() {
    let (year, status, salary, itemised) = (2026, SINGLE, dollars(120_000), dollars(9_400));
    let deduction = us::deduction_taken(
        year,
        status,
        Conditions::default(),
        itemised,
        Election::Larger,
    )
    .unwrap();
    let taxable = salary - deduction.value().amount;
    let tax = us::ordinary_income_tax(year, status, taxable).unwrap();
    let mut out = String::new();
    out += &format!("salary         {salary}\n");
    out += &format!(
        "deduction      {} (standard {}, itemised {})\n",
        deduction.value().amount,
        deduction.value().standard,
        itemised
    );
    out += &format!("taxable        {taxable}\n");
    out += &format!("tax            {}\n", tax.value().total);
    out += &format!(
        "marginal rate  {}, with {} of room before the next band\n",
        tax.value().marginal,
        tax.value().room_in_bracket
    );
    out += "\nwhy:\n";
    for r in tax.value().bands {
        if r.income == USCents(0) {
            continue;
        }
        out += &format!("  {} on {} = {}\n", r.rate, r.income, r.tax);
    }
    out += "\nunanswered:\n";
    for r in deduction.required() {
        out += &format!("  {}: {}\n", r.position(), r.because());
    }
    assert_eq!(
        out,
        "salary         $120000.00
deduction      $16100.00 (standard $16100.00, itemised $9400.00)
taxable        $103900.00
tax            $17570.00
marginal rate  22%, with $1800.00 of room before the next band

why:
  10% on $12400.00 = $1240.00
  12% on $38000.00 = $4560.00
  22% on $53500.00 = $11770.00

unanswered:
  deduction method: § 63(e) makes itemising an election rather than an outcome. A taxpayer may itemise a smaller total, and one filing separately must itemise if their spouse does
"
    );
}
