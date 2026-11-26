//! The employer vectors in golden/testdata/employer/employer.json.gz, which
//! golden/employer_test.go writes: every taxyear test case's jobs driven as
//! taxyear drives them, at the case's as_of and a few others.

use std::collections::HashSet;
use std::io::Read as _;
use std::sync::Arc;

use proto::{commonv1, factsv1, scenariosv1};
use serde::Deserialize;
use serde::de::DeserializeOwned;
use taxcalcs_taxyear::internal::civil;
use taxcalcs_taxyear::model::{Context, Notes, employer, retirement};

#[derive(Deserialize)]
struct Scenario {
    cases: Vec<String>,
    year: i64,
    as_of: String,
    facts: String,
    employers: Vec<String>,
    retirement: Vec<String>,
    overrides: Vec<String>,
    error: String,
    pay_dates: Vec<PayDates>,
    expand: Vec<Expand>,
    max_out: Vec<MaxOut>,
    hold_hsa: Vec<HoldHsa>,
    jobs: Vec<JobVector>,
}

#[derive(Deserialize)]
struct PayDates {
    id: String,
    error: String,
    dates: Vec<[i64; 3]>,
}

#[derive(Deserialize)]
struct Expand {
    id: String,
    person: String,
    error: String,
}

#[derive(Deserialize)]
struct MaxOut {
    retirement: String,
    person: String,
    part: String,
    error: String,
    questions: Vec<String>,
    left_out: Vec<String>,
}

#[derive(Deserialize)]
struct HoldHsa {
    person: String,
    limit: i64,
    ok: bool,
    questions: Vec<String>,
    left_out: Vec<String>,
    error: String,
}

