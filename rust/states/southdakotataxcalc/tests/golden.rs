//! South Dakota's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::SOUTH_DAKOTA,
        taxcalcs_southdakotataxcalc::estimated_tax,
    );
}
