//! Pennsylvania's answers against the vectors Go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::PENNSYLVANIA,
        taxcalcs_pennsylvaniataxcalc::estimated_tax,
    );
}
