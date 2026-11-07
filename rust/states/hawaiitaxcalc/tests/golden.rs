//! Hawaii's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::HAWAII, taxcalcs_hawaiitaxcalc::estimated_tax);
}
