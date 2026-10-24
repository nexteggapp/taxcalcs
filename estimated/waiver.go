package estimated

import (
	"errors"
	"fmt"
	"strings"
)

// WaiverOutcome is what a jurisdiction's rules make of a waiver asked for:
// whether the facts given meet its conditions, and why. The tax authority
// decides; an eligible waiver is one it may grant, so the penalty is still
// figured in full and [Penalty.IfWaived] shows what would remain.
type WaiverOutcome struct {
	state  waiverState
	reason string
	kept   []PartKind
}

type waiverState int

const (
	waiverUnset waiverState = iota
	waiverEligible
	waiverRefused
	waiverUndecided
)

// WaiverEligible is a waiver whose conditions the facts meet.
func WaiverEligible(reason string) WaiverOutcome {
	return WaiverOutcome{state: waiverEligible, reason: reason}
}

// WaiverEligibleExcept is a waiver whose conditions the facts meet, which
// covers every charge but those of the kinds kept: Washington's waiver
// lifts the late-payment penalty but not the interest, so PartInterest is
// kept.
func WaiverEligibleExcept(reason string, kept ...PartKind) WaiverOutcome {
	return WaiverOutcome{waiverEligible, reason, append([]PartKind(nil), kept...)}
}

// Kept are the kinds of charge an eligible waiver leaves due.
func (o WaiverOutcome) Kept() []PartKind { return append([]PartKind(nil), o.kept...) }

// WaiverRefused is a waiver whose conditions the facts don't meet, or that
// the jurisdiction doesn't grant.
func WaiverRefused(reason string) WaiverOutcome {
	return WaiverOutcome{state: waiverRefused, reason: reason}
}

// WaiverUndecided is a waiver that needs a fact not given, such as the date
// of retirement.
func WaiverUndecided(reason string) WaiverOutcome {
	return WaiverOutcome{state: waiverUndecided, reason: reason}
}

func (o WaiverOutcome) Eligible() bool  { return o.state == waiverEligible }
func (o WaiverOutcome) Refused() bool   { return o.state == waiverRefused }
func (o WaiverOutcome) Undecided() bool { return o.state == waiverUndecided }

// Reason says why in plain words, in one or two sentences.
func (o WaiverOutcome) Reason() string { return o.reason }

func (o WaiverOutcome) check() error {
	switch {
	case o.state == waiverUnset:
		return errors.New("estimated: a waiver outcome must be eligible, refused or undecided")
	case o.reason == "":
		return errors.New("estimated: a waiver outcome must say why")
	case strings.Count(strings.TrimSuffix(o.reason, "."), ". ") > 1:
		return fmt.Errorf("estimated: a waiver outcome's reason is at most two sentences, not %q", o.reason)
	}
	return nil
}

// yearsOld is the age on d of someone born on born.
func yearsOld(born, d Date) int {
	age := d.year - born.year
	if d.month < born.month || (d.month == born.month && d.day < born.day) {
		age--
	}
	return age
}

// StandardWaiver decides the two waivers federal law and many states grant,
// with the conditions as 26 U.S.C. § 6654(e)(3) and the Form 2210
// instructions state them:
//   - a casualty, disaster or other unusual circumstance that makes the
//     penalty inequitable, which only the authority can judge;
//   - retirement after reaching minAge, or disability, in the tax year or
//     the year before, with reasonable cause, which only the authority can
//     judge.
//
// authority names who decides, as "the IRS". The reason says whether the
// waiver can apply, what it needs, and who decides. It reports false when
// no waiver was asked for.
func StandardWaiver(f Facts, authority string, minAge int) (WaiverOutcome, bool) {
	p := f.Positions()
	kind, answered := p.Waiver()
	if !answered || kind == WaiverNone {
		return WaiverOutcome{}, false
	}
	year := f.TaxYear()
	window := fmt.Sprintf("%d or %d", year-1, year)
	if kind == WaiverCasualty {
		return WaiverEligible(fmt.Sprintf("%s can waive all or part of the penalty. It must be for %s that made it unfair.", capital(authority), causeWords(p.WaiverCause()))), true
	}
	on := p.WaiverOn()
	switch p.WaiverCause() {
	case CauseDisability:
		if on.IsZero() {
			return WaiverUndecided(fmt.Sprintf("The disability waiver needs the date you became disabled, in %s.", window)), true
		}
		if on.year != year && on.year != year-1 {
			return WaiverRefused(fmt.Sprintf("You became disabled in %d; the waiver needs it to be in %s.", on.year, window)), true
		}
		return WaiverEligible(fmt.Sprintf("You became disabled in %d, so the penalty can be waived. %s needs reasonable cause for the shortfall.", on.year, capital(authority))), true
	default:
		if on.IsZero() {
			return WaiverUndecided(fmt.Sprintf("The retirement waiver needs the date you retired, in %s.", window)), true
		}
		if on.year != year && on.year != year-1 {
			return WaiverRefused(fmt.Sprintf("You retired in %d, and the waiver covers retiring in %s.", on.year, window)), true
		}
		born := p.Born()
		if born.IsZero() {
			return WaiverUndecided(fmt.Sprintf("The retirement waiver needs your date of birth, to show you'd reached %d.", minAge)), true
		}
		if age := yearsOld(born, on); age < minAge {
			return WaiverRefused(fmt.Sprintf("You were %d when you retired, and the waiver needs you to have reached %d.", age, minAge)), true
		}
		return WaiverEligible(fmt.Sprintf("You retired in %d after reaching %d, so the penalty can be waived. %s needs reasonable cause for the shortfall.", on.year, minAge, capital(authority))), true
	}
}

func causeWords(c WaiverCause) string {
	switch c {
	case CauseCasualty:
		return "a casualty"
	case CauseDisaster:
		return "a disaster"
	default:
		return "a casualty, disaster or other unusual event"
	}
}

func capital(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
