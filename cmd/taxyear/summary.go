package main

import (
	"fmt"
	"io"

	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/taxyear"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

func cents(m *commonv1.Money) money.USCents { return money.USCents(m.GetMinor()) }

// printSummary writes each scenario's main figures: each job's wages and
// withholding, the federal return's bottom lines, each jurisdiction's
// estimated-payment answer, and the questions.
func printSummary(w io.Writer, r taxyear.Result) {
	p := r.Proto()
	fmt.Fprintf(w, "Tax year %d as of %04d-%02d-%02d: %s\n", p.GetTaxYear(), p.GetAsOf().GetYear(), p.GetAsOf().GetMonth(), p.GetAsOf().GetDay(), p.GetStatus())
	for _, pr := range p.GetProblems() {
		fmt.Fprintf(w, "  problem at %s: %s\n", pr.GetPath(), pr.GetMessage().GetText())
	}
	for _, s := range p.GetScenarios() {
		fmt.Fprintf(w, "\n%s\n", s.GetScenario())
		for _, pr := range s.GetProblems() {
			fmt.Fprintf(w, "  problem at %s: %s\n", pr.GetPath(), pr.GetMessage().GetText())
		}
		for _, e := range s.GetProjection().GetEmployers() {
			so, rest, year := e.GetSoFar().GetBoxes(), e.GetRest().GetBoxes(), e.GetYear().GetBoxes()
			fmt.Fprintf(w, "  %s: wages %v so far + %v rest = %v; federal withheld %v + %v = %v; %d items\n", e.GetId(),
				cents(so.GetWages()), cents(rest.GetWages()), cents(year.GetWages()),
				cents(so.GetFederalWithheld()), cents(rest.GetFederalWithheld()), cents(year.GetFederalWithheld()), len(e.GetItems()))
		}
		if f := s.GetFederal().GetForm_1040(); f != nil {
			fmt.Fprintf(w, "  federal: AGI %v, taxable %v, tax %v, total tax %v, payments %v, owed %v, overpaid %v\n",
				cents(f.GetAdjustedGrossIncome()), cents(f.GetTaxableIncome()), cents(f.GetTax()), cents(f.GetTotalTax()),
				cents(f.GetTotalPayments()), cents(f.GetAmountOwed()), cents(f.GetOverpaid()))
		}
		for _, e := range s.GetEstimated() {
			a := e.GetRules()
			line := fmt.Sprintf("  estimated %s: %s", e.GetJurisdiction(), a.GetOutcome())
			if req := a.GetRequirement(); req != nil {
				line += fmt.Sprintf(", required %v, target %v", req.GetRequired(), money.USCents(req.GetTargetCents()))
			}
			if pen := a.GetPenalty(); pen != nil {
				line += fmt.Sprintf(", penalty %v", money.USCents(pen.GetTotalCents()))
			}
			fmt.Fprintln(w, line)
		}
		for _, q := range s.GetQuestions() {
			fmt.Fprintf(w, "  question %s: %s\n", q.GetId(), q.GetQuestion().GetText())
		}
	}
}
