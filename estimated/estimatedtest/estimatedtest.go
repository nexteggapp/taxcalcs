// Package estimatedtest checks any jurisdiction's estimated tax rules against
// what every jurisdiction must satisfy: the shape of its answers and the copy
// rules for every sentence a person reads.
//
// It runs the rules over a fixed set of households chosen to reach the
// common branches: tax known and unknown, last year's return filed, not
// filed and absent, no tax last year, a late start, a part-year resident, the
// annualised method, an early return, a waiver asked for, and payments made
// late. State extras are never supplied, so each state's defaults and
// questions are exercised too.
package estimatedtest

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nexteggapp/taxcalcs/estimated"
	"github.com/nexteggapp/taxcalcs/filing"
	"github.com/nexteggapp/taxcalcs/money"
	"github.com/nexteggapp/taxcalcs/working"
)

// Household is one set of facts and its name.
type Household struct {
	name  string
	facts estimated.Facts
}

func (h Household) Name() string           { return h.name }
func (h Household) Facts() estimated.Facts { return h.facts }

// Households returns the fixed set of households for j and tax year 2026.
// A combination the facts builder refuses is left out.
func Households(j estimated.Jurisdiction) []Household {
	const year = 2026
	d := func(m time.Month, day int) estimated.Date { return estimated.MustDate(year, m, day) }
	dollars := func(n int64) money.USCents { return money.Dollars(n) }

	figures := func(tax, agi int64) estimated.Figures {
		b := estimated.NewFigures()
		if tax >= 0 {
			b.Tax(dollars(tax))
		}
		if agi >= 0 {
			b.FederalAGI(dollars(agi)).StateAGI(dollars(agi))
		}
		f, _ := b.Build()
		return f
	}
	prior := func(tax, agi int64) estimated.PriorReturn {
		p, _ := estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).Figures(figures(tax, agi)).Build()
		return p
	}
	notFiled, _ := estimated.NewPriorReturn().Filed(false).Build()
	withheld := func(total int64) []estimated.Withheld {
		var out []estimated.Withheld
		for m := time.January; m <= time.December; m++ {
			w, _ := estimated.NewWithheld().On(estimated.MustDate(year, m, 28)).Amount(dollars(total) / 12).Build()
			out = append(out, w)
		}
		return out
	}
	pay := func(m time.Month, day int, n int64) estimated.Payment {
		p, _ := estimated.NewPayment().On(estimatedDate(year, m, day)).Amount(dollars(n)).Build()
		return p
	}
	quarterly := func(n int64) []estimated.Payment {
		return []estimated.Payment{pay(time.April, 15, n), pay(time.June, 15, n), pay(time.September, 15, n)}
	}
	resident := func(from, to estimated.Date) estimated.ResidencePeriod {
		r, _ := estimated.NewResidencePeriod(string(j)).From(from).To(to).Build()
		return r
	}

	type spec struct {
		name  string
		build func(b *estimated.FactsBuilder)
	}
	specs := []spec{
		{"withholding over the tax", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(3_000, 60_000)).PriorYear(prior(2_800, 55_000)).Withheld(withheld(3_600)...)
		}},
		{"withholding over last year's tax", func(b *estimated.FactsBuilder) {
			p, _ := estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).
				Figures(func() estimated.Figures {
					f, _ := estimated.NewFigures().Tax(dollars(2_000)).Withheld(dollars(2_136)).FederalAGI(dollars(50_000)).StateAGI(dollars(50_000)).Build()
					return f
				}()).Build()
			b.ThisYear(figures(6_000, 70_000)).PriorYear(p).Withheld(withheld(1_000)...)
		}},
		{"this year's tax unknown, little due last year", func(b *estimated.FactsBuilder) {
			p, _ := estimated.NewPriorReturn().Filed(true).Months(12).FullYearResident(true).
				Figures(func() estimated.Figures {
					f, _ := estimated.NewFigures().Tax(dollars(600)).Withheld(dollars(400)).FederalAGI(dollars(30_000)).StateAGI(dollars(30_000)).Build()
					return f
				}()).Build()
			b.ThisYear(figures(-1, 40_000)).PriorYear(p).Withheld(withheld(500)...)
		}},
		{"withholding covers nearly all", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(5_000, 80_000)).PriorYear(prior(4_000, 75_000)).Withheld(withheld(4_500)...)
		}},
		{"large balance, paid quarterly", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(30_000, 160_000)).PriorYear(prior(20_000, 140_000)).Withheld(withheld(10_000)...).
				Paid(quarterly(2_000)...)
		}},
		{"nothing paid", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(12_000, 120_000)).PriorYear(prior(9_000, 110_000))
		}},
		{"this year's tax unknown", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(-1, -1)).PriorYear(prior(8_000, 100_000)).Withheld(withheld(2_000)...)
		}},
		{"neither year's tax", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(-1, 60_000)).Withheld(withheld(1_000)...)
		}},
		{"last year not filed", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(9_000, 90_000)).PriorYear(notFiled)
		}},
		{"no return on record", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(9_000, 90_000)).Withheld(withheld(1_000)...)
		}},
		{"no tax last year", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(9_000, 90_000)).PriorYear(prior(0, 20_000))
		}},
		{"high income", func(b *estimated.FactsBuilder) {
			b.FilingStatus(filing.MarriedFilingJointly).ThisYear(figures(90_000, 600_000)).PriorYear(prior(70_000, 500_000)).
				Withheld(withheld(40_000)...).Paid(quarterly(8_000)...)
		}},
		{"paid late", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(12_000, 120_000)).PriorYear(prior(9_000, 110_000)).
				Paid(pay(time.May, 20, 3_000), pay(time.August, 1, 3_000), pay(time.December, 1, 3_000))
		}},
		{"late start", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(12_000, 120_000)).PriorYear(prior(9_000, 110_000)).RequirementFirstMet(d(time.July, 1))
		}},
		{"part-year resident", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(6_000, 70_000)).PriorYear(prior(5_000, 60_000)).Residence(resident(d(time.January, 1), d(time.June, 30)))
		}},
		{"full-year resident", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(6_000, 70_000)).PriorYear(prior(5_000, 60_000)).Residence(resident(d(time.January, 1), d(time.December, 31))).
				Withheld(withheld(3_000)...)
		}},
		{"return filed in January", func(b *estimated.FactsBuilder) {
			b.ThisYear(figures(12_000, 120_000)).PriorYear(prior(9_000, 110_000)).Paid(quarterly(2_000)...).
				ReturnFiled(estimatedDate(year+1, time.January, 25))
		}},
		{"waiver asked for", func(b *estimated.FactsBuilder) {
			pos, _ := estimated.NewPositions().Waiver(estimated.WaiverCasualty).Build()
			b.ThisYear(figures(12_000, 120_000)).PriorYear(prior(9_000, 110_000)).Positions(pos)
		}},
		{"retired in 2026 at 64", func(b *estimated.FactsBuilder) {
			pos, _ := estimated.NewPositions().WaiverFor(estimated.CauseRetirement, d(time.June, 1)).Born(estimated.MustDate(1962, time.March, 1)).Build()
			b.ThisYear(figures(12_000, 120_000)).PriorYear(prior(9_000, 110_000)).Positions(pos)
		}},
		{"retired in 2020", func(b *estimated.FactsBuilder) {
			pos, _ := estimated.NewPositions().WaiverFor(estimated.CauseRetirement, estimated.MustDate(2020, time.June, 1)).Born(estimated.MustDate(1955, time.March, 1)).Build()
			b.ThisYear(figures(12_000, 120_000)).PriorYear(prior(9_000, 110_000)).Positions(pos)
		}},
		{"a disaster", func(b *estimated.FactsBuilder) {
			pos, _ := estimated.NewPositions().WaiverFor(estimated.CauseDisaster, d(time.August, 1)).Build()
			b.ThisYear(figures(12_000, 120_000)).PriorYear(prior(9_000, 110_000)).Positions(pos)
		}},
		{"withholding on its dates", func(b *estimated.FactsBuilder) {
			el, _ := estimated.NewElections().WithholdingAsWithheld().Build()
			b.ThisYear(figures(12_000, 120_000)).PriorYear(prior(9_000, 110_000)).Withheld(withheld(6_000)...).Elections(el)
		}},
		{"annualised", func(b *estimated.FactsBuilder) {
			el, _ := estimated.NewElections().Annualised().Build()
			var ps []estimated.Period
			for i, end := range []estimated.Date{d(time.March, 31), d(time.May, 31), d(time.August, 31), d(time.December, 31)} {
				p, _ := estimated.NewPeriod().End(end).Income(dollars(int64(10_000 * (i + 1)))).AnnualisedTax(dollars(int64(1_000 * (i + 1)))).Build()
				ps = append(ps, p)
			}
			b.ThisYear(figures(12_000, 120_000)).PriorYear(prior(9_000, 110_000)).Elections(el).Periods(ps...)
		}},
	}
	var out []Household
	for _, s := range specs {
		b := estimated.NewFacts(j, year).FilingStatus(filing.Single)
		s.build(b)
		if j == estimated.Washington {
			// Washington's capital gains tax has no withholding, no
			// annualised method, and takes payments only from Oct 15 of the
			// year of the sale, so those facts are refused rather than
			// answered. Its households carry the same tax and payments on
			// dates it accepts.
			if s.name == "annualised" {
				continue
			}
			b = washington(b, year)
		}
		f, err := b.Build()
		if err != nil {
			continue
		}
		out = append(out, Household{name: s.name, facts: f})
	}
	return out
}

