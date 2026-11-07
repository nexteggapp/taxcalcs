//! Oregon's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::OREGON, taxcalcs_oregontaxcalc::estimated_tax);
}
