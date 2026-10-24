// Package estimated is the shared shape of estimated tax: the facts a
// jurisdiction's rules read, the answers they give, and the arithmetic several
// jurisdictions do the same way.
//
// It holds no jurisdiction's rules. Those are code, one implementation per
// jurisdiction, in that jurisdiction's own package: the federal rules in
// ustaxcalc, California's in californiataxcalc. What is shared is the question
// each implementation answers, which is [EstimatedTax], and the helpers:
// interest accrued daily, compounded or by the month, or charged at a factor
// a form prints; due dates moved for
// weekends and a holiday [Calendar]; cumulative-share schedules; the
// annualised installment recapture; and applying payments to installments in
// the order the penalty forms do.
//
// # Every value is immutable
//
// No struct here has an exported field. Each is read through accessors that
// return copies of any slice, and built through a builder whose Build
// validates. A jurisdiction's rules therefore never see facts that Build would
// have refused, and a caller cannot change an answer after it was computed.
//
// # Dependencies
//
// The standard library and this module's money, filing and working packages.
// Nothing else, and no protobuf.
//
// See docs/estimated-tax.md for the design.
package estimated

import (
	"errors"

	"github.com/nexteggapp/taxcalcs/working"
)

// Jurisdiction names a taxing jurisdiction by postal code: "US" for the
// federal government, "CA" for California.
type Jurisdiction string

// The federal government, the fifty states and the District of Columbia.
const (
	Federal            Jurisdiction = "US"
	Alabama            Jurisdiction = "AL"
	Alaska             Jurisdiction = "AK"
	Arizona            Jurisdiction = "AZ"
	Arkansas           Jurisdiction = "AR"
	California         Jurisdiction = "CA"
	Colorado           Jurisdiction = "CO"
	Connecticut        Jurisdiction = "CT"
	Delaware           Jurisdiction = "DE"
	DistrictOfColumbia Jurisdiction = "DC"
	Florida            Jurisdiction = "FL"
	Georgia            Jurisdiction = "GA"
	Hawaii             Jurisdiction = "HI"
	Idaho              Jurisdiction = "ID"
	Illinois           Jurisdiction = "IL"
	Indiana            Jurisdiction = "IN"
	Iowa               Jurisdiction = "IA"
	Kansas             Jurisdiction = "KS"
	Kentucky           Jurisdiction = "KY"
	Louisiana          Jurisdiction = "LA"
	Maine              Jurisdiction = "ME"
	Maryland           Jurisdiction = "MD"
	Massachusetts      Jurisdiction = "MA"
	Michigan           Jurisdiction = "MI"
	Minnesota          Jurisdiction = "MN"
	Mississippi        Jurisdiction = "MS"
	Missouri           Jurisdiction = "MO"
	Montana            Jurisdiction = "MT"
	Nebraska           Jurisdiction = "NE"
	Nevada             Jurisdiction = "NV"
	NewHampshire       Jurisdiction = "NH"
	NewJersey          Jurisdiction = "NJ"
	NewMexico          Jurisdiction = "NM"
	NewYork            Jurisdiction = "NY"
	NorthCarolina      Jurisdiction = "NC"
	NorthDakota        Jurisdiction = "ND"
	Ohio               Jurisdiction = "OH"
	Oklahoma           Jurisdiction = "OK"
	Oregon             Jurisdiction = "OR"
	Pennsylvania       Jurisdiction = "PA"
	RhodeIsland        Jurisdiction = "RI"
	SouthCarolina      Jurisdiction = "SC"
	SouthDakota        Jurisdiction = "SD"
	Tennessee          Jurisdiction = "TN"
	Texas              Jurisdiction = "TX"
	Utah               Jurisdiction = "UT"
	Vermont            Jurisdiction = "VT"
	Virginia           Jurisdiction = "VA"
	Washington         Jurisdiction = "WA"
	WestVirginia       Jurisdiction = "WV"
	Wisconsin          Jurisdiction = "WI"
	Wyoming            Jurisdiction = "WY"
)

// Jurisdictions returns every jurisdiction above: the federal government
// first, then the states and the District of Columbia in alphabetical order
// of their names.
func Jurisdictions() []Jurisdiction {
	return []Jurisdiction{
		Federal,
		Alabama,
		Alaska,
		Arizona,
		Arkansas,
		California,
		Colorado,
		Connecticut,
		Delaware,
		DistrictOfColumbia,
		Florida,
		Georgia,
		Hawaii,
		Idaho,
		Illinois,
		Indiana,
		Iowa,
		Kansas,
		Kentucky,
		Louisiana,
		Maine,
		Maryland,
		Massachusetts,
		Michigan,
		Minnesota,
		Mississippi,
		Missouri,
		Montana,
		Nebraska,
		Nevada,
		NewHampshire,
		NewJersey,
		NewMexico,
		NewYork,
		NorthCarolina,
		NorthDakota,
		Ohio,
		Oklahoma,
		Oregon,
		Pennsylvania,
		RhodeIsland,
		SouthCarolina,
		SouthDakota,
		Tennessee,
		Texas,
		Utah,
		Vermont,
		Virginia,
		Washington,
		WestVirginia,
		Wisconsin,
		Wyoming,
	}
}

// ErrNoRules reports that no estimated tax rules are built for a jurisdiction
// and year. It is never answered with an adjacent year's rules.
var ErrNoRules = errors.New("estimated: no estimated tax rules for this jurisdiction and year")

// ErrUnsupported reports facts that put a household under rules this build
// does not implement, such as the separate rules for farmers and fishermen.
// An error rather than an answer under the wrong rules.
var ErrUnsupported = errors.New("estimated: these facts need rules this build does not implement")

// ErrNeedsThisYearTax reports that a figure cannot be worked out without this
// year's tax. Schedule and Penalty return it when no option for the target
// can be measured without it, and a jurisdiction whose penalty is measured
// against this year's tax in every case, as Pennsylvania's is, returns it
// from Penalty when the answer depends on that tax. Requirement never
// returns it: it reports the question in Required instead.
var ErrNeedsThisYearTax = errors.New("estimated: this figure needs this year's tax")

// NeedsThisYearTax is what an option measured on this year's tax says when
// the tax was not given.
const NeedsThisYearTax = "Needs this year's tax"

// Divergence is a rule on which a form and its statute disagree. It is
// working's type, so that [working.Result] can carry it.
type Divergence = working.Divergence

// NewDivergence starts a [Divergence] on the named rule.
func NewDivergence(rule string) *working.DivergenceBuilder { return working.NewDivergence(rule) }

// EstimatedTax is one jurisdiction's estimated tax rules for one tax year.
//
// Every method returns the figure with its working: Applied cites each rule
// in the caller's numbers, Required lists what the caller has not said and
// the default used instead, and Divergences lists where the jurisdiction's
// form and statute disagree on a rule that was used.
type EstimatedTax interface {
	// Requirement is whether estimated payments are needed, and what total
	// keeps the household clear of the penalty.
	Requirement(f Facts) (working.Result[Requirement], error)

	// Schedule is each due date, what is needed by it, and what has been
	// credited by it.
	Schedule(f Facts) (working.Result[[]Installment], error)

	// Penalty is the underpayment penalty accrued up to asOf, or up to the
	// date the rules stop it, or up to [Facts.ReturnFiled], whichever is
	// earliest. Every payment in the facts counts on its own date, so planned
	// payments and a later asOf give the penalty if the plan is kept.
	Penalty(f Facts, asOf Date) (working.Result[Penalty], error)

	// Explanation is how this jurisdiction's estimated tax works, in plain
	// words.
	Explanation() Explanation
}
