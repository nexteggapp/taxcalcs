//! What payroll reads from the federal taxlisp rules. Ported from the Go
//! file `taxyear/payroll/figures.go`.

use std::cell::{OnceCell, RefCell};
use std::collections::HashMap;
use std::rc::Rc;

use filing::Status;
use money::{Rate, USCents};
use returns::rules::federal;
use taxlisp::eval::Value;

use super::Error;

/// What payroll reads from the federal taxlisp rules for one tax year: the
/// § 1(j) brackets and § 63(c) standard deduction of the three statuses
/// Publication 15-T has tables for, the § 3121(a)(1) wage base, and the
/// § 402(g) limit. The § 414(v) catch-up is asked of the rules by age.
pub(super) struct Figures {
    year: i64,
    engine: Rc<federal::Engine>,
    brackets: HashMap<Status, Vec<Bracket>>,
    standard: HashMap<Status, USCents>,
    pub(super) wage_base: USCents,
    pub(super) deferral_limit: USCents,
    catch_ups: RefCell<HashMap<i64, USCents>>,
}

/// One row of a rate schedule: the rate on income over floor.
struct Bracket {
    floor: USCents,
    rate: Rate,
}

impl Figures {
    /// The standard deduction for a status Publication 15-T has a table for.
    pub(super) fn standard(&self, status: Status) -> USCents {
        self.standard.get(&status).copied().unwrap_or_default()
    }

    /// The rate schedule's tax on taxable income, each band's tax rounded
    /// to the cent. Less than nothing is nothing.
    pub(super) fn tax(&self, status: Status, taxable: USCents) -> USCents {
        let taxable = USCents(0).max(taxable);
        let Some(rows) = self.brackets.get(&status) else {
            return USCents(0);
        };
        let mut total = USCents(0);
        for (i, b) in rows.iter().enumerate() {
            let mut top = taxable;
            if let Some(next) = rows.get(i + 1) {
                top = top.min(next.floor);
            }
            if top > b.floor {
                total += b.rate.of(top - b.floor);
            }
        }
        total
    }

    /// The rules' plan-catch-up for a person of age at the end of the year.
    pub(super) fn catch_up(&self, age: i64) -> Result<USCents, Error> {
        if let Some(v) = self.catch_ups.borrow().get(&age) {
            return Ok(*v);
        }
        let v = self
            .engine
            .evaluator
            .call(
                "plan-catch-up",
                vec![Value::Int(self.year), Value::Int(age)],
            )
            .map_err(|e| Error::Rules(format!("payroll: plan-catch-up: {e}")))?;
        let Value::Money(m) = v else {
            return Err(Error::Rules(format!(
                "payroll: plan-catch-up gave {}",
                v.go_type()
            )));
        };
        let c = USCents(m.cents());
        self.catch_ups.borrow_mut().insert(age, c);
        Ok(c)
    }
}

/// Every year's figures, by year.
type ByYear = HashMap<i64, Rc<Figures>>;

thread_local! {
    static BY_YEAR: OnceCell<Result<Rc<ByYear>, Error>> = const { OnceCell::new() };
}

/// The year's figures, or [`Error::NoYear`]. They are loaded once per
/// thread, as the rules engine is.
pub(super) fn figures_for(year: i64) -> Result<Rc<Figures>, Error> {
    let by_year = BY_YEAR.with(|cell| cell.get_or_init(|| load().map(Rc::new)).clone())?;
    by_year
        .get(&year)
        .cloned()
        .ok_or_else(|| Error::NoYear(format!("{}: {year}", Error::NO_YEAR)))
}

/// Reads every year in the rules' payroll-tax-years.
fn load() -> Result<ByYear, Error> {
    let engine = federal::default_engine()
        .map_err(|e| Error::Rules(format!("payroll: loading the federal rules: {e}")))?;
    let ev = &engine.evaluator;
    let Some(v) = ev.globals.get(ev.intern("payroll-tax-years")) else {
        return Err(Error::Rules(
            "payroll: the federal rules have no payroll-tax-years".to_string(),
        ));
    };
    let Value::Slice(years) = v else {
        return Err(Error::Rules(format!(
            "payroll: payroll-tax-years is {}",
            v.go_type()
        )));
    };
    let mut out = HashMap::new();
    for y in years.elems.iter() {
        let Value::Int(year) = y else {
            return Err(Error::Rules(format!(
                "payroll: payroll-tax-years holds {}",
                y.go_type()
            )));
        };
        let f = load_year(&engine, *year)?;
        out.insert(f.year, Rc::new(f));
    }
    Ok(out)
}

