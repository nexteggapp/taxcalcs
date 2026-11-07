//! Connecticut's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::CONNECTICUT,
        taxcalcs_connecticuttaxcalc::estimated_tax,
    );
}
