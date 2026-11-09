package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/nexteggapp/taxcalcs/taxlisp/expand"
	"github.com/nexteggapp/taxcalcs/taxlisp/parser"
	taxlispv1 "github.com/nexteggapp/taxcalcs/taxlisp/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// Type is a protobuf message or enum, with the comments from its .proto file.
type Type struct {
	// Name is the full name; Slug is its page's address.
	Name    string       `json:"name"`
	Slug    string       `json:"slug"`
	Short   string       `json:"short"`
	Kind    string       `json:"kind"`
	Package string       `json:"package"`
	File    string       `json:"file"`
	Doc     string       `json:"doc,omitempty"`
	Fields  []*TypeField `json:"fields,omitempty"`
	Values  []*EnumValue `json:"values,omitempty"`
	// UsedBy are the slugs of the messages with a field of this type.
	UsedBy []string `json:"usedBy,omitempty"`
}

// TypeEntry names a type, for linking to its page.
type TypeEntry struct {
	Slug    string `json:"slug"`
	Short   string `json:"short"`
	Kind    string `json:"kind"`
	Summary string `json:"summary,omitempty"`
}

// TypeField is a message's field.
type TypeField struct {
	Name   string `json:"name"`
	Number int    `json:"number"`
	Type   string `json:"type"`
	// Ref is the slug of the field's message or enum type.
	Ref      string `json:"ref,omitempty"`
	Repeated bool   `json:"repeated,omitempty"`
	Oneof    string `json:"oneof,omitempty"`
	Doc      string `json:"doc,omitempty"`
	// ReadBy are the rules that read the field.
	ReadBy []string `json:"readBy,omitempty"`
}

// EnumValue is an enum's value.
type EnumValue struct {
	Name   string `json:"name"`
	Number int    `json:"number"`
	Doc    string `json:"doc,omitempty"`
}

// descriptorSet is the -descriptors flag: where the type pages come from.
var descriptorSet string

// protoAliases maps the rule files' use-proto aliases, such as ret, to
// their packages.
// The federal rules, loaded before the states', declare the first four.
var protoAliases = map[string]string{
	"ret":    "nexteggapp.taxcalcs.returns.v1",
	"fed":    "nexteggapp.taxprotos.us.federal.v1",
	"facts":  "nexteggapp.taxprotos.facts.v1",
	"common": "nexteggapp.taxprotos.common.v1",
}

// The messages the type pages start from: the return contract.
var typeRoots = []string{
	"nexteggapp.taxcalcs.returns.v1.ReturnInputs",
	"nexteggapp.taxcalcs.returns.v1.ReturnOutputs",
	"nexteggapp.taxcalcs.returns.v1.DescribeInputsRequest",
	"nexteggapp.taxcalcs.returns.v1.DescribeInputsResponse",
	"nexteggapp.taxcalcs.returns.v1.PrepareRequest",
	"nexteggapp.taxcalcs.returns.v1.PrepareResponse",
}

func typeSlug(name protoreflect.FullName) string {
	s := string(name)
	s = strings.TrimPrefix(s, "nexteggapp.taxcalcs.")
	return strings.TrimPrefix(s, "nexteggapp.")
}

