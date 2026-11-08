//! Loads, typechecks, and evaluates the taxlisp rule modules for U.S.
//! federal individual income tax returns (Form 1040 and schedules), ported
//! from the Go package `returns/rules/federal`.

use std::cell::OnceCell;
use std::rc::Rc;

use proto::returnsv1;
use proto::taxlispv1;
use taxlisp::eval::{self, Evaluator, ProtoMessageVal, Value};
use taxlisp::{parser, types};

/// The rule files, which Go embeds with `go:embed *.tlisp`, by name.
const RULE_FILES: &[(&str, &str)] = &[
    (
        "01_params.tlisp",
        include_str!("../../../../returns/rules/federal/01_params.tlisp"),
    ),
    (
        "02_household.tlisp",
        include_str!("../../../../returns/rules/federal/02_household.tlisp"),
    ),
    (
        "03_income_wages_schedule_b.tlisp",
        include_str!("../../../../returns/rules/federal/03_income_wages_schedule_b.tlisp"),
    ),
    (
        "04_capital_gains.tlisp",
        include_str!("../../../../returns/rules/federal/04_capital_gains.tlisp"),
    ),
    (
        "05_retirement_and_ss.tlisp",
        include_str!("../../../../returns/rules/federal/05_retirement_and_ss.tlisp"),
    ),
    (
        "05a_depreciation_and_dispositions.tlisp",
        include_str!("../../../../returns/rules/federal/05a_depreciation_and_dispositions.tlisp"),
    ),
    (
        "05b_schedule_c.tlisp",
        include_str!("../../../../returns/rules/federal/05b_schedule_c.tlisp"),
    ),
    (
        "05c_schedule_e.tlisp",
        include_str!("../../../../returns/rules/federal/05c_schedule_e.tlisp"),
    ),
    (
        "05d_schedule_se_and_qbi.tlisp",
        include_str!("../../../../returns/rules/federal/05d_schedule_se_and_qbi.tlisp"),
    ),
    (
        "05e_pal_basis_and_inv_interest.tlisp",
        include_str!("../../../../returns/rules/federal/05e_pal_basis_and_inv_interest.tlisp"),
    ),
    (
        "06_adjustments_and_hsa.tlisp",
        include_str!("../../../../returns/rules/federal/06_adjustments_and_hsa.tlisp"),
    ),
    (
        "07_deductions_and_schedule_1a.tlisp",
        include_str!("../../../../returns/rules/federal/07_deductions_and_schedule_1a.tlisp"),
    ),
    (
        "08_tax_and_credits.tlisp",
        include_str!("../../../../returns/rules/federal/08_tax_and_credits.tlisp"),
    ),
    (
        "08b_credits_and_amt.tlisp",
        include_str!("../../../../returns/rules/federal/08b_credits_and_amt.tlisp"),
    ),
    (
        "08c_form_8606_nol_and_amendment.tlisp",
        include_str!("../../../../returns/rules/federal/08c_form_8606_nol_and_amendment.tlisp"),
    ),
    (
        "08d_international_and_specialized.tlisp",
        include_str!("../../../../returns/rules/federal/08d_international_and_specialized.tlisp"),
    ),
    (
        "09_payments_and_1040.tlisp",
        include_str!("../../../../returns/rules/federal/09_payments_and_1040.tlisp"),
    ),
];

/// An error loading the rules or preparing a return; its text is the Go
/// error's.
#[derive(Debug, Clone, PartialEq, Eq, thiserror::Error)]
#[error("{0}")]
pub struct Error(pub String);

/// Engine holds a parsed, statically typechecked, and initialized taxlisp
/// evaluator for the federal Form 1040 rules.
pub struct Engine {
    /// The rule files' forms, combined into one module.
    pub module: taxlispv1::ModuleFile,
    /// The typechecker the module passed.
    pub checker: types::Checker,
    /// The evaluator holding the module's definitions.
    pub evaluator: Evaluator,
}

