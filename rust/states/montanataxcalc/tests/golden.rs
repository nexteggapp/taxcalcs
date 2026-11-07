//! Montana's answers against the vectors Go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::MONTANA, taxcalcs_montanataxcalc::estimated_tax);
}
