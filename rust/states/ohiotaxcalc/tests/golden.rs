//! The golden vectors golden/estimated_test.go wrote for Ohio.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::OHIO, taxcalcs_ohiotaxcalc::estimated_tax);
}
