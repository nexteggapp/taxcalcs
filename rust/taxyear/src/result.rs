//! A computed tax year: result.go.

use std::collections::HashMap;
use std::sync::Arc;

use buffa_descriptor::{FieldDescriptor, Value};
use money::USCents;
use proto::scenariosv1::{self, Problem, ScenarioInputs, ScenarioResult, Working};

use crate::path::{Path, lookup};
use crate::reflect::{self, MONEY};

/// TaxYearResult is a computed tax year, Go's Result. It is immutable:
/// [`TaxYearResult::proto`] returns a copy.
#[derive(Clone, Debug)]
pub struct TaxYearResult {
    p: Arc<scenariosv1::TaxYearResult>,
    inputs: HashMap<String, Arc<ScenarioInputs>>,
}

impl TaxYearResult {
    pub(crate) fn new(
        p: scenariosv1::TaxYearResult,
        inputs: HashMap<String, Arc<ScenarioInputs>>,
    ) -> Self {
        TaxYearResult {
            p: Arc::new(p),
            inputs,
        }
    }

    /// Proto returns the result as the TaxYearResult message, a copy.
    pub fn proto(&self) -> scenariosv1::TaxYearResult {
        (*self.p).clone()
    }

    /// Message returns the TaxYearResult message itself, not a copy, for a
    /// caller that only reads it.
    pub fn message(&self) -> &scenariosv1::TaxYearResult {
        &self.p
    }

    /// Inputs returns a scenario's resolved inputs, and None for a scenario
    /// that didn't resolve.
    pub fn inputs(&self, id: &str) -> Option<&ScenarioInputs> {
        self.inputs.get(id).map(|i| &**i)
    }

    /// Problems returns the file's problems; when there are any, no
    /// scenario was computed.
    pub fn problems(&self) -> Vec<Problem> {
        self.p.problems.clone()
    }

    /// Scenario returns one scenario's result by id.
    pub fn scenario(&self, id: &str) -> Option<Scenario<'_>> {
        self.p
            .scenarios
            .iter()
            .find(|s| s.scenario == id)
            .map(|p| Scenario { p })
    }

    /// Scenarios returns every scenario's result, in the file's order.
    pub fn scenarios(&self) -> Vec<Scenario<'_>> {
        self.p.scenarios.iter().map(|p| Scenario { p }).collect()
    }
}

/// Scenario is one scenario's computed figures.
#[derive(Clone, Copy, Debug)]
pub struct Scenario<'a> {
    p: &'a ScenarioResult,
}

impl Scenario<'_> {
    /// ID is the scenario's id.
    pub fn id(&self) -> &str {
        &self.p.scenario
    }

    /// Proto returns the scenario's result, a copy.
    pub fn proto(&self) -> ScenarioResult {
        self.p.clone()
    }

    /// Figure returns the figure a path names in the scenario's result, and
    /// None when the path names nothing.
    pub fn figure(&self, p: &Path) -> Option<Figure> {
        let d = reflect::to_dyn(self.p)?;
        let (value, fd) = lookup(&d, p)?;
        let mut f = Figure {
            path: p.clone(),
            value,
            field: fd,
            amount: USCents(0),
            is_amount: false,
            working: None,
        };
        if reflect::is_message(fd)
            && let Value::Message(m) = &f.value
            && reflect::full_name(m) == MONEY
        {
            f.amount = USCents(reflect::minor(m));
            f.is_amount = true;
        }
        let text = p.to_string();
        for w in &self.p.working {
            if w.path == text {
                f.working = Some(w.clone());
            }
        }
        Some(f)
    }
}

/// Figure is one value in a scenario's result, and its working when the
/// result holds one.
#[derive(Clone, Debug)]
pub struct Figure {
    path: Path,
    value: Value,
    field: &'static FieldDescriptor,
    amount: USCents,
    is_amount: bool,
    working: Option<Working>,
}

impl Figure {
    /// Path is the figure's path.
    pub fn path(&self) -> &Path {
        &self.path
    }

    /// Amount returns the figure's amount, and None when it isn't money.
    pub fn amount(&self) -> Option<USCents> {
        self.is_amount.then_some(self.amount)
    }

    /// Working returns how the figure was made, or None.
    pub fn working(&self) -> Option<&Working> {
        self.working.as_ref()
    }

    /// Value returns the figure as a reflected value, for a figure that
    /// isn't money.
    pub fn value(&self) -> &Value {
        &self.value
    }

    /// The field the figure is.
    pub fn field(&self) -> &FieldDescriptor {
        self.field
    }
}
