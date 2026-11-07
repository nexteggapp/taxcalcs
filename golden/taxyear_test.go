package golden

// The taxyear vectors: for every TaxYearScenarios file the taxyear tests
// read, what taxyear.Parse gives (the message, or its error's text), and
// what taxyear.Compute gives for it. taxyear_noworking_test.go, which
// builds only on feat/wasm-entry, adds Compute through a cache that leaves
// out the engine's working. Parse's error cases are the ones taxyear's own tests
// break the doc example with, and more that reach each of protojson's
// refusals and each JSON form it accepts. The cache vectors compute
// cache_test.go's edits of the example, one after another, through two
// caches, one small enough to drop what it holds, and record each result.
//
// Go's protobuf library writes "proto:" and a space before its errors, and
// picks per build whether that space is U+0020 or U+00A0 so that no one
// compares the text. The vectors record U+0020.

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
	"github.com/nexteggapp/taxcalcs/taxyear"
	commonv1 "github.com/nexteggapp/taxprotos/go/common/v1"
)

// taxyearFile is one file's Parse and Compute.
type taxyearFile struct {
	// File is the path from the repository root.
	File       string `json:"file"`
	Parsed     string `json:"parsed"`
	ParseError string `json:"parse_error"`
	// Result is Compute's TaxYearResult.
	Result       string `json:"result"`
	ComputeError string `json:"compute_error"`
}

// taxyearParse is Parse of the example with one edit: Old replaced by New
// once, or the whole text when Text is set.
type taxyearParse struct {
	Name string `json:"name"`
	Old  string `json:"old"`
	New  string `json:"new"`
	Text string `json:"text"`
	// TextBytes is Text in base64 when it is not UTF-8, which JSON can't
	// hold.
	TextBytes string `json:"text_bytes,omitempty"`
	Parsed    string `json:"parsed"`
	Error     string `json:"error"`
}

// taxyearCache is one of cache_test.go's variants, computed in turn through
// two caches that keep their work from the variants before.
type taxyearCache struct {
	Name  string `json:"name"`
	Old   string `json:"old"`
	New   string `json:"new"`
	Small string `json:"small"`
	Large string `json:"large"`
}

// protoText is an error's text with the protobuf library's space fixed.
func protoText(err error) string {
	return strings.ReplaceAll(errText(err), "proto: ", "proto: ")
}

func taxyearFiles(t *testing.T) []string {
	files := []string{"scenarios/v1/testdata/example.jsonc", "taxyear/testdata/example.jsonc"}
	for _, dir := range []string{"taxyear/testdata/cases", "taxyear/testdata/fixtures"} {
		entries, err := os.ReadDir(filepath.Join("..", dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jsonc") {
				files = append(files, dir+"/"+e.Name())
			}
		}
	}
	return files
}

func TestTaxYear(t *testing.T) {
	out := []taxyearFile{}
	for _, path := range taxyearFiles(t) {
		text, err := os.ReadFile(filepath.Join("..", path))
		if err != nil {
			t.Fatal(err)
		}
		v := taxyearFile{File: path}
		file, err := taxyear.Parse(text)
		v.ParseError = protoText(err)
		if err != nil {
			out = append(out, v)
			continue
		}
		v.Parsed = encode(t, file)
		r, err := taxyear.Compute(file)
		v.ComputeError = errText(err)
		if err == nil {
			v.Result = encode(t, r.Message())
		}
		out = append(out, v)
	}
	checkGzip(t, "taxyear/files.json.gz", out)
}

