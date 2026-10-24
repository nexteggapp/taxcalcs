package taxyear

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"

	scenariosv1 "github.com/nexteggapp/taxcalcs/scenarios/v1"
)

// Parse reads a TaxYearScenarios file as JSON with comments: comments
// removed, then the protobuf JSON mapping, strictly. An unknown field is
// refused, which catches a computed figure added to the file,
// and so is a message whose oneof has no case and an enum written as its
// UNSPECIFIED value. It returns every such failure with its path.
func Parse(jsonc []byte) (*scenariosv1.TaxYearScenarios, error) {
	text, err := StripComments(jsonc)
	if err != nil {
		return nil, err
	}
	file := &scenariosv1.TaxYearScenarios{}
	if err := (protojson.UnmarshalOptions{}).Unmarshal(text, file); err != nil {
		return nil, fmt.Errorf("taxyear: the file is not a TaxYearScenarios: %w", err)
	}
	var problems []string
	var raw any
	if err := json.Unmarshal(text, &raw); err == nil {
		unspecified("", raw, &problems)
	}
	emptyOneofs("", file.ProtoReflect(), &problems)
	if len(problems) > 0 {
		sort.Strings(problems)
		return nil, fmt.Errorf("taxyear: the file is refused:\n  %s", strings.Join(problems, "\n  "))
	}
	return file, nil
}

// StripComments removes JSONC comments: "//" to the end of the line, and
// "/*" to "*/". Text inside a JSON string is kept, and each comment's
// newlines are kept, so a JSON error's line number still points into the
// original.
func StripComments(in []byte) ([]byte, error) {
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
				return nil, errors.New("taxyear: a /* comment is not closed")
			}
			out.Write(bytes.Repeat([]byte("\n"), bytes.Count(in[i:i+2+end+2], []byte("\n"))))
			i += 2 + end + 1
		default:
			out.WriteByte(in[i])
		}
	}
	return out.Bytes(), nil
}

// stringEnd returns the index just past the JSON string that starts at
// in[start], or len(in) if it is not closed.
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

// unspecified records each string written as an enum's UNSPECIFIED value.
func unspecified(path string, v any, problems *[]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, c := range x {
			unspecified(path+"."+k, c, problems)
		}
	case []any:
		for i, c := range x {
			unspecified(fmt.Sprintf("%s[%d]", path, i), c, problems)
		}
	case string:
		if strings.HasSuffix(x, "_UNSPECIFIED") {
			*problems = append(*problems, fmt.Sprintf("%s: %s is not a value", strings.TrimPrefix(path, "."), x))
		}
	}
}

// emptyOneofs records each message present whose oneof has no case.
func emptyOneofs(path string, m protoreflect.Message, problems *[]string) {
	d := m.Descriptor()
	for i := 0; i < d.Oneofs().Len(); i++ {
		o := d.Oneofs().Get(i)
		if o.IsSynthetic() {
			continue
		}
		if m.WhichOneof(o) == nil {
			*problems = append(*problems, fmt.Sprintf("%s: %s has no case set", strings.TrimPrefix(path, "."), o.Name()))
		}
	}
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.Kind() != protoreflect.MessageKind && f.Kind() != protoreflect.GroupKind {
			return true
		}
		p := path + "." + string(f.Name())
		switch {
		case f.IsList():
			l := v.List()
			for i := 0; i < l.Len(); i++ {
				emptyOneofs(fmt.Sprintf("%s[%d]", p, i), l.Get(i).Message(), problems)
			}
		case f.IsMap():
		default:
			emptyOneofs(p, v.Message(), problems)
		}
		return true
	})
}
