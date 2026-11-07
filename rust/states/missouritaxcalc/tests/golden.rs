//! Missouri's answers against the vectors golden/estimated_test.go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::MISSOURI,
        taxcalcs_missouritaxcalc::estimated_tax,
    );
}
