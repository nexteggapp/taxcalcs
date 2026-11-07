//! Kentucky's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::KENTUCKY,
        taxcalcs_kentuckytaxcalc::estimated_tax,
    );
}
