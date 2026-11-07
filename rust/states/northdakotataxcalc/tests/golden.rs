//! North Dakota's answers against the vectors golden/estimated_test.go
//! wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::NORTH_DAKOTA,
        taxcalcs_northdakotataxcalc::estimated_tax,
    );
}
