//! Massachusetts's answers against the vectors Go wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(
        &estimated::MASSACHUSETTS,
        taxcalcs_massachusettstaxcalc::estimated_tax,
    );
}
