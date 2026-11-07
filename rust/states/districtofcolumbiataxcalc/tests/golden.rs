//! The District of Columbia's answers against the vectors Go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::DISTRICT_OF_COLUMBIA,
        taxcalcs_districtofcolumbiataxcalc::estimated_tax,
    );
}
