// Package v1_test checks the ReturnInputs, ReturnOutputs, and ReturnPreparer
// schemas against the first-slice example fixture, verifies that every element
// of returns/v1 is documented, and enforces schema design invariants (Go-style
// initialism capitalization, sensitive fields confined to identity.proto, and
// common fields 1-5 on all jurisdiction return messages).
package v1_test

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io"
	"strings"
	"testing"
	"unicode"

	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
)

// The schema sources, for the documentation check, and the first-slice fixture.
//
//go:embed *.proto testdata/first_slice.jsonc
var files embed.FS

// importPrefix is what the proto_library's import_prefix puts before the
// module-relative path of a hand-written file.
const importPrefix = "taxcalcs/"

// strict parses strictly: an unknown field is an error.
var strict = protojson.UnmarshalOptions{DiscardUnknown: false}

// TestFirstSliceParsesStrictly parses testdata/first_slice.jsonc and verifies
// that key household, document, deduction, prior-year, election, and state
// fields deserialize cleanly.
func TestFirstSliceParsesStrictly(t *testing.T) {
	in := readFirstSlice(t)
	if in.GetSchemaVersion() != 1 || in.GetTaxYear() != 2025 {
		t.Fatalf("schema_version = %d, tax_year = %d; want 1, 2025", in.GetSchemaVersion(), in.GetTaxYear())
	}
	hh := in.GetFederal().GetHousehold()
	if hh.GetTaxpayer().GetId() != "alex" || hh.GetSpouse().GetId() != "sam" {
		t.Errorf("taxpayer = %q, spouse = %q; want alex, sam", hh.GetTaxpayer().GetId(), hh.GetSpouse().GetId())
	}
	if len(hh.GetDependents()) != 1 || hh.GetDependents()[0].GetId() != "maya" {
		t.Errorf("dependents = %v; want 1 dependent (maya)", hh.GetDependents())
	}
	w2s := in.GetFederal().GetDocuments().GetW2S()
	if len(w2s) != 2 {
		t.Fatalf("len(w2s) = %d; want 2", len(w2s))
	}
	if got := w2s[0].GetForm().GetWagesTipsOtherCompensation().GetMinor(); got != 18500000 {
		t.Errorf("first W-2 wages = %d minor units; want 18500000", got)
	}
	if len(in.GetStates()) != 1 || in.GetStates()[0].GetState() != "CA" || in.GetStates()[0].GetCa() == nil {
		t.Errorf("states = %v; want 1 CA state input", in.GetStates())
	}
}

// TestFirstSliceRoundTrip verifies wire and JSON round-tripping of ReturnInputs.
func TestFirstSliceRoundTrip(t *testing.T) {
	in := readFirstSlice(t)

	wireBytes, err := proto.Marshal(in)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}
	fromWire := &returnsv1.ReturnInputs{}
	if err := proto.Unmarshal(wireBytes, fromWire); err != nil {
		t.Fatalf("proto.Unmarshal: %v", err)
	}
	if !proto.Equal(in, fromWire) {
		t.Error("ReturnInputs changed after binary wire round-trip")
	}

	jsonBytes, err := protojson.Marshal(in)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	fromJSON := &returnsv1.ReturnInputs{}
	if err := strict.Unmarshal(jsonBytes, fromJSON); err != nil {
		t.Fatalf("protojson.Unmarshal: %v", err)
	}
	if !proto.Equal(in, fromJSON) {
		t.Error("ReturnInputs changed after JSON round-trip")
	}
}

// TestUnknownFieldIsRefused verifies that unknown or computed fields in JSON
// are rejected by strict unmarshaling.
func TestUnknownFieldIsRefused(t *testing.T) {
	text := strings.Replace(
		string(stripComments(t, firstSliceText(t))),
		`"taxYear": 2025,`,
		`"taxYear": 2025, "computedTax": 123,`,
		1,
	)
	if !strings.Contains(text, "computedTax") {
		t.Fatal("failed to inject unknown field into fixture")
	}
	if err := strict.Unmarshal([]byte(text), &returnsv1.ReturnInputs{}); err == nil {
		t.Error("strict unmarshaler accepted an unknown field")
	}
}

// TestProtosAreDocumented fails if any message, field, oneof, enum, enum value,
// service, or method in returns/v1 lacks a leading comment.
func TestProtosAreDocumented(t *testing.T) {
	entries, err := files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	sources := map[string][]byte{}
	var paths []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".proto") {
			continue
		}
		b, err := files.ReadFile(e.Name())
		if err != nil {
			t.Fatal(err)
		}
		p := importPrefix + "returns/v1/" + e.Name()
		sources[p] = b
		paths = append(paths, p)
	}
	if len(paths) < 23 {
		t.Fatalf("found %d proto files, want at least 23: %v", len(paths), paths)
	}
	c := protocompile.Compiler{
		SourceInfoMode: protocompile.SourceInfoStandard,
		Resolver: protocompile.ResolverFunc(func(p string) (protocompile.SearchResult, error) {
			if b, ok := sources[p]; ok {
				return protocompile.SearchResult{Source: io.NopCloser(bytes.NewReader(b))}, nil
			}
			fd, err := protoregistry.GlobalFiles.FindFileByPath(p)
			if err != nil {
				return protocompile.SearchResult{}, fmt.Errorf("%s: %w", p, err)
			}
			return protocompile.SearchResult{Desc: fd}, nil
		}),
	}
	compiled, err := c.Compile(context.Background(), paths...)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range compiled {
		checkDocumented(t, f)
		checkGoStyleInitialisms(t, f)
	}
	for _, p := range paths {
		regFD, err := protoregistry.GlobalFiles.FindFileByPath(p)
		if err != nil {
			t.Fatalf("protoregistry.GlobalFiles.FindFileByPath(%q): %v", p, err)
		}
		checkSensitiveOnlyInIdentity(t, regFD)
	}
}