func estimatedDate(y int, m time.Month, d int) estimated.Date { return estimated.MustDate(y, m, d) }

// Answers are what the rules said about one household. An error from a
// method leaves its result zero.
type Answers struct {
	household   Household
	requirement working.Result[estimated.Requirement]
	schedule    working.Result[[]estimated.Installment]
	penalty     working.Result[estimated.Penalty]
	errs        [3]error
}

func (a Answers) Household() Household { return a.household }
func (a Answers) Requirement() (working.Result[estimated.Requirement], error) {
	return a.requirement, a.errs[0]
}
func (a Answers) Schedule() (working.Result[[]estimated.Installment], error) {
	return a.schedule, a.errs[1]
}
func (a Answers) Penalty() (working.Result[estimated.Penalty], error) { return a.penalty, a.errs[2] }

// PenaltyAsOf is the date every penalty is asked to: the end of the next
// year, which every jurisdiction's rules stop before.
var PenaltyAsOf = estimated.MustDate(2027, time.December, 31)

// Ask runs rules over every household.
func Ask(rules estimated.EstimatedTax, hs []Household) []Answers {
	var out []Answers
	for _, h := range hs {
		a := Answers{household: h}
		a.requirement, a.errs[0] = rules.Requirement(h.facts)
		a.schedule, a.errs[1] = rules.Schedule(h.facts)
		a.penalty, a.errs[2] = rules.Penalty(h.facts, PenaltyAsOf)
		out = append(out, a)
	}
	return out
}

