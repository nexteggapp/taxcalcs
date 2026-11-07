//! New Jersey's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::NEW_JERSEY,
        taxcalcs_newjerseytaxcalc::estimated_tax,
    );
}
