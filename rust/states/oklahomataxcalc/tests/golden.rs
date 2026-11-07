//! Oklahoma's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::OKLAHOMA,
        taxcalcs_oklahomataxcalc::estimated_tax,
    );
}
