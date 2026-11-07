//! The golden vectors golden/estimated_test.go wrote for Wyoming.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::WYOMING, taxcalcs_wyomingtaxcalc::estimated_tax);
}
