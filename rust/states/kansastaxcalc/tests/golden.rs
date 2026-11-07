//! Kansas's answers against the vectors golden/estimated_test.go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::KANSAS, taxcalcs_kansastaxcalc::estimated_tax);
}
