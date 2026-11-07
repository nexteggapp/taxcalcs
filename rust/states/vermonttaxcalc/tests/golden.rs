//! The golden vectors golden/estimated_test.go wrote for Vermont.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::VERMONT, taxcalcs_vermonttaxcalc::estimated_tax);
}
