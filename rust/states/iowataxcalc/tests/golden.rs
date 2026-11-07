//! The golden vectors golden/estimated_test.go wrote for Iowa.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::IOWA, taxcalcs_iowataxcalc::estimated_tax);
}
