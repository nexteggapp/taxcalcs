//! Idaho's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::IDAHO, taxcalcs_idahotaxcalc::estimated_tax);
}