func readTypes(m *Model, descriptorSet string) error {
	b, err := os.ReadFile(descriptorSet)
	if err != nil {
		return err
	}
	set := &descriptorpb.FileDescriptorSet{}
	if err := proto.Unmarshal(b, set); err != nil {
		return err
	}
	files, err := protodesc.NewFiles(set)
	if err != nil {
		return err
	}
	byName := map[protoreflect.FullName]*Type{}
	var visit func(d protoreflect.Descriptor)
	visit = func(d protoreflect.Descriptor) {
		if _, seen := byName[d.FullName()]; seen {
			return
		}
		t := &Type{
			Name:    string(d.FullName()),
			Slug:    typeSlug(d.FullName()),
			Short:   strings.TrimPrefix(string(d.FullName()), string(d.ParentFile().Package())+"."),
			Package: string(d.ParentFile().Package()),
			File:    d.ParentFile().Path(),
			Doc:     comment(d),
		}
		byName[d.FullName()] = t
		switch d := d.(type) {
		case protoreflect.EnumDescriptor:
			t.Kind = "enum"
			for i := 0; i < d.Values().Len(); i++ {
				v := d.Values().Get(i)
				t.Values = append(t.Values, &EnumValue{Name: string(v.Name()), Number: int(v.Number()), Doc: comment(v)})
			}
		case protoreflect.MessageDescriptor:
			t.Kind = "message"
			for i := 0; i < d.Fields().Len(); i++ {
				fd := d.Fields().Get(i)
				f := &TypeField{Name: string(fd.Name()), Number: int(fd.Number()), Repeated: fd.IsList(), Doc: comment(fd)}
				if o := fd.ContainingOneof(); o != nil && !o.IsSynthetic() {
					f.Oneof = string(o.Name())
				}
				var ref protoreflect.Descriptor
				switch {
				case fd.IsMap():
					f.Type = fmt.Sprintf("map<%s, %s>", kindName(fd.MapKey()), kindName(fd.MapValue()))
					if md := fd.MapValue().Message(); md != nil {
						ref = md
					} else if ed := fd.MapValue().Enum(); ed != nil {
						ref = ed
					}
				case fd.Message() != nil:
					ref = fd.Message()
				case fd.Enum() != nil:
					ref = fd.Enum()
				default:
					f.Type = fd.Kind().String()
				}
				if ref != nil {
					if f.Type == "" {
						f.Type = string(ref.Name())
					}
					f.Ref = typeSlug(ref.FullName())
					visit(ref)
					rt := byName[ref.FullName()]
					if len(rt.UsedBy) == 0 || rt.UsedBy[len(rt.UsedBy)-1] != t.Slug {
						rt.UsedBy = append(rt.UsedBy, t.Slug)
					}
				}
				t.Fields = append(t.Fields, f)
			}
		}
	}
	for _, r := range typeRoots {
		d, err := files.FindDescriptorByName(protoreflect.FullName(r))
		if err != nil {
			return fmt.Errorf("type %s: %w", r, err)
		}
		visit(d)
	}
	readBy(m, files, byName)
	for _, t := range byName {
		sort.Strings(t.UsedBy)
		m.Types = append(m.Types, t)
	}
	sort.Slice(m.Types, func(i, j int) bool { return m.Types[i].Slug < m.Types[j].Slug })
	for _, t := range m.Types {
		summary, _, _ := strings.Cut(t.Doc, "\n")
		if i := strings.Index(summary, ". "); i >= 0 {
			summary = summary[:i+1]
		}
		m.TypeIndex = append(m.TypeIndex, &TypeEntry{Slug: t.Slug, Short: t.Short, Kind: t.Kind, Summary: summary})
	}
	return nil
}

func kindName(fd protoreflect.FieldDescriptor) string {
	switch {
	case fd.Message() != nil:
		return string(fd.Message().Name())
	case fd.Enum() != nil:
		return string(fd.Enum().Name())
	}
	return fd.Kind().String()
}