// Expected reports whether err is one a jurisdiction may return for valid
// facts: rules not built for them, or a figure that needs this year's tax.
func Expected(err error) bool {
	return err == nil || errors.Is(err, estimated.ErrUnsupported) || errors.Is(err, estimated.ErrNeedsThisYearTax)
}

// Text is one string a person reads, and where it came from.
type Text struct {
	where string
	text  string
	limit int
	kind  textKind
}

type textKind int

const (
	textOther    textKind = iota
	textBody              // an explanation's summary or a body paragraph
	textSentence          // exactly one sentence: a penalty's summary
	textTwo               // one or two sentences: a question's effect, a note, a reason
	textQuestion          // one sentence ending in a question mark
)

func (t Text) Where() string { return t.where }
func (t Text) Text() string  { return t.text }

// Texts returns every string a person reads in the explanation, when rules
// is not nil, and the answers: the requirement's reason, each option's
// label and why it is not available, every question, every divergence, and
// the penalty's reason. Citations are not included: a person sees them as
// the titles of the documents they link to, not as sentences. Nor is a
// rule's working note, which the payments API does not return.
func Texts(rules estimated.EstimatedTax, as []Answers) []Text {
	var out []Text
	addKind := func(where, s string, limit int, k textKind) {
		if s != "" {
			out = append(out, Text{where: where, text: s, limit: limit, kind: k})
		}
	}
	add := func(where, s string, limit int) { addKind(where, s, limit, textOther) }
	if rules != nil {
		e := rules.Explanation()
		addKind("explanation summary", e.Summary(), 300, textBody)
		for i, p := range e.Body() {
			addKind(fmt.Sprintf("explanation paragraph %d", i+1), p, 700, textBody)
		}
		add("explanation versus federal", e.VersusFederal(), 400)
	}
	notes := func(h string, req []working.Required, ds []working.Divergence) {
		for _, r := range req {
			where := h + ": " + r.Position().String()
			if !r.Asked() {
				out = append(out, Text{where: where, text: r.Because(), kind: textOther, limit: -1})
				continue
			}
			addKind(where+" question", r.Question(), 200, textQuestion)
			addKind(where+" effect", r.Effect(), 250, textTwo)
			unanswerable := r.Position() == working.PositionUnpublishedRate || r.Position() == working.PositionUnsettledRule
			switch {
			case unanswerable && r.Question() != "":
				out = append(out, Text{where: where, text: r.Question(), kind: textOther, limit: -2})
			case !unanswerable && r.Question() == "":
				out = append(out, Text{where: where, text: r.Effect(), kind: textOther, limit: -3})
			}
		}
		for _, d := range ds {
			add(h+": divergence rule", d.Rule(), 120)
			add(h+": divergence form", d.Form(), 300)
			add(h+": divergence statute", d.Statute(), 300)
		}
	}
	for _, a := range as {
		h := a.household.name
		if a.errs[0] == nil {
			r := a.requirement.Value()
			add(h+": why", r.Why(), 300)
			for _, o := range r.Options() {
				add(h+": option label", o.Label(), 80)
				add(h+": option why not", o.WhyNot(), 200)
			}
			notes(h, a.requirement.Required(), a.requirement.Divergences())
		}
		if a.errs[1] == nil {
			notes(h, a.schedule.Required(), a.schedule.Divergences())
		}
		if a.errs[2] == nil {
			addKind(h+": penalty reason", a.penalty.Value().Reason(), 200, textTwo)
			addKind(h+": penalty summary", a.penalty.Value().Summary(), 200, textSentence)
			if o, ok := a.penalty.Value().WaiverOutcome(); ok {
				addKind(h+": waiver outcome", o.Reason(), 250, textTwo)
			}
			notes(h, a.penalty.Required(), a.penalty.Divergences())
		}
	}
	return out
}

