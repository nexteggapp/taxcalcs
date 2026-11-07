//! The golden vectors golden/estimated_test.go wrote for Delaware.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::DELAWARE,
        taxcalcs_delawaretaxcalc::estimated_tax,
    );
}