#[derive(Deserialize)]
struct JobVector {
    id: String,
    projection: String,
    error: String,
    questions: Vec<String>,
    left_out: Vec<String>,
    working: Vec<String>,
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

fn check_notes(at: &str, n: &Notes, questions: &[String], left_out: &[String]) {
    let got: Vec<scenariosv1::Question> = n.questions().iter().map(|q| q.proto()).collect();
    let want: Vec<scenariosv1::Question> = questions.iter().map(|q| golden::decode(q)).collect();
    golden::assert_same(&format!("{at}: questions"), &got, &want);
    golden::assert_same(
        &format!("{at}: left out"),
        &n.left_out(),
        &left_out.to_vec(),
    );
}

#[test]
fn employer() {
    let scenarios: Vec<Scenario> = read_gzip("employer/employer.json.gz");
    assert!(!scenarios.is_empty());
    let mut projections = 0;
    for s in &scenarios {
        let name = match s.cases.len() {
            1 => s.cases[0].clone(),
            n => format!("{} and {} more", s.cases[0], n - 1),
        };
        let as_of: commonv1::Date = golden::decode(&s.as_of);
        let facts: factsv1::TaxYearFacts = golden::decode(&s.facts);
        let ctx = match Context::new(s.year, Some(&as_of), Some(Arc::new(facts))) {
            Ok(c) => c,
            Err(e) => {
                assert_eq!(e.to_string(), s.error, "{name}");
                continue;
            }
        };
        assert_eq!("", s.error, "{name}");
        let employers: Vec<scenariosv1::EmployerModel> =
            s.employers.iter().map(|e| golden::decode(e)).collect();
        let retirements: Vec<scenariosv1::RetirementModel> =
            s.retirement.iter().map(|e| golden::decode(e)).collect();
        let overrides: Vec<scenariosv1::Override> =
            s.overrides.iter().map(|e| golden::decode(e)).collect();

        assert_eq!(employers.len(), s.pay_dates.len(), "{name}: pay dates");
        for (e, v) in employers.iter().zip(&s.pay_dates) {
            assert_eq!(e.id, v.id, "{name}");
            let at = format!("{name}: pay dates of {}", e.id);
            match (
                civil::from_field(&e.first_pay_date),
                civil::from_field(&e.starts),
            ) {
                (Err(err), _) | (_, Err(err)) => assert_eq!(err.to_string(), v.error, "{at}"),
                (Ok(first), Ok(starts)) => {
                    assert_eq!("", v.error, "{at}");
                    let got: Vec<[i64; 3]> =
                        employer::pay_dates(ctx.year(), e.pay_frequency, first, starts)
                            .iter()
                            .map(|d| [d.year(), d.month(), d.day()])
                            .collect();
                    assert_eq!(got, v.dates, "{at}");
                }
            }
        }

        assert_eq!(employers.len(), s.expand.len(), "{name}: expand");
        let mut jobs = Vec::new();
        let mut failed = false;
        for (e, v) in employers.iter().zip(&s.expand) {
            assert_eq!(
                (e.id.as_str(), e.person.as_str()),
                (v.id.as_str(), v.person.as_str())
            );
            match employer::expand(&ctx, e, &overrides) {
                Ok(j) => {
                    assert_eq!("", v.error, "{name}: expand {}", e.id);
                    jobs.push(j);
                }
                Err(err) => {
                    assert_eq!(err.to_string(), v.error, "{name}: expand {}", e.id);
                    failed = true;
                }
            }
        }
        if failed {
            assert!(s.max_out.is_empty() && s.hold_hsa.is_empty() && s.jobs.is_empty());
            continue;
        }

        let mut want_max = s.max_out.iter();
        let mut stopped = false;
        for r in &retirements {
            let Some(part) = retirement::plan_part(r) else {
                continue;
            };
            let v = want_max
                .next()
                .unwrap_or_else(|| panic!("{name}: Go ran no MaxOut for {}", r.id));
            assert_eq!(
                (&r.id, &r.person, &part.id),
                (&v.retirement, &v.person, &v.part),
                "{name}"
            );
            let at = format!("{name}: max out {}", r.id);
            match employer::max_out(&ctx, &r.person, part, &mut jobs) {
                Ok(n) => {
                    assert_eq!("", v.error, "{at}");
                    check_notes(&at, &n, &v.questions, &v.left_out);
                }
                Err(err) => {
                    assert_eq!(err.to_string(), v.error, "{at}");
                    stopped = true;
                    break;
                }
            }
        }
        assert!(want_max.next().is_none(), "{name}: Go ran more MaxOuts");
        if stopped {
            continue;
        }

        let mut seen = HashSet::new();
        let mut want_hold = s.hold_hsa.iter();
        for e in &employers {
            if !seen.insert(e.person.clone()) {
                continue;
            }
            let v = want_hold
                .next()
                .unwrap_or_else(|| panic!("{name}: Go held no HSA for {}", e.person));
            assert_eq!(e.person, v.person, "{name}");
            let at = format!("{name}: hold HSA of {}", e.person);
            let mut n = Notes::default();
            let (limit, ok) = retirement::hsa_limit(&ctx, &e.person, &mut n);
            assert_eq!((limit.0, ok), (v.limit, v.ok), "{at}");
            let mut err = String::new();
            if ok && let Err(e) = employer::hold_hsa(&e.person, limit, &mut jobs) {
                err = e.to_string();
            }
            assert_eq!(err, v.error, "{at}");
            check_notes(&at, &n, &v.questions, &v.left_out);
            if !err.is_empty() {
                stopped = true;
                break;
            }
        }
        if stopped {
            continue;
        }
        assert!(want_hold.next().is_none(), "{name}: Go held more HSAs");

        let mut want_jobs = s.jobs.iter();
        for j in &mut jobs {
            let v = want_jobs
                .next()
                .unwrap_or_else(|| panic!("{name}: Go projected no {}", j.id()));
            assert_eq!(j.id(), v.id, "{name}");
            let at = format!("{name}: projection of {}", v.id);
            match j.projection() {
                Ok(p) => {
                    assert_eq!("", v.error, "{at}");
                    golden::assert_same(
                        &at,
                        &p,
                        &golden::decode::<scenariosv1::EmployerProjection>(&v.projection),
                    );
                    check_notes(&at, j.notes(), &v.questions, &v.left_out);
                    let want: Vec<scenariosv1::Working> =
                        v.working.iter().map(|w| golden::decode(w)).collect();
                    golden::assert_same(&format!("{at}: working"), &j.workings().to_vec(), &want);
                    projections += 1;
                }
                Err(err) => {
                    assert_eq!(err.to_string(), v.error, "{at}");
                    stopped = true;
                    break;
                }
            }
        }
        if stopped {
            continue;
        }
        if let Some(j) = jobs.first_mut() {
            let v = want_jobs
                .next()
                .unwrap_or_else(|| panic!("{name}: no second projection"));
            let err = j
                .projection()
                .err()
                .map(|e| e.to_string())
                .unwrap_or_default();
            assert_eq!(err, v.error, "{name}: second projection");
        }
        assert!(want_jobs.next().is_none(), "{name}: Go projected more jobs");
    }
    assert!(projections > 100, "only {projections} projections");
}
