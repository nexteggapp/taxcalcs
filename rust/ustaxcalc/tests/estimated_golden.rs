//! The federal estimated tax rules against golden/testdata/estimated/US.json.gz.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::FEDERAL, taxcalcs_ustaxcalc::estimated_tax);
}
