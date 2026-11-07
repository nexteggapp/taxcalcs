//! Nevada's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::NEVADA, taxcalcs_nevadataxcalc::estimated_tax);
}