// TestJurisdictionReturnsShareCommonFields verifies that FederalReturn,
// CaliforniaReturn, OregonReturn, WashingtonReturn, and UnsupportedStateReturn
// all share fields 1-5 (status, summary, diagnostics, elections, amendment).
func TestJurisdictionReturnsShareCommonFields(t *testing.T) {
	returns := []protoreflect.MessageDescriptor{
		(&returnsv1.FederalReturn{}).ProtoReflect().Descriptor(),
		(&returnsv1.CaliforniaReturn{}).ProtoReflect().Descriptor(),
		(&returnsv1.OregonReturn{}).ProtoReflect().Descriptor(),
		(&returnsv1.WashingtonReturn{}).ProtoReflect().Descriptor(),
		(&returnsv1.UnsupportedStateReturn{}).ProtoReflect().Descriptor(),
	}
	wantFields := []struct {
		num  protoreflect.FieldNumber
		name protoreflect.Name
	}{
		{1, "status"},
		{2, "summary"},
		{3, "diagnostics"},
		{4, "elections"},
		{5, "amendment"},
	}
	for _, md := range returns {
		for _, wf := range wantFields {
			fd := md.Fields().ByNumber(wf.num)
			if fd == nil {
				t.Errorf("%s is missing field %d (%s)", md.FullName(), wf.num, wf.name)
				continue
			}
			if fd.Name() != wf.name {
				t.Errorf("%s field %d has name %q; want %q", md.FullName(), wf.num, fd.Name(), wf.name)
			}
		}
	}
}

// checkGoStyleInitialisms ensures that message and enum type names in
// returns/v1 capitalize acronyms in Go style (e.g. TaxpayerID, IPPIN,
// Form1099INTDocument, QDCGTWorksheet, IRABasis, IRSAdjustment) rather than
// title-casing them (Id, Ippin, Ira, Irs, Ssa, Hsa, Amt, Nol).
func checkGoStyleInitialisms(t *testing.T, fd protoreflect.FileDescriptor) {
	t.Helper()
	badTokens := map[string]string{
		"Id":    "ID",
		"Ippin": "IPPIN",
		"Pin":   "PIN",
		"Tin":   "TIN",
		"Ssn":   "SSN",
		"Itin":  "ITIN",
		"Ein":   "EIN",
		"Ira":   "IRA",
		"Irs":   "IRS",
		"Ssa":   "SSA",
		"Hsa":   "HSA",
		"Hdhp":  "HDHP",
		"Amt":   "AMT",
		"Nol":   "NOL",
		"Qbi":   "QBI",
		"Qdcgt": "QDCGT",
		"Eic":   "EIC",
		"Aotc":  "AOTC",
		"Oid":   "OID",
		"Nec":   "NEC",
		"Misc":  "MISC",
		"Div":   "DIV",
		"Int":   "INT",
	}
	checkName := func(d protoreflect.Descriptor) {
		name := string(d.Name())
		for _, word := range splitCamelWords(name) {
			if want, bad := badTokens[word]; bad {
				t.Errorf("%s: %s uses non-Go initialism %q in type name %q; want %q", fd.Path(), d.FullName(), word, name, want)
			}
		}
	}
	var walkMsgs func(protoreflect.MessageDescriptors)
	walkMsgs = func(ms protoreflect.MessageDescriptors) {
		for i := 0; i < ms.Len(); i++ {
			md := ms.Get(i)
			if md.IsMapEntry() {
				continue
			}
			checkName(md)
			for j := 0; j < md.Enums().Len(); j++ {
				checkName(md.Enums().Get(j))
			}
			walkMsgs(md.Messages())
		}
	}
	walkMsgs(fd.Messages())
	for i := 0; i < fd.Enums().Len(); i++ {
		checkName(fd.Enums().Get(i))
	}
}

