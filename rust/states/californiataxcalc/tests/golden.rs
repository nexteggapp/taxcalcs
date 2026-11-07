//! California's answers against the vectors Go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::CALIFORNIA,
        taxcalcs_californiataxcalc::estimated_tax,
    );
}
