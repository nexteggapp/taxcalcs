//! Minnesota's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::MINNESOTA,
        taxcalcs_minnesotataxcalc::estimated_tax,
    );
}