/// The rules' names for the statuses Publication 15-T has tables for.
const STATUSES: [(&str, Status); 3] = [
    ("FILING_STATUS_SINGLE", Status::Single),
    (
        "FILING_STATUS_MARRIED_FILING_JOINTLY",
        Status::MarriedFilingJointly,
    ),
    ("FILING_STATUS_HEAD_OF_HOUSEHOLD", Status::HeadOfHousehold),
];

fn load_year(engine: &Rc<federal::Engine>, year: i64) -> Result<Figures, Error> {
    let ev = &engine.evaluator;
    let y = || vec![Value::Int(year)];
    let mut f = Figures {
        year,
        engine: Rc::clone(engine),
        brackets: HashMap::new(),
        standard: HashMap::new(),
        wage_base: USCents(0),
        deferral_limit: USCents(0),
        catch_ups: RefCell::new(HashMap::new()),
    };

    let params = ev
        .call("year-params", y())
        .map_err(|e| Error::Rules(format!("payroll: year-params {year}: {e}")))?;
    let Value::Struct(p) = params else {
        return Err(Error::Rules(format!(
            "payroll: year-params {year} gave {}",
            params.go_type()
        )));
    };
    let amount = |field: &str| -> Result<USCents, Error> {
        match p.fields.get(field) {
            Some(Value::Money(m)) => Ok(USCents(m.cents())),
            _ => Err(Error::Rules(format!(
                "payroll: year-params {year} has no {field}"
            ))),
        }
    };
    for (field, status) in [
        ("std_ded_single", Status::Single),
        ("std_ded_mfj", Status::MarriedFilingJointly),
        ("std_ded_hoh", Status::HeadOfHousehold),
    ] {
        f.standard.insert(status, amount(field)?);
    }
    f.wage_base = amount("oasdi_wage_base")?;

    let table = ev
        .call("bracket-table-for-year", y())
        .map_err(|e| Error::Rules(format!("payroll: bracket-table-for-year {year}: {e}")))?;
    let Value::Table(t) = table else {
        return Err(Error::Rules(format!(
            "payroll: bracket-table-for-year {year} gave {}",
            table.go_type()
        )));
    };
    for e in t.entries.iter() {
        let name: &str = match &e.key {
            Value::Symbol(k) => &k.name,
            Value::ProtoEnum(k) => k.name(),
            k => {
                return Err(Error::Rules(format!(
                    "payroll: a {year} bracket table key is {}",
                    k.go_type()
                )));
            }
        };
        let Some(&(_, status)) = STATUSES.iter().find(|(n, _)| *n == name) else {
            continue;
        };
        let Value::Slice(rows) = &e.value else {
            return Err(Error::Rules(format!(
                "payroll: the {year} {name} brackets are {}",
                e.value.go_type()
            )));
        };
        for r in rows.elems.iter() {
            let Value::Struct(s) = r else {
                return Err(Error::Rules(format!(
                    "payroll: a {year} {name} bracket is {}",
                    r.go_type()
                )));
            };
            let (Some(Value::Money(floor)), Some(Value::Float(rate))) =
                (s.fields.get("floor"), s.fields.get("rate"))
            else {
                return Err(Error::Rules(format!(
                    "payroll: a {year} {name} bracket has no floor or rate"
                )));
            };
            f.brackets.entry(status).or_default().push(Bracket {
                floor: USCents(floor.cents()),
                rate: Rate((rate * 10_000.0).round() as i32),
            });
        }
    }
    for (_, s) in STATUSES {
        if f.brackets.get(&s).is_none_or(Vec::is_empty) {
            return Err(Error::Rules(format!(
                "payroll: the rules have no {year} brackets for {s}"
            )));
        }
    }

    let limit = ev
        .call("plan-elective-deferral-limit", y())
        .map_err(|e| Error::Rules(format!("payroll: plan-elective-deferral-limit {year}: {e}")))?;
    let Value::Money(m) = limit else {
        return Err(Error::Rules(format!(
            "payroll: plan-elective-deferral-limit {year} gave {}",
            limit.go_type()
        )));
    };
    f.deferral_limit = USCents(m.cents());
    Ok(f)
}
