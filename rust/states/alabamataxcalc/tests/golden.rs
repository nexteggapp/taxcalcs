//! Alabama's answers against the vectors Go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::ALABAMA, taxcalcs_alabamataxcalc::estimated_tax);
}
