package working

import "errors"

// Divergence is a rule on which a tax authority's form and its statute
// disagree.
//
// A calculator in this module computes what the current form computes,
// because the form is what a person filing follows and, as far as anything
// published shows, what is assessed. It reports the statute's reading beside
// it, with the source for each, so that a reviewer can see both and a later
// change to either is noticed.
//
// A statutory exception the form omits is a divergence too. It is reported as
// available rather than applied or ignored.
//
// The fields are unexported and set only through [DivergenceBuilder], so a
// value cannot change after it is checked.
type Divergence struct {
	rule          string
	form          string
	formSource    string
	statute       string
	statuteSource string
	uses          Reading
	affects       bool
	affectsSet    bool
}

// Reading is which of the two documents a figure follows.
type Reading int

const (
	ReadingUnspecified Reading = iota

	// ReadingForm is the form's reading: the usual case.
	ReadingForm

	// ReadingStatute is the statute's reading, used where the form predates a
	// change in the law.
	ReadingStatute
)

func (r Reading) String() string {
	switch r {
	case ReadingForm:
		return "the form"
	case ReadingStatute:
		return "the statute"
	default:
		return "unspecified reading"
	}
}

// Rule names the rule the two documents disagree about.
func (d Divergence) Rule() string { return d.rule }

// Form says what the form does, in plain words.
func (d Divergence) Form() string { return d.form }

// FormSource cites the form, its line or section, and its year.
func (d Divergence) FormSource() string { return d.formSource }

// Statute says what the statute says instead.
func (d Divergence) Statute() string { return d.statute }

// StatuteSource cites the statute.
func (d Divergence) StatuteSource() string { return d.statuteSource }

// Uses is the reading the figures follow.
func (d Divergence) Uses() Reading { return d.uses }

// AffectsHousehold reports whether the two readings give this household
// different figures or answers. A divergence that does not is still reported,
// so a reviewer sees every rule that was read, and a product can leave it
// out of what it shows the household.
func (d Divergence) AffectsHousehold() bool { return d.affects }

// DivergenceBuilder builds a [Divergence].
type DivergenceBuilder struct{ d Divergence }

// NewDivergence starts a divergence on the named rule.
func NewDivergence(rule string) *DivergenceBuilder {
	return &DivergenceBuilder{d: Divergence{rule: rule}}
}

// Form records what the form does and where it says so.
func (b *DivergenceBuilder) Form(does, source string) *DivergenceBuilder {
	b.d.form, b.d.formSource = does, source
	return b
}

// Statute records what the statute says and where.
func (b *DivergenceBuilder) Statute(says, source string) *DivergenceBuilder {
	b.d.statute, b.d.statuteSource = says, source
	return b
}

// Uses records the reading the figures follow.
func (b *DivergenceBuilder) Uses(r Reading) *DivergenceBuilder {
	b.d.uses = r
	return b
}

// Affects records whether the two readings change anything for the facts
// the rules were given.
func (b *DivergenceBuilder) Affects(affects bool) *DivergenceBuilder {
	b.d.affects, b.d.affectsSet = affects, true
	return b
}

// Build checks that every part is present. A divergence missing either
// reading or either source cannot be checked by anybody, which is the reason
// to report it.
func (b *DivergenceBuilder) Build() (Divergence, error) {
	d := b.d
	switch {
	case d.rule == "":
		return Divergence{}, errors.New("working: a divergence needs the rule it is about")
	case d.form == "" || d.formSource == "":
		return Divergence{}, errors.New("working: a divergence needs what the form does and its source")
	case d.statute == "" || d.statuteSource == "":
		return Divergence{}, errors.New("working: a divergence needs what the statute says and its source")
	case d.uses != ReadingForm && d.uses != ReadingStatute:
		return Divergence{}, errors.New("working: a divergence needs the reading the figures follow")
	case !d.affectsSet:
		return Divergence{}, errors.New("working: a divergence needs whether it affects these facts")
	}
	return d, nil
}
