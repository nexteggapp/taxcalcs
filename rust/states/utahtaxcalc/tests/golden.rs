//! Utah's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::UTAH, taxcalcs_utahtaxcalc::estimated_tax);
}
