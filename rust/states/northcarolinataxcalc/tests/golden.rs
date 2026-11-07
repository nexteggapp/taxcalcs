//! North Carolina's answers against the vectors Go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::NORTH_CAROLINA,
        taxcalcs_northcarolinataxcalc::estimated_tax,
    );
}
