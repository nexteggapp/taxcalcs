//! Maryland's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::MARYLAND,
        taxcalcs_marylandtaxcalc::estimated_tax,
    );
}
