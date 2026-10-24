package working

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
)

// Every type in this file has unexported fields and accessors that return
// copies, and is made by a builder whose Build validates. A result cannot
// change after the calculator that made it has returned it.

// Rule is a published rule and what applying it to the caller's figures did.
//
// Every calculator returns these so that a product can show its working.
type Rule struct {
	cite    string
	applied string
}

// Cite names the authority, as "26 U.S.C. § 1(j)(2)(C)",
// "Rev. Proc. 2025-32 § 3.14(3)" or "RCW 82.87.040(1)".
func (r Rule) Cite() string { return r.cite }

// Applied says what the rule did, in the caller's own numbers.
func (r Rule) Applied() string { return r.applied }

// RuleBuilder builds a [Rule].
type RuleBuilder struct{ r Rule }

// NewRule starts a rule citing an authority.
func NewRule(cite string) *RuleBuilder { return &RuleBuilder{r: Rule{cite: cite}} }

// Applied records what the rule did.
func (b *RuleBuilder) Applied(text string) *RuleBuilder {
	b.r.applied = text
	return b
}

// Build requires both the citation and what the rule did. A rule with
// either missing is one nobody can check.
func (b *RuleBuilder) Build() (Rule, error) {
	switch {
	case b.r.cite == "":
		return Rule{}, errors.New("working: a rule needs the authority it cites")
	case b.r.applied == "":
		return Rule{}, fmt.Errorf("working: the rule citing %s needs to say what it did", b.r.cite)
	}
	return b.r, nil
}

// Required names a position the caller has not supplied.
//
// A calculator returns one instead of guessing. What it means is the caller's
// business: a product asks the user, a script reads a configuration, a
// preparer's tool renders a form field.
type Required struct {
	position PositionKind
	because  string
	question string
	effect   string
	asked    bool
}

// Position is the kind of choice or fact the caller owes.
func (r Required) Position() PositionKind { return r.position }

// Because states the rule that makes this a choice rather than arithmetic,
// and the default the calculator used instead. For a position made with
// [Notes.Ask] it is the question and the effect together.
func (r Required) Because() string { return r.because }

// Question is a question the person can answer, as "What do you expect your
// 2026 California tax to be?", or empty when the position is not theirs to
// answer, as a rate not yet published. Set only through [Notes.Ask].
func (r Required) Question() string { return r.question }

// Effect is one or two plain sentences saying what the position changes, as
// "Without it, the target can only come from last year's tax." Set only
// through [Notes.Ask].
func (r Required) Effect() string { return r.effect }

// Asked reports that the position was made with [Notes.Ask], so it has an
// effect and, where the person can answer it, a question.
func (r Required) Asked() bool { return r.asked }

// RequiredBuilder builds a [Required].
type RequiredBuilder struct{ r Required }

// NewRequired starts a position of the given kind.
func NewRequired(position PositionKind) *RequiredBuilder {
	return &RequiredBuilder{r: Required{position: position}}
}

// Because records the rule that makes it a position.
func (b *RequiredBuilder) Because(text string) *RequiredBuilder {
	b.r.because = text
	return b
}

// Build requires a known kind and a reason. A position made with
// [Notes.Ask] also needs its effect, and a question, if it has one, that
// ends with a question mark.
func (b *RequiredBuilder) Build() (Required, error) {
	switch {
	case b.r.position == PositionUnspecified:
		return Required{}, errors.New("working: a required position needs its kind")
	case b.r.because == "":
		return Required{}, fmt.Errorf("working: the %v position needs the rule that makes it one", b.r.position)
	case b.r.asked && b.r.effect == "":
		return Required{}, fmt.Errorf("working: the %v position needs its effect", b.r.position)
	case b.r.asked && b.r.question != "" && !strings.HasSuffix(b.r.question, "?"):
		return Required{}, fmt.Errorf("working: %q is not a question", b.r.question)
	}
	return b.r, nil
}

// Result is a computed figure, the rules that produced it, and the positions
// the caller still owes.
//
// A non-empty Required does not mean Value is wrong. It means Value rests on
// the calculator's stated default, and the caller can change the answer by
// supplying the position.
type Result[T any] struct {
	value       T
	applied     []Rule
	required    []Required
	divergences []Divergence
}

// Value is the figure. A slice or map is returned as a copy; the element
// types every calculator here returns are themselves immutable.
func (r Result[T]) Value() T { return copyOf(r.value) }

// Applied are the rules that produced the figure, in the order applied.
func (r Result[T]) Applied() []Rule { return append([]Rule(nil), r.applied...) }

// Required are the positions the caller has not supplied, each with the
// default used instead.
func (r Result[T]) Required() []Required { return append([]Required(nil), r.required...) }

// Divergences are the places where the authority's form and its statute
// disagree about a rule this result used. The figure follows the form, and
// each entry says what the statute says instead. Most calculators never set
// it.
func (r Result[T]) Divergences() []Divergence { return append([]Divergence(nil), r.divergences...) }

// Needs reports whether the result depends on a position the caller has not
// supplied.
func (r Result[T]) Needs() bool { return len(r.required) > 0 }

