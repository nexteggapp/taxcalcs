//! The engine's working for the return's lines, and its diagnostics,
//! ported from Go's `taxyear/federal/working.go`.

use std::sync::Arc;

use buffa::MessageField;
use buffa_descriptor::reflect::{ReflectMessage, Value as DynValue};
use buffa_descriptor::{DynamicMessage, FieldKind, SingularKind};
use proto::returnsv1;
use proto::scenariosv1;

use super::Return;
use crate::model;

/// formsPrefix is where the engine's working cells for the return's forms
/// start.
const FORMS_PREFIX: &str = "federal.forms.";

/// The full name of the Money message a line holds.
const MONEY: &str = "nexteggapp.taxprotos.common.v1.Money";

/// working is the engine's working cells for the return's lines, keyed by
/// the line's path in the result, federal.<form>.<line>. A cell is kept for
/// a line with an amount other than zero; its formula and citations become
/// the line's rule.
pub(super) fn working(out: &returnsv1::ReturnOutputs) -> Vec<scenariosv1::Working> {
    // An unset return, as Go's invalid message, has no lines set.
    let forms = out.federal.forms.as_option().and_then(|f| {
        let pool = taxlisp::eval::default_pool().ok()?;
        DynamicMessage::try_from_message(f, Arc::clone(pool)).ok()
    });
    let mut list = Vec::new();
    for c in &out.working.cells {
        let Some(rest) = c.path.strip_prefix(FORMS_PREFIX) else {
            continue;
        };
        if !forms.as_ref().is_some_and(|m| non_zero(m, rest)) {
            continue;
        }
        let path = format!("federal.{rest}");
        let cites: Vec<&str> = c.citations.iter().map(|ci| ci.authority.as_str()).collect();
        list.push(scenariosv1::Working {
            path: path.clone(),
            rules: vec![scenariosv1::AppliedRule {
                cite: cites.join("; "),
                applied: MessageField::some(scenariosv1::Statement {
                    text: c.formula.clone(),
                    states: vec![path],
                    ..Default::default()
                }),
                ..Default::default()
            }],
            ..Default::default()
        });
    }
    list
}

/// nonZero reports whether the money field at a dotted path in m is set and
/// not zero.
fn non_zero(m: &DynamicMessage, path: &str) -> bool {
    let names: Vec<&str> = path.split('.').collect();
    let mut m = m;
    for (i, n) in names.iter().enumerate() {
        let md = m.message_descriptor();
        let Some(fd) = md.field_by_name(n) else {
            return false;
        };
        if !matches!(fd.kind(), FieldKind::Singular(SingularKind::Message(_))) || !m.has(fd) {
            return false;
        }
        let Some(DynValue::Message(v)) = m.field_by_number(fd.number()) else {
            return false;
        };
        if i == names.len() - 1 {
            let vd = v.message_descriptor();
            if vd.full_name() != MONEY {
                return false;
            }
            let minor = vd
                .field_by_name("minor")
                .and_then(|f| v.field_by_number(f.number()));
            return matches!(minor, Some(DynValue::I64(x)) if *x != 0);
        }
        m = v;
    }
    false
}

impl Return {
    /// diagnostics adds the engine's warnings and errors to what the return
    /// leaves out, and each input it says is missing as a question.
    pub(super) fn diagnostics(&mut self) {
        for d in &self.out.federal.diagnostics {
            if d.severity == returnsv1::Severity::SEVERITY_WARNING
                || d.severity == returnsv1::Severity::SEVERITY_ERROR
            {
                self.notes.leave_out(d.message.clone());
            }
        }
        for p in &self.out.working.missing_inputs {
            self.notes.ask(model::new_question(
                format!("federal/missing/{p}"),
                "federal",
                format!("The federal return needs {p}."),
                "It is worked out without it until it is given.",
                "facts",
                &[],
            ));
        }
    }
}
