//! Indiana's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::INDIANA, taxcalcs_indianataxcalc::estimated_tax);
}
