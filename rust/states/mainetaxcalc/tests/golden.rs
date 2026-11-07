//! Maine's answers against the vectors the Go code wrote.

#[test]
fn golden() {
    estimatedtest::assert_golden(&estimated::MAINE, taxcalcs_mainetaxcalc::estimated_tax);
}