// taxyearParseCases are the example's edits Parse refuses or reads.
var taxyearParseCases = []taxyearParse{
	// taxyear_test.go's.
	{Name: "unknown field", Old: `"taxYear": 2026,`, New: `"taxYear": 2026, "taxOwed": 1,`},
	{Name: "unspecified enum", Old: `"PAY_FREQUENCY_SEMI_MONTHLY"`, New: `"PAY_FREQUENCY_UNSPECIFIED"`},
	{Name: "empty oneof", Old: `"defaultPart": { "id": "acme-m9t3r7xe", "sameAsLatest": {} }`, New: `"defaultPart": { "id": "acme-m9t3r7xe" }`},
	// Comments.
	{Name: "unclosed comment", Old: `"shared": {`, New: `"shared": { /* not closed`},
	{Name: "block comment", Old: `"shared": {`, New: "/* a\nblock */ \"shared\": {"},
	{Name: "comment markers in a string", Old: `"name": "Acme Corp"`, New: `"name": "Acme // Corp /* x */"`},
	{Name: "line comment at the end", Text: `{"schemaVersion": 1} // no newline`},
	{Name: "comment then error", Old: `"taxYear": 2026,`, New: `/* x */ "taxYear": 2026, /* y */ "nope": 1,`},
	// Malformed JSON.
	{Name: "empty", Text: ``},
	{Name: "only a comment", Text: `// nothing`},
	{Name: "truncated", Text: `{"schemaVersion": 1, "shared": {"model": {`},
	{Name: "truncated in a string", Text: `{"exportedFrom": "abc`},
	{Name: "truncated after a name", Text: `{"exportedFrom"`},
	{Name: "trailing garbage", Text: `{"schemaVersion": 1} x`},
	{Name: "trailing object", Text: `{"schemaVersion": 1} {}`},
	{Name: "trailing comma", Old: `"exportedFrom": "y2026.41/d3",`, New: `"exportedFrom": "y2026.41/d3", }, "x": {`},
	{Name: "trailing comma in an object", Text: `{"schemaVersion": 1,}`},
	{Name: "trailing comma in a list", Text: `{"scenarios": [{"id": "plan"},]}`},
	{Name: "missing colon", Text: `{"schemaVersion" 1}`},
	{Name: "missing comma", Text: `{"schemaVersion": 1 "taxYear": 2026}`},
	{Name: "a list at the top", Text: `[]`},
	{Name: "a number at the top", Text: `1`},
	{Name: "single quotes", Text: `{'schemaVersion': 1}`},
	{Name: "bare word", Text: `{"exportedFrom": abc}`},
	{Name: "nul", Text: `{"exportedFrom": nul}`},
	{Name: "truex", Text: `{"exportedFrom": truex}`},
	{Name: "leading zero", Text: `{"schemaVersion": 01}`},
	{Name: "plus sign", Text: `{"schemaVersion": +1}`},
	{Name: "bad exponent", Text: `{"schemaVersion": 1e}`},
	{Name: "control character", Text: "{\"exportedFrom\": \"a\tb\"}"},
	{Name: "invalid escape", Text: `{"exportedFrom": "a\xb"}`},
	{Name: "short unicode escape", Text: `{"exportedFrom": "a\u12"}`},
	{Name: "bad unicode escape", Text: `{"exportedFrom": "a\u12zz"}`},
	{Name: "lone surrogate", Text: `{"exportedFrom": "a\ud83d"}`},
	{Name: "bad surrogate pair", Text: `{"exportedFrom": "a\ud83dA"}`},
	{Name: "invalid UTF-8", Text: "{\"exportedFrom\": \"a\xffb\"}"},
	{Name: "multibyte column", Text: "{\"exportedFrom\": \"é\", \"nope\": 1}"},
	{Name: "a name not a string", Text: `{schemaVersion: 1}`},
	{Name: "a close in place of a value", Text: `{"schemaVersion": }`},
	{Name: "mismatched close", Text: `{"scenarios": [}`},
	// The protobuf JSON mapping.
	{Name: "proto names", Old: `"schemaVersion": 1, "taxYear": 2026,`, New: `"schema_version": 1, "tax_year": 2026,`},
	{Name: "duplicate field", Old: `"taxYear": 2026,`, New: `"taxYear": 2026, "taxYear": 2026,`},
	{Name: "duplicate field by both names", Old: `"taxYear": 2026,`, New: `"taxYear": 2026, "tax_year": 2026,`},
	{Name: "int32 as a string", Old: `"taxYear": 2026,`, New: `"taxYear": "2026",`},
	{Name: "int32 with an exponent", Old: `"taxYear": 2026,`, New: `"taxYear": 2.026e3,`},
	{Name: "int32 with a zero fraction", Old: `"taxYear": 2026,`, New: `"taxYear": 2026.000,`},
	{Name: "int32 with a fraction", Old: `"taxYear": 2026,`, New: `"taxYear": 2026.5,`},
	{Name: "int32 too large", Old: `"taxYear": 2026,`, New: `"taxYear": 3000000000,`},
	{Name: "int32 string with spaces", Old: `"taxYear": 2026,`, New: `"taxYear": " 2026",`},
	{Name: "int32 string not a number", Old: `"taxYear": 2026,`, New: `"taxYear": "20x6",`},
	{Name: "int32 string with two numbers", Old: `"taxYear": 2026,`, New: `"taxYear": "2026 1",`},
	{Name: "int32 as a bool", Old: `"taxYear": 2026,`, New: `"taxYear": true,`},
	{Name: "int32 as an object", Old: `"taxYear": 2026,`, New: `"taxYear": {},`},
	{Name: "int64 as a number", Old: `"minor": "583333"`, New: `"minor": 583333`},
	{Name: "int64 negative string", Old: `"minor": "583333"`, New: `"minor": "-583333"`},
	{Name: "int64 with an exponent string", Old: `"minor": "583333"`, New: `"minor": "5.83333e5"`},
	{Name: "int64 too large", Old: `"minor": "583333"`, New: `"minor": "9223372036854775808"`},
	{Name: "int64 largest", Old: `"minor": "583333"`, New: `"minor": "9223372036854775807"`},
	{Name: "int64 negative exponent", Old: `"minor": "583333"`, New: `"minor": 58333300e-2`},
	{Name: "int64 negative exponent with a fraction", Old: `"minor": "583333"`, New: `"minor": 5833.33e-2`},
	{Name: "int64 zero", Old: `"minor": "583333"`, New: `"minor": "-0"`},
	{Name: "enum by number", Old: `"PAY_FREQUENCY_SEMI_MONTHLY"`, New: `3`},
	{Name: "enum by an unknown number", Old: `"PAY_FREQUENCY_SEMI_MONTHLY"`, New: `99`},
	{Name: "enum by zero", Old: `"PAY_FREQUENCY_SEMI_MONTHLY"`, New: `0`},
	{Name: "enum by an unknown name", Old: `"PAY_FREQUENCY_SEMI_MONTHLY"`, New: `"PAY_FREQUENCY_FORTNIGHTLY"`},
	{Name: "enum in lower case", Old: `"PAY_FREQUENCY_SEMI_MONTHLY"`, New: `"pay_frequency_semi_monthly"`},
	{Name: "enum as a string number", Old: `"PAY_FREQUENCY_SEMI_MONTHLY"`, New: `"3"`},
	{Name: "enum as a fraction", Old: `"PAY_FREQUENCY_SEMI_MONTHLY"`, New: `3.5`},
	{Name: "enum as null", Old: `"PAY_FREQUENCY_SEMI_MONTHLY"`, New: `null`},
	{Name: "null field", Old: `"taxYear": 2026,`, New: `"taxYear": 2026, "exportedFrom": null,`},
	{Name: "null message", Old: `"facts": { "taxYear": 2026 },`, New: `"facts": null,`},
	{Name: "null list", Old: `"scenarios": [`, New: `"nothing": null, "scenarios": [`},
	{Name: "null list field", Old: `"parts": [`, New: `"parts": null, "x": [`},
	{Name: "null in a list", Old: `{ "id": "plan", "name": "Your plan" },`, New: `null, { "id": "plan", "name": "Your plan" },`},
	{Name: "null field then duplicate", Old: `"taxYear": 2026,`, New: `"taxYear": null, "taxYear": 2026,`},
	{Name: "string as a number", Old: `"exportedFrom": "y2026.41/d3"`, New: `"exportedFrom": 12`},
	{Name: "string as an object", Old: `"exportedFrom": "y2026.41/d3"`, New: `"exportedFrom": {"a": 1}`},
	{Name: "string as a list", Old: `"exportedFrom": "y2026.41/d3"`, New: `"exportedFrom": ["a"]`},
	{Name: "string escapes", Old: `"name": "Acme Corp"`, New: `"name": "Acme \"Corp\" \\ \/ \b\f\n\r\t é 😀 é"`},
	{Name: "message as a string", Old: `"facts": { "taxYear": 2026 },`, New: `"facts": "x",`},
	{Name: "message as a list", Old: `"facts": { "taxYear": 2026 },`, New: `"facts": [],`},
	{Name: "list as an object", Old: `"scenarios": [`, New: `"scenarios": {}, "x": [`},
	{Name: "list as a string", Old: `"parts": [`, New: `"parts": "x", "y": [`},
	{Name: "empty list", Old: `"parts": [`, New: `"parts": [], "y": [`},
	{Name: "a list item not a message", Old: `{ "id": "plan", "name": "Your plan" },`, New: `1, { "id": "plan", "name": "Your plan" },`},
	{Name: "bool field as a string", Old: `"whereLived": {}`, New: `"whereLived": {}, "coveredByPlan": "true"`},
	{Name: "bool field", Old: `"whereLived": {}`, New: `"whereLived": {}, "coveredByPlan": false`},
	{Name: "oneof set twice", Old: `"sameAsLatest": {}`, New: `"sameAsLatest": {}, "eachPaycheck": {}`},
	{Name: "oneof set by a null then a case", Old: `"sameAsLatest": {}`, New: `"eachPaycheck": null, "sameAsLatest": {}`},
	{Name: "oneof case as null", Old: `"sameAsLatest": {}`, New: `"sameAsLatest": null`},
	{Name: "empty message with a field", Old: `"sameAsLatest": {}`, New: `"sameAsLatest": {"x": 1}`},
	{Name: "an extension name", Old: `"taxYear": 2026,`, New: `"taxYear": 2026, "[a.b.c]": 1,`},
	{Name: "an unknown field in a list", Old: `{ "id": "plan", "name": "Your plan" },`, New: `{ "id": "plan", "name": "Your plan", "planned": 1 },`},
	// Taxyear's own refusals, several at once and sorted.
	{Name: "many refusals", Old: `"PAY_FREQUENCY_SEMI_MONTHLY",`, New: `"PAY_FREQUENCY_UNSPECIFIED", "w4": {}, "lastYear": {}, "retirementPlan": "RETIREMENT_PLAN_UNSPECIFIED",`},
	{Name: "unspecified enums and empty oneofs", Old: `"defaultPart": { "id": "acme-m9t3r7xe", "sameAsLatest": {} } }`,
		New: `"defaultPart": { "id": "acme-m9t3r7xe" } }, { "id": "b", "supplemental": "SUPPLEMENTAL_METHOD_UNSPECIFIED", "parts": [ { "id": "p" } ] }`},
	{Name: "unspecified text in a string field", Old: `"name": "Acme Corp"`, New: `"name": "ACME_UNSPECIFIED"`},
	{Name: "unspecified in a list", Old: `"exportedFrom": "y2026.41/d3",`, New: `"exportedFrom": "y2026.41/d3", "shared2": ["X_UNSPECIFIED"],`},
	{Name: "an empty oneof in a diff", Old: `"payRate": {`, New: `"x": {`},
}

