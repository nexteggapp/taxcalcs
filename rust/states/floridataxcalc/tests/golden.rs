//! Florida's answers against the vectors Go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::FLORIDA, taxcalcs_floridataxcalc::estimated_tax);
}
