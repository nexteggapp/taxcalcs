//! New Mexico's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::NEW_MEXICO,
        taxcalcs_newmexicotaxcalc::estimated_tax,
    );
}
