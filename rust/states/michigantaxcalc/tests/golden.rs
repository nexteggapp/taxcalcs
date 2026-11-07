//! Michigan's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::MICHIGAN,
        taxcalcs_michigantaxcalc::estimated_tax,
    );
}
