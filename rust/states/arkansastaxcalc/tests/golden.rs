//! Arkansas's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::ARKANSAS,
        taxcalcs_arkansastaxcalc::estimated_tax,
    );
}
