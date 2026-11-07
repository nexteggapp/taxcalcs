//! A message checked once as the target of many diffs: target.go.

use std::sync::Arc;

use buffa_descriptor::{DescriptorPool, DynamicMessage, MessageIndex, ReflectMessage};

use crate::Error;
use crate::apply::apply;
use crate::plan::{Plan, Plans, plan_for};
use crate::validate::{check_data, validate_diff};

/// A message checked once as the target of one diff type's diffs: it has
/// no unknown fields, and each keyed list in it has every item's key set
/// and no key twice. [`crate::validate_dynamic`] and
/// [`crate::apply_dynamic`] check that of their target on every call, which
/// walks the whole message however small the diff. [`Target::apply`]
/// checks only the diff, and returns a Target, since a diff that validates
/// leaves the result with the same properties.
///
/// A Target holds its message, shared rather than copied.
#[derive(Clone, Debug, Default)]
pub struct Target {
    m: Option<Arc<DynamicMessage>>,
    p: Option<(Plans, Arc<Plan>)>,
}

impl Target {
    /// Checks a as the target of diffs whose message is diff, of the pool
    /// pool, as validate checks its a.
    ///
    /// # Errors
    ///
    /// When diff is not a patch body for a's type, or a fails the check.
    pub fn new(
        a: Arc<DynamicMessage>,
        pool: &Arc<DescriptorPool>,
        diff: MessageIndex,
    ) -> Result<Target, Error> {
        let (ps, p) = plan_for(pool, diff)?;
        p.bind(&ps, a.pool(), a.message_index())?;
        check_data(&ps, Some(&p), &a, "a")?;
        Ok(Target {
            m: Some(a),
            p: Some((ps, p)),
        })
    }

    /// The target's message, not a copy; None for the zero Target.
    pub fn message(&self) -> Option<&Arc<DynamicMessage>> {
        self.m.as_ref()
    }

    /// Validates d against the target as validate does, without checking
    /// the target again, then returns the target with d applied. The target
    /// is not changed.
    ///
    /// # Errors
    ///
    /// When the Target is the zero value, d is not of the Target's diff
    /// type, or d doesn't apply.
    pub fn apply(&self, d: &DynamicMessage) -> Result<Target, Error> {
        let (Some(m), Some((ps, p))) = (&self.m, &self.p) else {
            return Err(Error::Mismatch(
                "apply: the Target is the zero value; use NewTarget".to_string(),
            ));
        };
        if !Arc::ptr_eq(d.pool(), &ps.pool) || d.message_index() != p.body {
            return Err(Error::Mismatch(format!(
                "apply: the diff is a {}; the Target takes {}",
                d.message_descriptor().full_name(),
                p.body_desc(&ps.pool).full_name()
            )));
        }
        validate_diff(ps, p, m, d)?;
        let mut out = (**m).clone();
        apply(ps, p, &mut out, d);
        Ok(Target {
            m: Some(Arc::new(out)),
            p: self.p.clone(),
        })
    }
}
