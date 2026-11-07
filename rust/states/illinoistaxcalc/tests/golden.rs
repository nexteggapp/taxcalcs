//! The golden vectors golden/estimated_test.go wrote for Illinois.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::ILLINOIS,
        taxcalcs_illinoistaxcalc::estimated_tax,
    );
}