// forbidden are the patterns the copy rules exclude, each with the rule.
var forbidden = []struct {
	re   *regexp.Regexp
	rule string
}{
	{regexp.MustCompile(`\$[0-9][0-9,]*\.[0-9]{2}\b`), "money in whole dollars, no cents"},
	{regexp.MustCompile(`\$[0-9]{4,}`), "money with a thousands separator"},
	{regexp.MustCompile(`(?i)\bow(e|es|ed|ing)\b`), `"due", not "owe"`},
	{regexp.MustCompile(`(?i)\b(we|we're|we've|our|ours)\b`), `never "we"`},
	{regexp.MustCompile(`(?i)\bthis estimate\b|\bthe estimate\b|\bmodell?ed\b|\bprototype\b|\bfixtures?\b|\bthe builder\b`), "never about the estimate or the software"},
	{regexp.MustCompile(`(?i)\b(not|was|were|is|if|when) given\b|\bwasn't given\b|\bweren't given\b`), "never about what was or wasn't entered"},
	{regexp.MustCompile(`(?i)\bhere\b`), `never "here": the person is not inside the software`},
	{regexp.MustCompile(`(?i)\bthis follows\b|\bthe default\b|\bdefaults?\b|\bnot built\b|\bthis build\b|\bthe library\b|\bthe caller\b`), "never about the software's choices"},
	{regexp.MustCompile(`(?i)can'?t be worked out|cannot be worked out|counts only the days|\bisn't known\b|\bis not known\b|\bnot known\b|\bunknown\b`), "never about what the software can't do"},
	{regexp.MustCompile(`(?i)research/|\.md\b|estimated-payments/`), "never a file path"},
	{regexp.MustCompile(`§|U\.S\.C\.|\bRSMo\b|\bO\.S\.\b|\bIC [0-9]`), "no statute citations in a sentence; they go in the sources"},
	{regexp.MustCompile(`%!|%\(|\(MISSING\)|\bEXTRA\b`), "a formatting error"},
	{regexp.MustCompile(`[−-]\s?\$[0-9]`), "a negative balance reads as a refund, not a minus sign"},
	{regexp.MustCompile(`(?i)last day a published`), "the penalty's summary carries where interest stops"},
}

