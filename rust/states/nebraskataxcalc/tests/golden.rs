//! Nebraska's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::NEBRASKA,
        taxcalcs_nebraskataxcalc::estimated_tax,
    );
}