thread_local! {
    static DEFAULT_ENGINE: OnceCell<Result<Rc<Engine>, Error>> = const { OnceCell::new() };
}

/// DefaultEngine returns a shared, pre-typechecked federal taxlisp Engine,
/// loaded once per thread (an evaluator's values are not shared across
/// threads; under wasm there is one).
pub fn default_engine() -> Result<Rc<Engine>, Error> {
    DEFAULT_ENGINE.with(|cell| cell.get_or_init(|| load_engine().map(Rc::new)).clone())
}

/// LoadEngine parses all embedded .tlisp files in lexical order, then the
/// state rules from [`super::states`], statically
/// typechecks the combined module against the protobuf descriptor pool, and
/// initializes the runtime evaluator.
pub fn load_engine() -> Result<Engine, Error> {
    let mut names: Vec<&(&str, &str)> = RULE_FILES.iter().collect();
    names.sort_by(|a, b| a.0.cmp(b.0));

    let mut combined = taxlispv1::ModuleFile::default();
    for (name, raw) in names {
        let module =
            parser::parse_file(name, raw).map_err(|e| Error(format!("parsing {name}: {e}")))?;
        combined.expressions.extend(module.expressions);
    }
    // The state rules read the prepared federal return, and prepare-return
    // calls them, so they are part of the same module.
    for (name, raw) in super::states::files() {
        let module = parser::parse_file(&format!("states/{name}"), raw)
            .map_err(|e| Error(format!("parsing states/{name}: {e}")))?;
        combined.expressions.extend(module.expressions);
    }

    let files = types::global_files().map_err(|e| Error(e.to_string()))?;
    let mut checker = types::Checker::new(files);
    checker
        .check_module(&combined)
        .map_err(|e| Error(format!("typechecking federal rules: {e}")))?;

    let pool = eval::default_pool().map_err(|e| Error(e.to_string()))?;
    let evaluator = Evaluator::new(Some(pool)).map_err(|e| Error(e.to_string()))?;
    evaluator
        .eval_module(&combined)
        .map_err(|e| Error(format!("initializing federal rules: {e}")))?;

    Ok(Engine {
        module: combined,
        checker,
        evaluator,
    })
}

impl Engine {
    /// Calls `fn_name` on the inputs and returns the protobuf message it
    /// builds.
    fn call_on_inputs(
        &self,
        fn_name: &str,
        inputs: &returnsv1::ReturnInputs,
    ) -> Result<ProtoMessageVal, Error> {
        let arg = ProtoMessageVal::from_message(self.evaluator.pool, inputs)
            .map_err(|e| Error(e.to_string()))?;
        let val = self
            .evaluator
            .call(fn_name, vec![Value::ProtoMessage(arg)])
            .map_err(|e| Error(e.to_string()))?;
        match val {
            Value::ProtoMessage(pm) => Ok(pm),
            other => Err(Error(format!(
                "{fn_name} returned {}, want ProtoMessageVal",
                other.go_type()
            ))),
        }
    }

    /// Prepare evaluates `prepare-return` on the given ReturnInputs and
    /// returns the populated ReturnOutputs message.
    pub fn prepare(
        &self,
        inputs: &returnsv1::ReturnInputs,
    ) -> Result<returnsv1::ReturnOutputs, Error> {
        let pm = self.call_on_inputs("prepare-return", inputs)?;
        pm.to_message()
            .map_err(|e| Error(format!("unmarshaling ReturnOutputs: {e}")))
    }

    /// PrepareFederal evaluates `prepare-federal-return` on the given
    /// ReturnInputs and returns the populated FederalReturn message.
    pub fn prepare_federal(
        &self,
        inputs: &returnsv1::ReturnInputs,
    ) -> Result<returnsv1::FederalReturn, Error> {
        let pm = self.call_on_inputs("prepare-federal-return", inputs)?;
        pm.to_message()
            .map_err(|e| Error(format!("unmarshaling FederalReturn: {e}")))
    }
}
