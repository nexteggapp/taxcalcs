//! Wisconsin's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::WISCONSIN,
        taxcalcs_wisconsintaxcalc::estimated_tax,
    );
}
