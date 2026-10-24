// Package utahtaxcalc answers the shared estimated tax questions for Utah,
// which has no estimated tax for individuals.
//
// # Only the extension prepayment test
//
// Utah does not require payments during the year, and a return filed and
// paid by its due date owes no prepayment penalty. The one rule is for a
// return filed later, on the automatic six-month extension: by the original
// due date, withholding and prepayments must reach the lesser of 90% of this
// year's tax or 100% of last year's. Requirement is that test, Schedule is
// its one date, and Penalty is the extension penalty of 2% a month on the
// tax unpaid on that date.
//
// Whether the filer will use the extension is the one Utah fact, carried in
// [EstimatedFacts].
//
// # What is not built
//
// Interest, and the late-payment penalty on a balance paid after the due
// date, are charged whether or not the return is extended, and are not
// computed here. A return filed after the extension ends loses it, and the
// late-filing and late-payment penalties that replace the extension penalty
// get [estimated.ErrUnsupported].
//
// Start at [EstimatedTax].
package utahtaxcalc