func comment(d protoreflect.Descriptor) string {
	loc := d.ParentFile().SourceLocations().ByDescriptor(d)
	lines := strings.Split(strings.TrimSpace(loc.LeadingComments), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(joinParagraphs(lines))
}

// readBy finds, in each rule, the protobuf fields it reads with (. x 'path),
// where x is a parameter, a lambda's parameter, or a name bound to a field
// or to a rule's result whose type is known.
func readBy(m *Model, files *protoregistry.Files, types map[protoreflect.FullName]*Type) {
	returns := map[string]string{}
	for _, r := range m.Rules {
		returns[r.Name] = r.Returns
	}
	resolve := func(typ string) protoreflect.MessageDescriptor {
		typ = strings.TrimSuffix(strings.TrimPrefix(typ, "(Slice "), ")")
		alias, name, ok := strings.Cut(typ, "/")
		if !ok {
			return nil
		}
		d, err := files.FindDescriptorByName(protoreflect.FullName(protoAliases[alias] + "." + name))
		if err != nil {
			return nil
		}
		md, _ := d.(protoreflect.MessageDescriptor)
		return md
	}
	for _, r := range m.Rules {
		if r.decl == nil {
			continue
		}
		env := map[string]protoreflect.MessageDescriptor{}
		for _, p := range r.Params {
			if md := resolve(p.Type); md != nil {
				env[p.Name] = md
			}
		}
		// follow reads path from md, marking each field read, and returns
		// the message the path ends in, if any.
		follow := func(md protoreflect.MessageDescriptor, path string) protoreflect.MessageDescriptor {
			for _, seg := range strings.Split(path, ".") {
				if md == nil {
					return nil
				}
				fd := md.Fields().ByName(protoreflect.Name(seg))
				if fd == nil {
					return nil
				}
				if t := types[md.FullName()]; t != nil {
					for _, f := range t.Fields {
						if f.Name == seg && (len(f.ReadBy) == 0 || f.ReadBy[len(f.ReadBy)-1] != r.Name) {
							f.ReadBy = append(f.ReadBy, r.Name)
						}
					}
				}
				md = fd.Message()
			}
			return md
		}
		var typeOf func(e *taxlispv1.Expression) protoreflect.MessageDescriptor
		typeOf = func(e *taxlispv1.Expression) protoreflect.MessageDescriptor {
			el := e.GetList().GetExpressions()
			if len(el) == 0 {
				return nil
			}
			head := expand.Head(e)
			if head == "." && len(el) == 3 {
				if md := env[el[1].GetAtom().GetSymbol().GetName()]; md != nil {
					return follow(md, quoted(el[2]))
				}
				return nil
			}
			if ret, ok := returns[head]; ok {
				return resolve(ret)
			}
			return nil
		}
		bind := func(name *taxlispv1.Expression, value *taxlispv1.Expression) {
			if md := typeOf(value); md != nil {
				env[name.GetAtom().GetSymbol().GetName()] = md
			}
		}
		var walk func(e *taxlispv1.Expression)
		walk = func(e *taxlispv1.Expression) {
			el := e.GetList().GetExpressions()
			if len(el) == 0 {
				return
			}
			switch expand.Head(e) {
			case "bind", "let":
				if len(el) >= 3 {
					bind(el[1], el[2])
				}
			case "attach":
				if len(el) == 4 {
					bind(el[1], el[3])
				}
			case "with", "withs":
				if len(el) >= 2 {
					bs := el[1].GetList().GetExpressions()
					for i := 0; i+1 < len(bs); i += 2 {
						walk(bs[i+1])
						bind(bs[i], bs[i+1])
					}
				}
			case "fn":
				if len(el) >= 2 {
					for _, p := range el[1].GetList().GetExpressions() {
						if pe := p.GetList().GetExpressions(); len(pe) == 2 {
							if md := resolve(parser.FormatExpr(pe[1])); md != nil {
								env[pe[0].GetAtom().GetSymbol().GetName()] = md
							}
						}
					}
				}
			case ".":
				typeOf(e)
			}
			for _, c := range el {
				walk(c)
			}
		}
		walk(r.decl)
	}
}

// quoted is the text of a quoted symbol: 'ret or 'summary.total_tax.
func quoted(e *taxlispv1.Expression) string {
	el := e.GetList().GetExpressions()
	if expand.Head(e) != "quote" || len(el) != 2 {
		return ""
	}
	s := el[1].GetAtom().GetSymbol()
	if s.GetPackage() != "" {
		return s.GetPackage() + "/" + s.GetName()
	}
	return s.GetName()
}
