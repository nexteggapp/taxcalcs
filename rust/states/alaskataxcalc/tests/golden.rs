//! Alaska's answers against the vectors golden/estimated_test.go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::ALASKA, taxcalcs_alaskataxcalc::estimated_tax);
}
