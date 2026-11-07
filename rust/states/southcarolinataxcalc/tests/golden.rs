//! South Carolina's answers against the vectors golden/estimated_test.go
//! wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::SOUTH_CAROLINA,
        taxcalcs_southcarolinataxcalc::estimated_tax,
    );
}
