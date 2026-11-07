//! The golden vectors golden/estimated_test.go wrote for Tennessee.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::TENNESSEE,
        taxcalcs_tennesseetaxcalc::estimated_tax,
    );
}
