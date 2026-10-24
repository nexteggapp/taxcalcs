// Package filing is the filing status a return is filed under.
//
// # Why one type serves every jurisdiction here
//
// A filing status is not federal law that the states happen to copy. Each state
// in this module requires the status on its return to be the status on the
// federal return, by its own statute: California by Rev. & Tax. Code § 18521(b),
// Oregon by ORS 316.122(1). A taxpayer cannot be single in Salem and married in
// Washington, D.C., so a separate state enum would be five names for the same
// five facts and a conversion function that can only be the identity.
//
// That is why this is shared while rate tables are not. The status is one fact
// about a household. What each jurisdiction does with it is that jurisdiction's
// business, and lives in its own calculator package.
//
// # What sharing does not mean
//
// It does not mean every jurisdiction taxes all five the same way, or at all.
// Washington's capital gains excise tax has no rate table by status; it gives
// spouses filing a joint federal return one deduction between them, which
// [wataxcalc] takes as a filing status argument and uses for that alone.
//
// A jurisdiction whose statuses genuinely differ should declare its own type
// rather than stretch this one. California registered domestic partners are the
// case to watch: Rev. & Tax. Code § 17021.7 has them file as married for state
// purposes while remaining unmarried federally, so a partnered filer's state
// status is [MarriedFilingJointly] though no federal return says so. That is
// still one of these five values, so it is the caller's job to pass the right
// one — not a sixth status.
package filing

// Status is the status a return is filed under.
//
// Surviving spouse is its own value even though 26 U.S.C. § 1(j)(2)(A) gives it
// the same federal rate table and § 63(c)(2) the same standard deduction as
// married filing jointly. The two differ elsewhere — § 63(f)(3) gives an
// unmarried individual a larger additional deduction for age or blindness, and
// a surviving spouse is not unmarried for that purpose — so folding them
// together here would lose the distinction at the point where it matters.
type Status int

const (
	// Unspecified is the zero value. Every calculator refuses it, so a caller
	// that forgot to set a status gets an error rather than a single filer's
	// tax.
	Unspecified Status = iota

	Single
	MarriedFilingJointly
	MarriedFilingSeparately
	HeadOfHousehold
	SurvivingSpouse
)

// String names the status as 26 U.S.C. § 1(j)(2) does. The state returns use
// the same words, except that California prints "married/RDP" where this says
// "married".
func (s Status) String() string {
	switch s {
	case Single:
		return "single"
	case MarriedFilingJointly:
		return "married filing jointly"
	case MarriedFilingSeparately:
		return "married filing separately"
	case HeadOfHousehold:
		return "head of household"
	case SurvivingSpouse:
		return "surviving spouse"
	default:
		return "unspecified filing status"
	}
}
