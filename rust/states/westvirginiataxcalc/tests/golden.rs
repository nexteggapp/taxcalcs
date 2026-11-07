//! West Virginia's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::WEST_VIRGINIA,
        taxcalcs_westvirginiataxcalc::estimated_tax,
    );
}
