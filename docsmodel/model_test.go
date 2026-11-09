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
}