// fragments are shortenings that drop a word a sentence needs, each with
// what is missing. [CheckCopy] and [CheckSource] both look for them.
var fragments = []struct {
	re   *regexp.Regexp
	rule string
}{
	// "It can for a casualty", "It can also if you retired".
	{regexp.MustCompile(`\b(?:[Ii]t|[Tt]hey|[Tt]his|[Tt]hat)\s+(?:can|could|must|may|might|will|would|should|does|do|did)\s+(?:also\s+|still\s+|only\s+|too\s+)?(?:for|if|when|after|because|by|on|from|with|unless|once|but)\b`), "a modal with no verb: say what it can do"},
	// "Nor can it for a casualty".
	{regexp.MustCompile(`\b(?:can|could|must|may|will|would|does|do|did)\s+(?:it|they)\s+(?:also\s+)?(?:for|if|when|after|because|unless)\b`), "a modal with no verb: say what it can do"},
	// "Nor is there if you had no tax".
	{regexp.MustCompile(`\bis there\s+(?:if|for|when|unless)\b`), "a missing noun: say there is none, or any"},
	// "either year's is under $500".
	{regexp.MustCompile(`\b(?:year|month|quarter|period)'s\s+(?:is|was|are|were|can|could|may|will|would|has|had)\b`), "a possessive with no noun: say whose what"},
}

// niche are the cases an explanation's body leaves to the reasons and
// divergences, which say whether they apply to the household.
var niche = regexp.MustCompile(`(?i)group return|nonresident|non-resident|part-year|part year|new resident|first-year|first year resident|\bmov(e|es|ed|ing) (out|in|into|to|from|away)\b|\byou move\b|\bfarm|\bfisher|\bfishing\b|military|nursing home`)

// sentenceEnd finds the end of a sentence followed by another.
var sentenceEnd = regexp.MustCompile(`[.?!]["”’)]?\s+[A-Z0-9(“"$]`)

// MaxSentence is the most characters a sentence a person reads may have.
const MaxSentence = 80

// Sentences splits text at each sentence end that sentenceEnd finds.
func Sentences(text string) []string {
	var out []string
	start := 0
	for _, m := range sentenceEnd.FindAllStringIndex(text, -1) {
		// The match ends on the next sentence's first character.
		_, size := utf8.DecodeLastRuneInString(text[:m[1]])
		end := m[1] - size
		out = append(out, strings.TrimSpace(text[start:end]))
		start = end
	}
	return append(out, strings.TrimSpace(text[start:]))
}

// CheckCopy returns each breach of the copy rules in texts, and each text
// longer than its kind allows. An explanation may have at most four
// paragraphs, and its summary and body describe the ordinary household:
// group returns, moves, farmers and the like are left to the reasons and
// divergences. A penalty's summary is one sentence; a penalty's reason, a
// waiver's reason and a question's effect are one or two. A question is one
// sentence ending in a question mark. Every sentence a person reads is at
// most [MaxSentence] characters. Every question
// comes from working.Notes.Ask, with no question for a rate not yet
// published or a rule left open, and a question for everything else.
func CheckCopy(rules estimated.EstimatedTax, texts []Text) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range texts {
		switch t.limit {
		case -1:
			out = append(out, fmt.Sprintf("%s: made with Require, not Ask: %q", t.where, t.text))
			continue
		case -2:
			out = append(out, fmt.Sprintf("%s: asks %q, though the person can't answer it", t.where, t.text))
			continue
		case -3:
			out = append(out, fmt.Sprintf("%s: has no question for the person, only %q", t.where, t.text))
			continue
		}
		switch t.kind {
		case textBody:
			if m := niche.FindString(t.text); m != "" {
				out = append(out, fmt.Sprintf("%s: %q is a case for the reasons and divergences, not the explanation: %q", t.where, m, t.text))
			}
		case textSentence, textQuestion:
			if sentenceEnd.MatchString(t.text) {
				out = append(out, fmt.Sprintf("%s: more than one sentence: %q", t.where, t.text))
			}
			if t.kind == textQuestion && !strings.HasSuffix(t.text, "?") {
				out = append(out, fmt.Sprintf("%s: not a question: %q", t.where, t.text))
			}
		case textTwo:
			if n := len(Sentences(t.text)); n > 2 {
				out = append(out, fmt.Sprintf("%s: %d sentences, more than two: %q", t.where, n, t.text))
			}
		}
		for _, f := range fragments {
			if m := f.re.FindString(t.text); m != "" && !seen[f.rule+t.text] {
				seen[f.rule+t.text] = true
				out = append(out, fmt.Sprintf("%s: %q is %s, in %q", t.where, m, f.rule, t.text))
			}
		}
		for _, f := range forbidden {
			if m := f.re.FindString(t.text); m != "" {
				msg := fmt.Sprintf("%s: %q breaks %q in %q", t.where, m, f.rule, t.text)
				if !seen[f.rule+t.text] {
					seen[f.rule+t.text] = true
					out = append(out, msg)
				}
			}
		}
		for _, sentence := range Sentences(t.text) {
			if n := utf8.RuneCountInString(sentence); n > MaxSentence && !seen["sentence"+sentence] {
				seen["sentence"+sentence] = true
				out = append(out, fmt.Sprintf("%s: a sentence of %d characters, more than %d: %q", t.where, n, MaxSentence, sentence))
			}
		}
		if len(t.text) > t.limit && !seen["len"+t.text] {
			seen["len"+t.text] = true
			out = append(out, fmt.Sprintf("%s: %d characters, more than %d: %q", t.where, len(t.text), t.limit, t.text))
		}
	}
	if n := len(rules.Explanation().Body()); n > 4 {
		out = append(out, fmt.Sprintf("the explanation has %d paragraphs, more than four", n))
	}
	return out
}