func TestTaxYearParse(t *testing.T) {
	example, err := os.ReadFile("../taxyear/testdata/example.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	out := []taxyearParse{}
	for _, c := range taxyearParseCases {
		text := c.Text
		if c.Old != "" {
			if !strings.Contains(string(example), c.Old) {
				t.Fatalf("%s: the example has no %q", c.Name, c.Old)
			}
			text = strings.Replace(string(example), c.Old, c.New, 1)
		}
		if !utf8.ValidString(c.Text) {
			c.TextBytes = base64.StdEncoding.EncodeToString([]byte(c.Text))
		}
		f, err := taxyear.Parse([]byte(text))
		c.Error = protoText(err)
		if err == nil {
			c.Parsed = encode(t, f)
		}
		out = append(out, c)
	}
	check(t, "taxyear/parse.json", out)
}

// taxyearCacheVariants are cache_test.go's variants.
var taxyearCacheVariants = []taxyearCache{
	{Name: "the example"},
	{Name: "a raise edited", Old: `"minor": "625000"`, New: `"minor": "700000"`},
	{Name: "the raise back"},
	{Name: "shared edited", Old: `"minor": "583333"`, New: `"minor": "600000"`},
	{Name: "a missing parent", Old: `"parent": "plan"`, New: `"parent": "nope"`},
	{Name: "a diff that doesn't apply", Old: `"add": { "id": "acme-v2k8n5zd"`, New: `"add": { "id": "acme-h4c8w2pq"`},
	{Name: "an identifier in shared", Old: `"Aug 14 paystub"`, New: `"123456789"`},
	{Name: "a second child", Old: "] } } ] } } }\n  ]", New: "] } } ] } } },\n    { \"id\": \"raise2\", \"name\": \"Raise again\", \"parent\": \"raise\" }\n  ]"},
	{Name: "the example again"},
}

func TestTaxYearCache(t *testing.T) {
	example, err := os.ReadFile("../taxyear/testdata/example.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	small, err := taxyear.NewCache(1)
	if err != nil {
		t.Fatal(err)
	}
	large, err := taxyear.NewCache(64)
	if err != nil {
		t.Fatal(err)
	}
	out := []taxyearCache{}
	for _, v := range taxyearCacheVariants {
		text := string(example)
		if v.Old != "" {
			if !strings.Contains(text, v.Old) {
				t.Fatalf("%s: the example has no %q", v.Name, v.Old)
			}
			text = strings.Replace(text, v.Old, v.New, 1)
		}
		f, err := taxyear.Parse([]byte(text))
		if err != nil {
			t.Fatalf("%s: %v", v.Name, err)
		}
		s, err := small.Compute(f)
		if err != nil {
			t.Fatal(err)
		}
		l, err := large.Compute(f)
		if err != nil {
			t.Fatal(err)
		}
		v.Small, v.Large = encode(t, s.Message()), encode(t, l.Message())
		out = append(out, v)
	}
	checkGzip(t, "taxyear/cache.json.gz", out)
}

// taxyearOverride is Your plan of a file with one figure so far
// overridden: the figure as computed, and 123 dollars more.
type taxyearOverride struct {
	File   string `json:"file"`
	Path   string `json:"path"`
	Amount int64  `json:"amount"`
	Plan   string `json:"plan"`
}

// TestTaxYearOverrides overrides, in each file with figures to override,
// one figure so far of each kind a model's so far items hold (the first
// path of each shape, ids and states left out), each alone, as
// taxyear's TestOverridesTakeEffect does, and records Your plan.
func TestTaxYearOverrides(t *testing.T) {
	out := []taxyearOverride{}
	for _, path := range []string{"taxyear/testdata/cases/full-2026.jsonc", "taxyear/testdata/cases/simple-upload-view.jsonc"} {
		text, err := os.ReadFile(filepath.Join("..", path))
		if err != nil {
			t.Fatal(err)
		}
		file, err := taxyear.Parse(text)
		if err != nil {
			t.Fatal(err)
		}
		base, err := taxyear.Compute(file)
		if err != nil {
			t.Fatal(err)
		}
		figures := overridableFigures(base.Message().GetScenarios()[0])
		keys := make([]string, 0, len(figures))
		for k := range figures {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		shapes := map[string]bool{}
		for _, k := range keys {
			shape := regexp.MustCompile(`\[[^\]]*\]`).ReplaceAllString(k, "[]")
			if shapes[shape] {
				continue
			}
			shapes[shape] = true
			f := proto.Clone(file).(*scenariosv1.TaxYearScenarios)
			want := figures[k] + 12_300
			f.Shared.Overrides = append(f.Shared.Overrides, &scenariosv1.Override{Path: k, Amount: &commonv1.Money{Minor: want, Currency: "USD"}})
			r, err := taxyear.Compute(f)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, taxyearOverride{File: path, Path: k, Amount: want, Plan: encode(t, r.Message().GetScenarios()[0])})
		}
	}
	checkGzip(t, "taxyear/overrides.json.gz", out)
}

// overridableFigures is taxyear's override_test.go overridable: the
// figures so far an override can name in a result, with their values.
func overridableFigures(s *scenariosv1.ScenarioResult) map[string]int64 {
	out := map[string]int64{}
	proj := s.GetProjection().ProtoReflect()
	proj.Range(func(list protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if list.Name() == "tax_paid" {
			return true
		}
		for i := 0; i < v.List().Len(); i++ {
			p := v.List().Get(i).Message()
			id := p.Get(p.Descriptor().Fields().ByName("id")).String()
			sf := p.Descriptor().Fields().ByName("so_far")
			if sf == nil || !p.Has(sf) {
				continue
			}
			at := fmt.Sprintf("projection.%s[%s].so_far.items", list.Name(), id)
			items := p.Get(sf).Message().Get(p.Get(sf).Message().Descriptor().Fields().ByName("items")).Message()
			if m, ok := items.Interface().(*commonv1.Money); ok {
				out[at+".amount"] = m.GetMinor()
				continue
			}
			items.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
				if fd.Kind() != protoreflect.MessageKind || fd.IsMap() {
					return true
				}
				if !fd.IsList() {
					if m, ok := v.Message().Interface().(*commonv1.Money); ok {
						out[at+"."+string(fd.Name())] = m.GetMinor()
					}
					return true
				}
				for k := 0; k < v.List().Len(); k++ {
					switch x := v.List().Get(k).Message().Interface().(type) {
					case interface{ GetAmount() *commonv1.Money }:
						if fd.Name() == "pretax" {
							out[fmt.Sprintf("%s.pretax[%d].amount", at, k)] = x.GetAmount().GetMinor()
						}
					case interface {
						GetState() string
						GetStateIncome() *commonv1.Money
						GetTaxWithheld() *commonv1.Money
					}:
						out[fmt.Sprintf("%s.states[%s].state_income", at, x.GetState())] = x.GetStateIncome().GetMinor()
						out[fmt.Sprintf("%s.states[%s].tax_withheld", at, x.GetState())] = x.GetTaxWithheld().GetMinor()
					}
				}
				return true
			})
		}
		return true
	})
	return out
}