// Notes are the rules, positions and divergences behind a figure that is not
// yet known.
//
// A calculator that answers several questions from one assessment collects
// its working here first, then hands it to each [ResultBuilder] with
// [ResultBuilder.With]. Notes is part of a builder: it is checked when that
// builder's Build runs, not before.
type Notes struct {
	rules       []Rule
	required    []Required
	divergences []Divergence
}

// Rule records that the rule citing cite did what applied says.
func (n *Notes) Rule(cite, applied string) *Notes {
	n.rules = append(n.rules, Rule{cite: cite, applied: applied})
	return n
}

// Applied records rules already built, as those of another result.
func (n *Notes) Applied(rules ...Rule) *Notes {
	n.rules = append(n.rules, rules...)
	return n
}

// Require records a position the caller owes, and the rule that makes it
// one.
func (n *Notes) Require(position PositionKind, because string) *Notes {
	n.required = append(n.required, Required{position: position, because: because})
	return n
}

// Ask records a position as a question the person can answer and the
// effect of answering it. question is empty when the position is not the
// person's to answer, as a rate not yet published or a rule the authority
// leaves open; effect is one or two sentences.
func (n *Notes) Ask(position PositionKind, question, effect string) *Notes {
	n.required = append(n.required, Required{position: position, because: strings.TrimSpace(question + " " + effect),
		question: question, effect: effect, asked: true})
	return n
}

// Required records positions already built, as those of another result.
func (n *Notes) Required(rs ...Required) *Notes {
	n.required = append(n.required, rs...)
	return n
}

// Diverges records where a form and its statute disagree on a rule used.
func (n *Notes) Diverges(ds ...Divergence) *Notes {
	n.divergences = append(n.divergences, ds...)
	return n
}

// Len is how many rules have been recorded.
func (n *Notes) Len() int { return len(n.rules) }

// check validates every entry, as [RuleBuilder.Build] and
// [RequiredBuilder.Build] would.
func (n *Notes) check() error {
	for _, r := range n.rules {
		if _, err := (&RuleBuilder{r: r}).Build(); err != nil {
			return err
		}
	}
	for _, r := range n.required {
		if _, err := (&RequiredBuilder{r: r}).Build(); err != nil {
			return err
		}
	}
	for _, d := range n.divergences {
		if _, err := (&DivergenceBuilder{d: d}).Build(); err != nil {
			return err
		}
	}
	return nil
}

// ResultBuilder builds a [Result].
type ResultBuilder[T any] struct {
	value T
	notes Notes
}

// NewResult starts a result for a figure.
func NewResult[T any](value T) *ResultBuilder[T] {
	return &ResultBuilder[T]{value: value}
}

// Rule records that the rule citing cite did what applied says.
func (b *ResultBuilder[T]) Rule(cite, applied string) *ResultBuilder[T] {
	b.notes.Rule(cite, applied)
	return b
}

// Applied records rules already built, as those of another result.
func (b *ResultBuilder[T]) Applied(rules ...Rule) *ResultBuilder[T] {
	b.notes.Applied(rules...)
	return b
}

// Require records a position the caller owes.
func (b *ResultBuilder[T]) Require(position PositionKind, because string) *ResultBuilder[T] {
	b.notes.Require(position, because)
	return b
}

// Ask records a position as a question and its effect, as [Notes.Ask].
func (b *ResultBuilder[T]) Ask(position PositionKind, question, effect string) *ResultBuilder[T] {
	b.notes.Ask(position, question, effect)
	return b
}

// Required records positions already built, as those of another result.
func (b *ResultBuilder[T]) Required(rs ...Required) *ResultBuilder[T] {
	b.notes.Required(rs...)
	return b
}

// Diverges records where a form and its statute disagree on a rule used.
func (b *ResultBuilder[T]) Diverges(ds ...Divergence) *ResultBuilder[T] {
	b.notes.Diverges(ds...)
	return b
}

// With adds everything in n after what is recorded so far. Later changes to
// n do not reach this builder.
func (b *ResultBuilder[T]) With(n Notes) *ResultBuilder[T] {
	b.notes.Applied(n.rules...)
	b.notes.Required(n.required...)
	b.notes.Diverges(n.divergences...)
	return b
}

// Build checks every rule, position and divergence, and returns a result
// nothing can change.
func (b *ResultBuilder[T]) Build() (Result[T], error) {
	if err := b.notes.check(); err != nil {
		return Result[T]{}, err
	}
	return Result[T]{
		value:       copyOf(b.value),
		applied:     append([]Rule(nil), b.notes.rules...),
		required:    append([]Required(nil), b.notes.required...),
		divergences: append([]Divergence(nil), b.notes.divergences...),
	}, nil
}

// copyOf returns v, with a slice or map copied one level deep so that a
// caller holding the result and a caller holding the value do not share
// storage.
func copyOf[T any](v T) T {
	rv := reflect.ValueOf(&v).Elem()
	switch rv.Kind() {
	case reflect.Slice:
		if rv.IsNil() {
			return v
		}
		c := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		reflect.Copy(c, rv)
		return c.Interface().(T)
	case reflect.Map:
		if rv.IsNil() {
			return v
		}
		c := reflect.MakeMapWithSize(rv.Type(), rv.Len())
		for it := rv.MapRange(); it.Next(); {
			c.SetMapIndex(it.Key(), it.Value())
		}
		return c.Interface().(T)
	}
	return v
}