// CheckAnswers returns each breach of the answers' invariants:
//   - when a target is known exactly one option is chosen, and none
//     otherwise;
//   - an exempt requirement's chosen option is an exception at $0, and the
//     target is $0;
//   - every penalty has a reason, and under an exception it is the
//     exception's label and the penalty is $0;
//   - every installment's status is [estimated.StatusOf] its running totals;
//   - a jurisdiction's methods return only the errors [Expected] allows.
func CheckAnswers(as []Answers) []string {
	var out []string
	for _, a := range as {
		h := a.household.name
		for i, err := range a.errs {
			if !Expected(err) {
				out = append(out, fmt.Sprintf("%s: %s: %v", h, [...]string{"Requirement", "Schedule", "Penalty"}[i], err))
			}
		}
		var ds []working.Divergence
		if a.errs[0] == nil {
			ds = append(ds, a.requirement.Divergences()...)
		}
		if a.errs[2] == nil {
			ds = append(ds, a.penalty.Divergences()...)
		}
		for _, d := range ds {
			if why := sameReadings(d); why != "" {
				out = append(out, fmt.Sprintf("%s: divergence %q doesn't diverge: %s", h, d.Rule(), why))
			}
		}
		if a.errs[0] != nil {
			continue
		}
		r := a.requirement.Value()
		chosen := 0
		for _, o := range r.Options() {
			if c, ok := r.Chosen(); ok && c == o {
				chosen++
			}
		}
		c, ok := r.Chosen()
		switch {
		case r.TargetKnown() && (!ok || chosen != 1):
			out = append(out, fmt.Sprintf("%s: a target is known but %d options are chosen", h, chosen))
		case !r.TargetKnown() && ok:
			out = append(out, fmt.Sprintf("%s: no target is known but %q is chosen", h, c.Label()))
		case r.Exempt() && (c.Basis() != estimated.BasisException || c.Amount() != 0 || r.Target() != 0):
			out = append(out, fmt.Sprintf("%s: exempt, but the chosen option is %v at %v", h, c.Basis(), c.Amount()))
		case ok && c.Basis() == estimated.BasisException && !r.Exempt():
			out = append(out, fmt.Sprintf("%s: an exception is chosen but the requirement is not exempt", h))
		}
		if a.errs[1] == nil {
			for _, i := range a.schedule.Value() {
				if want := estimated.StatusOf(i.NeededBy(), i.CreditedBy(), i.Status() == estimated.StatusExcused); i.Status() != want {
					out = append(out, fmt.Sprintf("%s: installment %d is %v, want %v", h, i.Number(), i.Status(), want))
				}
			}
		}
		if a.errs[2] == nil && !a.penalty.Value().RatedThrough().IsZero() {
			// The interest stops at the last published rate, so the penalty
			// is no maximum: nothing may call it one, and the unpublished
			// rate is a question of its own.
			p := a.penalty.Value()
			if p.UpperBound() || strings.Contains(p.Summary(), "At most") {
				out = append(out, fmt.Sprintf("%s: the penalty stops at %v but is called a maximum: %q", h, p.RatedThrough(), p.Summary()))
			}
			asked := false
			for _, q := range a.penalty.Required() {
				asked = asked || q.Position() == working.PositionUnpublishedRate
			}
			if !asked {
				out = append(out, fmt.Sprintf("%s: the penalty stops at %v with no unpublished-rate question", h, p.RatedThrough()))
			}
			for _, t := range Texts(nil, []Answers{a}) {
				if strings.Contains(strings.ToLower(t.text), "at most") {
					out = append(out, fmt.Sprintf("%s: the penalty stops at %v, yet %s says %q", h, p.RatedThrough(), t.where, t.text))
				}
			}
		}
		if a.errs[0] == nil {
			// An effect agrees with the options in the same result: one
			// that says the target comes from last year needs last year's
			// option open, and one that says there's no target needs none.
			r := a.requirement.Value()
			lastOpen := false
			for _, o := range r.Options() {
				lastOpen = lastOpen || (o.Available() && (o.Basis() == estimated.BasisPriorYearTax || o.Basis() == estimated.BasisPriorYearIncome))
			}
			for _, q := range a.requirement.Required() {
				e := strings.ToLower(q.Effect())
				switch {
				case strings.Contains(e, "target can only come from last year") && !lastOpen:
					out = append(out, fmt.Sprintf("%s: %q, but last year's option isn't open", h, q.Effect()))
				case strings.Contains(e, "there's no target") && r.TargetKnown():
					out = append(out, fmt.Sprintf("%s: %q, but there is a target", h, q.Effect()))
				}
			}
		}
		if a.errs[0] == nil && a.requirement.Value().Exempt() {
			// An exception sets the target, so no answer changes it.
			for _, q := range a.requirement.Required() {
				if strings.Contains(strings.ToLower(q.Effect()), "target") {
					out = append(out, fmt.Sprintf("%s: an exception sets the target, yet a question says %q", h, q.Effect()))
				}
			}
		}
		if a.errs[2] == nil {
			// If an eligible waiver is granted, what's left is the charges
			// of the kinds it keeps; one that says it leaves the interest
			// keeps the interest.
			if o, ok := a.penalty.Value().WaiverOutcome(); ok && o.Eligible() {
				var left money.USCents
				for _, l := range a.penalty.Value().Lines() {
					for _, part := range l.Parts() {
						for _, k := range o.Kept() {
							if part.Kind() == k {
								left += part.Charge()
							}
						}
					}
				}
				keepsInterest := false
				for _, k := range o.Kept() {
					keepsInterest = keepsInterest || k == estimated.PartInterest
				}
				switch {
				case a.penalty.Value().IfWaived() != left:
					out = append(out, fmt.Sprintf("%s: if waived %v, but the charges the waiver keeps are %v", h, a.penalty.Value().IfWaived(), left))
				case strings.Contains(o.Reason(), "not the interest") && !keepsInterest:
					out = append(out, fmt.Sprintf("%s: %q, but the waiver leaves no interest", h, o.Reason()))
				}
			}
			// A waiver asked for, where a penalty arises, has an outcome.
			if k, answered := a.household.facts.Positions().Waiver(); answered && k != estimated.WaiverNone && a.penalty.Value().Total() > 0 {
				if _, ok := a.penalty.Value().WaiverOutcome(); !ok {
					out = append(out, fmt.Sprintf("%s: a %v waiver was asked for, and the penalty says nothing of it", h, k))
				}
			}
		}
		if a.errs[2] == nil {
			// A waiver asked for is reported on the penalty, which is
			// figured before it, wherever a penalty can arise.
			exempt := a.errs[0] == nil && a.requirement.Value().Exempt()
			if asked, answered := a.household.facts.Positions().Waiver(); answered && !exempt && a.penalty.Value().Waiver() != asked {
				out = append(out, fmt.Sprintf("%s: the %v waiver asked for is reported as %v", h, asked, a.penalty.Value().Waiver()))
			}
		}
		if a.errs[2] == nil && a.penalty.Value().RatedThrough().IsZero() {
			for _, q := range a.penalty.Required() {
				if q.Position() == working.PositionUnpublishedRate && strings.Contains(q.Effect(), "rate for the days after") {
					out = append(out, fmt.Sprintf("%s: %q, but the penalty doesn't record the last rated day", h, q.Effect()))
				}
			}
		}
		if a.errs[2] == nil {
			p := a.penalty.Value()
			switch {
			case p.Reason() == "":
				out = append(out, fmt.Sprintf("%s: the penalty has no reason", h))
			case r.Exempt() && (p.Total() != 0 || p.Reason() != estimated.ExceptionApplies(c)):
				out = append(out, fmt.Sprintf("%s: exempt under %q, but the penalty is %v because %q", h, c.Label(), p.Total(), p.Reason()))
			}
			// The reason counts the due dates the schedule shows short, and
			// so does ShortDueDates.
			short := 0
			if a.errs[1] == nil {
				for _, i := range a.schedule.Value() {
					if i.Status() == estimated.StatusShort {
						short++
					}
				}
				if p.ShortDueDates() != short {
					out = append(out, fmt.Sprintf("%s: the penalty counts %d short due dates, the schedule %d", h, p.ShortDueDates(), short))
				}
			}
			words := []string{"no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
			if n := p.ShortDueDates(); !r.Exempt() && n > 0 && n < len(words) && !strings.Contains(p.Reason(), "fall short on "+words[n]+" due date") {
				out = append(out, fmt.Sprintf("%s: %d due dates are short, but the reason is %q", h, n, p.Reason()))
			}
		}
	}
	return out
}

// washington rebuilds b with no withholding and every payment of the tax
// year moved to Oct 15 or later.
func washington(b *estimated.FactsBuilder, year int) *estimated.FactsBuilder {
	f, err := b.Build()
	if err != nil {
		return b
	}
	out := estimated.NewFacts(f.Jurisdiction(), f.TaxYear()).FilingStatus(f.FilingStatus()).ThisYear(f.ThisYear()).
		Residence(f.Residence()...).PriorResidence(f.PriorResidence()...).People(f.People()...).Positions(f.Positions())
	if p, ok := f.PriorYear(); ok {
		out.PriorYear(p)
	}
	if d := f.ReturnFiled(); !d.IsZero() {
		out.ReturnFiled(d)
	}
	if d := f.RequirementFirstMet(); !d.IsZero() {
		out.RequirementFirstMet(d)
	}
	oct15 := estimated.MustDate(year, time.October, 15)
	for _, p := range f.Payments() {
		on := p.On()
		if on.Before(oct15) {
			on = oct15
		}
		moved, err := estimated.NewPayment().On(on).Amount(p.Amount()).Build()
		if err == nil {
			out.Paid(moved)
		}
	}
	return out
}

// figure is an amount, a percentage or a date in a reading.
var figure = regexp.MustCompile(`\$[0-9][0-9,]*|[0-9][0-9.,/]*%|\b(Jan|Feb|Mar|Apr|May|Jun|Jul|Aug|Sep|Oct|Nov|Dec)[a-z]* [0-9]+\b`)

func figures(s string) string {
	set := map[string]bool{}
	for _, f := range figure.FindAllString(s, -1) {
		set[f] = true
	}
	var fs []string
	for f := range set {
		fs = append(fs, f)
	}
	sort.Strings(fs)
	return strings.Join(fs, ", ")
}

var notWords = regexp.MustCompile(`[^a-z0-9%$]+`)

// sameReadings says why a divergence's two readings agree, or "" when they
// differ: the same text, or the same figures with the statute's reading
// calling them "the same".
func sameReadings(d working.Divergence) string {
	norm := func(s string) string { return strings.TrimSpace(notWords.ReplaceAllString(strings.ToLower(s), " ")) }
	if norm(d.Form()) == norm(d.Statute()) {
		return "the form and the statute read alike"
	}
	if f := figures(d.Form()); f != "" && f == figures(d.Statute()) && strings.Contains(strings.ToLower(d.Statute()), "the same") {
		return fmt.Sprintf("both readings give %s, and the statute's calls them the same", f)
	}
	return ""
}
