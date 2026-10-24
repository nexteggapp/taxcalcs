// Package working is what a calculator returns besides the number: the rules it
// applied, and the choices it could not make for you.
//
// A tax figure a user cannot check is one they have to take on trust, and they
// cannot tell when it is wrong. So every calculator in this module returns a
// [Result], which carries the figure, the [Rule]s that produced it in the
// caller's own numbers, and any [Required] position the caller still owes.
//
// The wire mirror of this package is us/v1/working.proto, and the names match
// deliberately.
//
// # Why this is shared across jurisdictions
//
// Showing your working is not a federal idea. California's standard-versus-
// itemised election and the federal one are the same kind of unanswerable
// question, and a product rendering an "unanswered" list wants one list rather
// than one per jurisdiction. Sharing the envelope is what lets a caller loop
// over federal and state results together.
//
// What is not shared is the content. A [Rule] cites whatever authority the
// calculator that made it answers to — 26 U.S.C. for the federal calculators,
// Rev. & Tax. Code for California, ORS for Oregon, RCW for Washington.
package working

// PositionKind names a choice that the figures alone do not determine.
//
// These are the places tax law admits more than one answer, or where the answer
// depends on something outside the numbers a calculator was handed. The kinds
// are jurisdiction-neutral because the questions are: a state that conforms to
// the federal definition of adjusted gross income inherits the federal
// questions along with it. Which statute makes a given case a choice is said in
// [Required.Because] by the calculator that returns it.
type PositionKind int

const (
	PositionUnspecified PositionKind = iota

	// PositionDeductionMethod is whether to take the standard deduction or
	// itemise. Itemising wins only if the itemised total is larger, and a
	// calculator cannot know the total without being given it.
	//
	// 26 U.S.C. § 63(e) makes it an election federally. California and Oregon
	// each pose the same question on their own returns, and neither is bound by
	// what the federal return elected.
	PositionDeductionMethod

	// PositionLotSelection is which lots a sale consumed, which decides basis
	// and holding period. The default is first in, first out; specific
	// identification requires the seller to have identified the lots at the
	// time of sale.
	PositionLotSelection

	// PositionIRADeductibility is whether a traditional IRA contribution is
	// deductible, which under 26 U.S.C. § 219(g) depends on modified AGI and on
	// whether the taxpayer or their spouse is covered by a workplace plan.
	PositionIRADeductibility

	// PositionSubstantiallyIdentical is whether a security bought inside the
	// wash-sale window under 26 U.S.C. § 1091 is substantially identical to the
	// one sold. Two index funds tracking one index are the unsettled case.
	PositionSubstantiallyIdentical

	// PositionTradeOrBusiness is whether an activity is a trade or business
	// under 26 U.S.C. § 162, rather than a hobby under § 183. It decides
	// whether net earnings are subject to self-employment tax.
	PositionTradeOrBusiness

	// PositionDependentQualifies is whether a person is a qualifying child or
	// relative under 26 U.S.C. § 152, which turns on relationship, residency,
	// age and support tests that are facts about a household rather than
	// figures.
	PositionDependentQualifies

	// PositionResidency is whether the filer was a resident of the state for
	// the whole tax year, which decides both the form they file and how much of
	// their income the state reaches.
	//
	// No jurisdiction's figures answer it. Residency turns on domicile and on
	// days present, which are facts about a person rather than about their
	// income, and the state calculators in this module compute a full-year
	// resident's tax and say so.
	PositionResidency

	// PositionCapitalAssetSitus is whether a capital asset's gain is allocated
	// to a state at all, which for intangible property turns on where the filer
	// was domiciled when the sale settled rather than on where the asset was.
	//
	// Washington's capital gains excise tax under RCW 82.87.100 is the case
	// this exists for.
	PositionCapitalAssetSitus

	// PositionPenaltyWaiver is whether the filer asks for the estimated tax
	// penalty to be waived: for a casualty, disaster or other unusual
	// circumstance, or for retirement after 62 or disability with reasonable
	// cause. 26 U.S.C. § 6654(e)(3) and its state counterparts leave the
	// request to the filer and the decision to the tax authority, so a
	// calculator reports the penalty before any waiver.
	PositionPenaltyWaiver

	// PositionPriorYearReturn is what last year's return showed: whether one
	// was filed, whether it covered twelve months, and its tax. The
	// estimated tax safe harbours and the no-liability exception read it, and
	// a calculator not given it cannot offer them.
	PositionPriorYearReturn

	// PositionAdjustedGrossIncome is a year's adjusted gross income that a
	// threshold tests and the caller did not supply, such as the
	// § 6654(d)(1)(C) step to 110% of last year's tax.
	PositionAdjustedGrossIncome

	// PositionUnpublishedRate is not a choice. It reports that a figure
	// depends on an interest rate the authority has not yet published, so the
	// figure stops at the last day a published rate covers. The caller cannot
	// answer it; a later build of this library that carries the rate does.
	PositionUnpublishedRate

	// PositionRequirementFirstMet is when the filer could first expect to
	// have to make estimated payments for the year. Several states give a
	// filer who first meets the requirement after a due date fewer
	// installments, or excuse the earlier ones, and a calculator not told
	// assumes the requirement was met from the start of the year.
	PositionRequirementFirstMet

	// PositionJurisdictionFact is a fact only one jurisdiction's rules read,
	// carried in that jurisdiction's own extras, that the caller did not
	// supply. [Required.Because] names the fact and the default used.
	PositionJurisdictionFact

	// PositionThisYearTax is this year's tax, which the estimated tax rules
	// measure one option for the target against, and which the caller did
	// not give. The rules then use the options that do not need it, and say
	// so.
	PositionThisYearTax

	// PositionUnsettledRule is not a choice the caller can make. It reports
	// that the jurisdiction's own documents leave a rule open, or disagree,
	// and nothing the caller could supply settles it. [Required.Because]
	// names the readings and the one the figure uses. Like
	// [PositionUnpublishedRate], it is answered by a later build of this
	// library, once the jurisdiction says which reading holds.
	PositionUnsettledRule
)

// String names the position as the rule that creates it does. A caller
// rendering an unanswered position needs a word rather than an ordinal, and
// without this every one of them prints as its integer.
func (k PositionKind) String() string {
	switch k {
	case PositionDeductionMethod:
		return "deduction method"
	case PositionLotSelection:
		return "lot selection"
	case PositionIRADeductibility:
		return "IRA deductibility"
	case PositionSubstantiallyIdentical:
		return "substantially identical security"
	case PositionTradeOrBusiness:
		return "trade or business"
	case PositionDependentQualifies:
		return "dependent qualifies"
	case PositionResidency:
		return "residency"
	case PositionCapitalAssetSitus:
		return "capital asset situs"
	case PositionPenaltyWaiver:
		return "penalty waiver"
	case PositionPriorYearReturn:
		return "prior year return"
	case PositionAdjustedGrossIncome:
		return "adjusted gross income"
	case PositionUnpublishedRate:
		return "unpublished rate"
	case PositionRequirementFirstMet:
		return "requirement first met"
	case PositionJurisdictionFact:
		return "jurisdiction fact"
	case PositionThisYearTax:
		return "this year's tax"
	case PositionUnsettledRule:
		return "unsettled rule"
	default:
		return "unspecified position"
	}
}
