//! Colorado's answers against the vectors golden/estimated_test.go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::COLORADO,
        taxcalcs_coloradotaxcalc::estimated_tax,
    );
}
