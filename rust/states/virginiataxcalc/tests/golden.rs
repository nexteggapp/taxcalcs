//! Virginia's answers against the vectors golden/estimated_test.go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::VIRGINIA,
        taxcalcs_virginiataxcalc::estimated_tax,
    );
}
