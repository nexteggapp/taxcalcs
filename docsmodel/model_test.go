package main

import (
	"testing"
)

func TestModel(t *testing.T) {
	m, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Problems {
		t.Error(p)
	}
	var form *Rule
	for _, r := range m.Rules {
		if r.Name == "ca-form-540" {
			form = r
		}
	}
	if form == nil || len(form.Parts) < 5 {
		t.Fatalf("ca-form-540 has parts %v", form)
	}
	if got := m.Households[0].TotalTax; got != "$522.69" {
		t.Errorf("the single renter's total tax is %s, want $522.69", got)
	}

	var fed *Rule
	for _, r := range m.Rules {
		if r.Name == "fed-form-1040" {
			fed = r
		}
	}
	if fed == nil || len(fed.Parts) < 5 {
		t.Fatalf("fed-form-1040 has parts %v", fed)
	}
	// Taxable income of $24,250 after the $15,750 standard deduction.
	want := map[string]string{"us-single-40k": "$2,672.00", "us-retired-couple": "$0.00"}
	for _, h := range m.Households {
		if h.Form != "fed-form-1040" {
			continue
		}
		lines := 0
		for _, c := range h.Derivation.Children {
			if c.Kind == "line" {
				lines++
			}
		}
		if lines < 40 {
			t.Errorf("%s: the derivation of Form 1040 has %d lines", h.ID, lines)
		}
		if w, ok := want[h.ID]; ok && h.TotalTax != w {
			t.Errorf("%s: total tax is %s, want %s", h.ID, h.TotalTax, w)
		}
		delete(want, h.ID)
	}
	if len(want) > 0 {
		t.Errorf("no federal households %v", want)
	}
}
