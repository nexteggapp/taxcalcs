//! Louisiana's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::LOUISIANA,
        taxcalcs_louisianataxcalc::estimated_tax,
    );
}