// splitCamelWords splits a PascalCase identifier into its title-cased or
// all-caps words.
func splitCamelWords(s string) []string {
	runes := []rune(s)
	var words []string
	start := 0
	for i := 1; i < len(runes); i++ {
		prev := runes[i-1]
		curr := runes[i]
		if unicode.IsLower(prev) && unicode.IsUpper(curr) {
			words = append(words, string(runes[start:i]))
			start = i
		} else if i+1 < len(runes) && unicode.IsUpper(prev) && unicode.IsUpper(curr) && unicode.IsLower(runes[i+1]) {
			words = append(words, string(runes[start:i]))
			start = i
		}
	}
	if start < len(runes) {
		words = append(words, string(runes[start:]))
	}
	return words
}

// checkSensitiveOnlyInIdentity ensures that `sensitive: true` in InputSpec
// options only appears in identity.proto.
func checkSensitiveOnlyInIdentity(t *testing.T, fd protoreflect.FileDescriptor) {
	t.Helper()
	isIdentity := strings.HasSuffix(fd.Path(), "/identity.proto")
	var walkMsgs func(protoreflect.MessageDescriptors)
	walkMsgs = func(ms protoreflect.MessageDescriptors) {
		for i := 0; i < ms.Len(); i++ {
			md := ms.Get(i)
			for j := 0; j < md.Fields().Len(); j++ {
				field := md.Fields().Get(j)
				opts := field.Options()
				if opts == nil {
					continue
				}
				spec, ok := proto.GetExtension(opts, returnsv1.E_Input).(*returnsv1.InputSpec)
				if ok && spec != nil && spec.GetSensitive() && !isIdentity {
					t.Errorf("%s: field %s marks sensitive=true outside identity.proto", fd.Path(), field.FullName())
				}
			}
			walkMsgs(md.Messages())
		}
	}
	walkMsgs(fd.Messages())
}

func readFirstSlice(t *testing.T) *returnsv1.ReturnInputs {
	t.Helper()
	in := &returnsv1.ReturnInputs{}
	if err := strict.Unmarshal(stripComments(t, firstSliceText(t)), in); err != nil {
		t.Fatalf("parse testdata/first_slice.jsonc: %v", err)
	}
	return in
}

func firstSliceText(t *testing.T) []byte {
	t.Helper()
	b, err := files.ReadFile("testdata/first_slice.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func stripComments(t *testing.T, in []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	for i := 0; i < len(in); i++ {
		switch {
		case in[i] == '"':
			end := stringEnd(in, i)
			out.Write(in[i:end])
			i = end - 1
		case bytes.HasPrefix(in[i:], []byte("//")):
			for i < len(in) && in[i] != '\n' {
				i++
			}
			if i < len(in) {
				out.WriteByte('\n')
			}
		case bytes.HasPrefix(in[i:], []byte("/*")):
			end := bytes.Index(in[i+2:], []byte("*/"))
			if end < 0 {
				t.Fatal("a /* comment is not closed")
			}
			out.Write(bytes.Repeat([]byte("\n"), bytes.Count(in[i:i+2+end+2], []byte("\n"))))
			i += 2 + end + 1
		default:
			out.WriteByte(in[i])
		}
	}
	return out.Bytes()
}

func stringEnd(in []byte, start int) int {
	for i := start + 1; i < len(in); i++ {
		switch in[i] {
		case '\\':
			i++
		case '"':
			return i + 1
		}
	}
	return len(in)
}

func checkDocumented(t *testing.T, fd protoreflect.FileDescriptor) {
	t.Helper()
	locs := fd.SourceLocations()
	check := func(d protoreflect.Descriptor) {
		if strings.TrimSpace(locs.ByDescriptor(d).LeadingComments) == "" {
			t.Errorf("%s: %s has no doc comment", fd.Path(), d.FullName())
		}
	}
	var enums func(protoreflect.EnumDescriptors)
	enums = func(es protoreflect.EnumDescriptors) {
		for i := 0; i < es.Len(); i++ {
			check(es.Get(i))
			for j := 0; j < es.Get(i).Values().Len(); j++ {
				check(es.Get(i).Values().Get(j))
			}
		}
	}
	exts := func(xs protoreflect.ExtensionDescriptors) {
		for i := 0; i < xs.Len(); i++ {
			check(xs.Get(i))
		}
	}
	var messages func(protoreflect.MessageDescriptors)
	messages = func(ms protoreflect.MessageDescriptors) {
		for i := 0; i < ms.Len(); i++ {
			md := ms.Get(i)
			if md.IsMapEntry() {
				continue
			}
			check(md)
			for j := 0; j < md.Fields().Len(); j++ {
				check(md.Fields().Get(j))
			}
			for j := 0; j < md.Oneofs().Len(); j++ {
				if !md.Oneofs().Get(j).IsSynthetic() {
					check(md.Oneofs().Get(j))
				}
			}
			enums(md.Enums())
			exts(md.Extensions())
			messages(md.Messages())
		}
	}
	services := func(ss protoreflect.ServiceDescriptors) {
		for i := 0; i < ss.Len(); i++ {
			sd := ss.Get(i)
			check(sd)
			for j := 0; j < sd.Methods().Len(); j++ {
				check(sd.Methods().Get(j))
			}
		}
	}
	messages(fd.Messages())
	enums(fd.Enums())
	exts(fd.Extensions())
	services(fd.Services())
}
