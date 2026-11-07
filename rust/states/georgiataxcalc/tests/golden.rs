//! Georgia's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::GEORGIA, taxcalcs_georgiataxcalc::estimated_tax);
}
