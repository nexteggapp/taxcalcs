//! New Hampshire's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::NEW_HAMPSHIRE,
        taxcalcs_newhampshiretaxcalc::estimated_tax,
    );
}
