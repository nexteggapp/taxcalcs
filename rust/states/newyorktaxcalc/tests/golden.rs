//! The golden vectors golden/estimated_test.go wrote for New York.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::NEW_YORK, taxcalcs_newyorktaxcalc::estimated_tax);
}
