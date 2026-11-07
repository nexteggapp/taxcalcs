//! The payroll vectors in golden/testdata/employer/payroll.json.gz, which
//! golden/employer_test.go writes: the payroll functions over a sweep of
//! years, W-4s, wages, pay periods and ages.

use std::collections::BTreeMap;
use std::io::Read as _;

use filing::Status;
use money::USCents;
use serde::Deserialize;
use serde::de::DeserializeOwned;
use taxcalcs_taxyear::payroll::{self, W4};

#[derive(Deserialize)]
struct Vectors {
    withholding: Vec<Withholding>,
    w4_errors: Vec<W4Error>,
    supplemental: Vec<[i64; 3]>,
    social_security: Vec<Social>,
    wage_bases: Vec<WageBase>,
    medicare: Vec<[i64; 3]>,
    plan_limits: Vec<PlanLimit>,
    no_wage_tax: BTreeMap<String, bool>,
}

#[derive(Deserialize)]
struct W4Spec {
    status: i32,
    multiple_jobs: bool,
    dependents: i64,
    other_income: i64,
    deductions: i64,
    extra: i64,
    exempt: bool,
}

impl W4Spec {
    fn build(&self) -> Result<W4, ustaxcalc::Error> {
        let mut b = payroll::new_w4(Status::from_i32(self.status));
        b.dependents(USCents(self.dependents))
            .other_income(USCents(self.other_income))
            .deductions(USCents(self.deductions))
            .extra(USCents(self.extra));
        if self.multiple_jobs {
            b.multiple_jobs();
        }
        if self.exempt {
            b.exempt();
        }
        b.build()
    }
}

#[derive(Deserialize)]
struct Withholding {
    year: i64,
    w4: W4Spec,
    taxable: i64,
    periods: i64,
    withheld: i64,
    cite: String,
    applied: String,
    error: String,
}

#[derive(Deserialize)]
struct W4Error {
    w4: W4Spec,
    error: String,
}

#[derive(Deserialize)]
struct Social {
    year: i64,
    wages: i64,
    before: i64,
    tax: i64,
    error: String,
}

#[derive(Deserialize)]
struct WageBase {
    year: i64,
    base: i64,
    error: String,
}

#[derive(Deserialize)]
struct PlanLimit {
    year: i64,
    age: i64,
    base: i64,
    catch_up: i64,
    error: String,
}

fn read_gzip<T: DeserializeOwned>(name: &str) -> T {
    let p = golden::path(name);
    let f = std::fs::File::open(&p).unwrap_or_else(|e| panic!("reading {}: {e}", p.display()));
    let mut text = String::new();
    flate2::read::GzDecoder::new(f)
        .read_to_string(&mut text)
        .unwrap_or_else(|e| panic!("decompressing {}: {e}", p.display()));
    serde_json::from_str(&text).unwrap_or_else(|e| panic!("parsing {}: {e}", p.display()))
}

fn err_text<T, E: std::fmt::Display>(r: &Result<T, E>) -> String {
    match r {
        Ok(_) => String::new(),
        Err(e) => e.to_string(),
    }
}

#[test]
fn payroll() {
    let v: Vectors = read_gzip("employer/payroll.json.gz");
    assert!(v.withholding.len() > 1000);
    for c in &v.withholding {
        let at = format!(
            "{} status {} step2 {} exempt {} {} x {}",
            c.year, c.w4.status, c.w4.multiple_jobs, c.w4.exempt, c.taxable, c.periods
        );
        let w = c.w4.build().unwrap_or_else(|e| panic!("{at}: {e}"));
        let got = payroll::federal_withholding(c.year, &w, USCents(c.taxable), c.periods);
        assert_eq!(err_text(&got), c.error, "{at}");
        let (withheld, cite, applied) = match &got {
            Ok((v, r)) => (v.0, r.cite(), r.applied()),
            Err(_) => (0, String::new(), String::new()),
        };
        assert_eq!(
            (withheld, cite.as_str(), applied.as_str()),
            (c.withheld, c.cite.as_str(), c.applied.as_str()),
            "{at}"
        );
    }
    for c in &v.w4_errors {
        // Go's filing.Status is an int, and a W-4 can hold one past the
        // last status; Rust's Status can't.
        if !(0..=5).contains(&c.w4.status) {
            continue;
        }
        assert_eq!(
            err_text(&c.w4.build()),
            c.error,
            "W-4 status {}",
            c.w4.status
        );
    }
    for [a, b, want] in &v.supplemental {
        assert_eq!(
            payroll::flat_supplemental(USCents(*a), USCents(*b)).0,
            *want,
            "supplemental {a} after {b}"
        );
    }
    for [a, b, want] in &v.medicare {
        assert_eq!(
            payroll::medicare(USCents(*a), USCents(*b)).0,
            *want,
            "Medicare {a} after {b}"
        );
    }
    for c in &v.social_security {
        let got = payroll::social_security(c.year, USCents(c.wages), USCents(c.before));
        assert_eq!(
            err_text(&got),
            c.error,
            "{} {} after {}",
            c.year,
            c.wages,
            c.before
        );
        assert_eq!(
            got.map(|t| t.0).unwrap_or(0),
            c.tax,
            "{} {} after {}",
            c.year,
            c.wages,
            c.before
        );
    }
    for c in &v.wage_bases {
        let got = payroll::wage_base(c.year);
        assert_eq!(err_text(&got), c.error, "{}", c.year);
        assert_eq!(got.map(|t| t.0).unwrap_or(0), c.base, "{}", c.year);
    }
    for c in &v.plan_limits {
        let got = payroll::plan_limit(c.year, c.age);
        assert_eq!(err_text(&got), c.error, "{} age {}", c.year, c.age);
        if let Err(e) = &got {
            assert!(e.is(&ustaxcalc::Error::NoTable), "{} age {}", c.year, c.age);
        }
        let (b, k) = got.unwrap_or_default();
        assert_eq!((b.0, k.0), (c.base, c.catch_up), "{} age {}", c.year, c.age);
    }
    for (s, want) in &v.no_wage_tax {
        assert_eq!(payroll::no_wage_tax(s), *want, "{s}");
    }
    for (f, n) in [
        (payroll::Frequency::Unspecified, 0),
        (payroll::Frequency::Weekly, 52),
        (payroll::Frequency::Biweekly, 26),
        (payroll::Frequency::SemiMonthly, 24),
        (payroll::Frequency::Monthly, 12),
    ] {
        assert_eq!(f.periods(), n);
    }
}
