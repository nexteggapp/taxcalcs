package main

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/nexteggapp/taxcalcs/returns/preparer"
	"github.com/nexteggapp/taxcalcs/returns/rules/federal"
	returnsv1 "github.com/nexteggapp/taxcalcs/returns/v1"
	"github.com/nexteggapp/taxcalcs/sources"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Example is a real message, or the parts of one, in protobuf text format:
// what a sample household gives taxcalcs, or what it gives back.
type Example struct {
	// Type is the message's full name.
	Type string `json:"type"`
	// From says where the message came from.
	From string `json:"from"`
	// Paths are the fields shown, when the others are left out.
	Paths []string `json:"paths,omitempty"`
	Text  string   `json:"text"`
}

// run is one sample household's inputs and outputs.
type run struct {
	sample
	in  *returnsv1.ReturnInputs
	out *returnsv1.ReturnOutputs
}

func examples(m *Model, runs []run, eng *federal.Engine) error {
	m.Examples = map[string]*Example{}
	add := func(key string, msg proto.Message, from string, paths ...string) error {
		shown := msg.ProtoReflect()
		if len(paths) > 0 {
			var err error
			if shown, err = prune(shown, paths); err != nil {
				return fmt.Errorf("example %s: %w", key, err)
			}
		}
		m.Examples[key] = &Example{
			Type:  string(shown.Descriptor().FullName()),
			From:  from,
			Paths: paths,
			Text:  textFormat(shown.Interface()),
		}
		return nil
	}
	renter, student := runs[0], runs[len(runs)-1]
	if renter.id != "single-renter-40k" || student.id != "student-dependent-18k" {
		return fmt.Errorf("examples: the samples have changed order")
	}
	from := func(r run) string { return "the sample household “" + r.label + "”" }
	steps := []struct {
		key   string
		msg   proto.Message
		r     run
		paths []string
	}{
		{"inputs", renter.in, renter, nil},
		{"household", renter.in, renter, []string{"federal.household", "states[0].state", "states[0].ca"}},
		{"w2", renter.in, renter, []string{"federal.documents.w2s[0]"}},
		{"elections", renter.in, renter, []string{"federal.elections", "states[0].ca.deduction_method"}},
		{"return", renter.out, renter, []string{"states[0].ca.status", "states[0].ca.summary"}},
		{"resolved", renter.out, renter, []string{"states[0].ca.elections"}},
		{"diagnostics", student.out, student, []string{"states[0].ca.status", "states[0].ca.diagnostics"}},
		{"money", renter.in, renter, []string{"federal.documents.w2s[0].form.wages_tips_other_compensation"}},
	}
	for _, s := range steps {
		if err := add(s.key, s.msg, from(s.r), s.paths...); err != nil {
			return err
		}
	}

	// What DescribeInputs asks for first when the inputs hold only a year.
	srv := preparer.NewServerWithEngine(eng)
	desc, err := srv.DescribeInputs(context.Background(), &returnsv1.DescribeInputsRequest{
		Inputs: &returnsv1.ReturnInputs{SchemaVersion: 1, TaxYear: 2025},
	})
	if err != nil {
		return err
	}
	var first []*returnsv1.InputFieldDescriptor
	for _, f := range desc.GetFields() {
		if f.GetRequiredForCompletion() && len(first) < 2 {
			first = append(first, f)
		}
	}
	if len(first) == 0 {
		first = desc.GetFields()[:2]
	}
	if err := add("describe", &returnsv1.DescribeInputsResponse{Fields: first},
		"DescribeInputs, given only tax_year: 2025 (the first two fields required)"); err != nil {
		return err
	}

	// A source and one of its anchors, from the catalog.
	catalog, err := sources.Catalog()
	if err != nil {
		return err
	}
	for _, src := range catalog {
		if src.GetId() != "ca-ftb-540-booklet-2025" {
			continue
		}
		for i, a := range src.GetAnchors() {
			if a.GetId() == "agi-limitation-worksheet" {
				return add("source", src, "the source catalog, sources/catalog/ca/2025/ftb-540-booklet.textproto",
					"id", "kind", "title", "tax_years", "copies", fmt.Sprintf("anchors[%d]", i))
			}
		}
	}
	return fmt.Errorf("examples: no AGI limitation worksheet anchor in the catalog")
}

var segment = regexp.MustCompile(`^(\w+)(?:\[(\d+)\])?$`)

// prune copies only the fields at paths, such as "federal.documents.w2s[0]",
// into a new message of msg's type.
func prune(msg protoreflect.Message, paths []string) (protoreflect.Message, error) {
	out := msg.New()
	for _, p := range paths {
		if err := copyPath(msg, out, strings.Split(p, ".")); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
	}
	return out, nil
}

func copyPath(src, dst protoreflect.Message, segs []string) error {
	m := segment.FindStringSubmatch(segs[0])
	if m == nil {
		return fmt.Errorf("bad segment %q", segs[0])
	}
	fd := src.Descriptor().Fields().ByName(protoreflect.Name(m[1]))
	if fd == nil {
		return fmt.Errorf("%s has no field %s", src.Descriptor().FullName(), m[1])
	}
	last := len(segs) == 1
	if !src.Has(fd) {
		return fmt.Errorf("%s is not set", m[1])
	}
	if !fd.IsList() || m[2] == "" {
		if last {
			dst.Set(fd, src.Get(fd))
			return nil
		}
		return copyPath(src.Get(fd).Message(), dst.Mutable(fd).Message(), segs[1:])
	}
	i, _ := strconv.Atoi(m[2])
	from := src.Get(fd).List()
	if i >= from.Len() {
		return fmt.Errorf("%s has no element %d", m[1], i)
	}
	to := dst.Mutable(fd).List()
	if last {
		to.Append(from.Get(i))
		return nil
	}
	if to.Len() == 0 {
		to.Append(to.NewElement())
	}
	return copyPath(from.Get(i).Message(), to.Get(to.Len()-1).Message(), segs[1:])
}

// The protobuf module spaces its text output unpredictably, so that no one
// depends on its exact form; these pages want it the same on every build.
//
// Messages are written "name {", as the catalog's textproto files are, and
// each amount of money is followed by a comment saying it in dollars.
var (
	field   = regexp.MustCompile(`(?m)^(\s*[\w.\[\]/]+):\s+`)
	message = regexp.MustCompile(`(?m)^(\s*[\w.\[\]/]+): \{`)
	minor   = regexp.MustCompile(`(?m)^(\s*)minor: (-?\d+)$`)
)

func textFormat(m proto.Message) string {
	b := prototext.MarshalOptions{Multiline: true, Indent: "  "}.Format(m)
	b = field.ReplaceAllString(b, "$1: ")
	b = message.ReplaceAllString(b, "$1 {")
	b = minor.ReplaceAllStringFunc(b, func(s string) string {
		g := minor.FindStringSubmatch(s)
		c, _ := strconv.ParseInt(g[2], 10, 64)
		sign := ""
		if c < 0 {
			sign, c = "-", -c
		}
		return fmt.Sprintf("%sminor: %s  # %s%s", g[1], g[2], sign, dollars(c))
	})
	return strings.TrimSpace(b)
}
