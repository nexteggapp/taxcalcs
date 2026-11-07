//! Texas's answers against the vectors golden/estimated_test.go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::TEXAS, taxcalcs_texastaxcalc::estimated_tax);
}
