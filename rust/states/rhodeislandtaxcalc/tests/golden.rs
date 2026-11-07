//! The golden vectors golden/estimated_test.go wrote for Rhode Island.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::RHODE_ISLAND,
        taxcalcs_rhodeislandtaxcalc::estimated_tax,
    );
}
