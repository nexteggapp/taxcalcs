//! Washington's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::WASHINGTON,
        taxcalcs_washingtontaxcalc::estimated_tax,
    );
}
