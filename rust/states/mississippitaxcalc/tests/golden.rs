//! Mississippi's answers against the vectors golden/estimated_test.go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::MISSISSIPPI,
        taxcalcs_mississippitaxcalc::estimated_tax,
    );
}
