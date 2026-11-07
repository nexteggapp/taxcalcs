//! Arizona's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::ARIZONA, taxcalcs_arizonataxcalc::estimated_tax);
}
