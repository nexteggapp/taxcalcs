package taxyear

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// Path is a field path: proto field names joined by dots, a list item named
// by its key in brackets, as docs/scenarios.md gives the grammar. A key is an
// item's id; a jurisdiction or state in a list of them; else a position from
// 0.
type Path struct {
	steps []step
}

type step struct {
	field  string
	key    string
	hasKey bool
}

// ParsePath parses a field path, such as
// "projection.employers[acme].year.boxes.wages". A key may hold dots and
// slashes, as an item id does.
func ParsePath(s string) (Path, error) {
	if s == "" {
		return Path{}, errors.New("taxyear: an empty path")
	}
	var p Path
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && s[j] != '.' && s[j] != '[' {
			j++
		}
		st := step{field: s[i:j]}
		if st.field == "" {
			return Path{}, fmt.Errorf("taxyear: %q has an empty step at %d", s, i)
		}
		if j < len(s) && s[j] == '[' {
			end := strings.IndexByte(s[j:], ']')
			if end < 0 {
				return Path{}, fmt.Errorf("taxyear: %q has an unclosed [", s)
			}
			st.key, st.hasKey = s[j+1:j+end], true
			j += end + 1
		}
		p.steps = append(p.steps, st)
		switch {
		case j == len(s):
			return p, nil
		case s[j] != '.':
			return Path{}, fmt.Errorf("taxyear: %q has %q after a key", s, s[j])
		}
		i = j + 1
		if i == len(s) {
			return Path{}, fmt.Errorf("taxyear: %q ends with a dot", s)
		}
	}
	return p, nil
}

// String writes the path back.
func (p Path) String() string {
	var b strings.Builder
	for i, s := range p.steps {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(s.field)
		if s.hasKey {
			b.WriteString("[" + s.key + "]")
		}
	}
	return b.String()
}

// keyFields are the fields a list item's key is read from, in order.
var keyFields = []protoreflect.Name{"id", "jurisdiction", "state", "scenario", "path", "person"}

// lookup follows a path into a message and returns the value it names and
// that value's field.
func lookup(m protoreflect.Message, p Path) (protoreflect.Value, protoreflect.FieldDescriptor, bool) {
	var v protoreflect.Value
	var fd protoreflect.FieldDescriptor
	for i, s := range p.steps {
		if m == nil {
			return protoreflect.Value{}, nil, false
		}
		fd = m.Descriptor().Fields().ByName(protoreflect.Name(s.field))
		if fd == nil {
			return protoreflect.Value{}, nil, false
		}
		v = m.Get(fd)
		if fd.IsList() {
			if !s.hasKey {
				return v, fd, i == len(p.steps)-1
			}
			el, ok := element(v.List(), fd, s.key)
			if !ok {
				return protoreflect.Value{}, nil, false
			}
			v = el
		} else if s.hasKey {
			return protoreflect.Value{}, nil, false
		}
		m = nil
		if fd.Kind() == protoreflect.MessageKind {
			if !fd.IsList() && !m0(v) {
				return protoreflect.Value{}, nil, false
			}
			m = v.Message()
		}
	}
	return v, fd, true
}

func m0(v protoreflect.Value) bool { return v.Message().IsValid() }

// element finds a list item by its key.
func element(l protoreflect.List, fd protoreflect.FieldDescriptor, key string) (protoreflect.Value, bool) {
	if fd.Kind() == protoreflect.MessageKind {
		d := fd.Message()
		for _, name := range keyFields {
			kf := d.Fields().ByName(name)
			if kf == nil || kf.Kind() != protoreflect.StringKind {
				continue
			}
			for i := 0; i < l.Len(); i++ {
				if l.Get(i).Message().Get(kf).String() == key {
					return l.Get(i), true
				}
			}
			return protoreflect.Value{}, false
		}
	}
	n, err := strconv.Atoi(key)
	if err != nil || n < 0 || n >= l.Len() {
		return protoreflect.Value{}, false
	}
	return l.Get(n), true
}
