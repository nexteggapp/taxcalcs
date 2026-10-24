package taxyear

import (
	"sync"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	configv1 "github.com/nexteggapp/protodiffgen/v1"
)

// pruned is the part of a message a diff touches, for apply to copy and
// check instead of the whole, and how to put the rest back.
//
// At each message the diff patches, the part holds only the fields the diff
// sets or clears. In a keyed list it holds only the items whose keys the
// diff names, each pruned in turn when it is patched. A list whose order
// the diff changes is held whole.
type pruned struct {
	given protoreflect.Message
	// restore puts what wasn't given back into apply's result, shared
	// with the message it came from.
	restore func(result protoreflect.Message)
}

// body is what pruning reads from a diff message's descriptor: for each of
// its fields, the target field it changes and how.
type body struct {
	fields map[protoreflect.Name]diffField
	// ordered names the target lists whose order a field of the body sets.
	ordered map[protoreflect.Name]protoreflect.FieldDescriptor
}

type diffField struct {
	patch bool // a singular patch body
	list  bool // a keyed list's changes
	// For a keyed list: the change wrapper's key field, its patch field,
	// and the key's name in the item.
	key, patchFd protoreflect.FieldDescriptor
	itemKey      protoreflect.Name
}

var bodies sync.Map // protoreflect.MessageDescriptor -> *body

func bodyOf(md protoreflect.MessageDescriptor) *body {
	if b, ok := bodies.Load(md); ok {
		return b.(*body)
	}
	b := &body{fields: map[protoreflect.Name]diffField{}, ordered: map[protoreflect.Name]protoreflect.FieldDescriptor{}}
	fields := md.Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		src, _ := proto.GetExtension(f.Options(), configv1.E_Field).(*configv1.FieldSource)
		if src.GetOrder() {
			b.ordered[lastName(src.GetSource())] = f
			continue
		}
		if f.Message() == nil {
			continue
		}
		change, _ := proto.GetExtension(f.Message().Options(), configv1.E_Change).(*configv1.Change)
		switch {
		case !f.IsList() && change.GetKind() == configv1.Change_PATCH:
			b.fields[f.Name()] = diffField{patch: true}
		case f.IsList() && change.GetKind() == configv1.Change_LIST_CHANGE:
			b.fields[f.Name()] = listField(f.Message())
		}
	}
	bodies.Store(md, b)
	return b
}

// listField reads a keyed list's change wrapper.
func listField(wrapper protoreflect.MessageDescriptor) diffField {
	out := diffField{list: true}
	fields := wrapper.Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		src, _ := proto.GetExtension(f.Options(), configv1.E_Field).(*configv1.FieldSource)
		switch {
		case src.GetKey():
			out.key, out.itemKey = f, lastName(src.GetSource())
		case f.Name() == "patch":
			out.patchFd = f
		}
	}
	return out
}

func lastName(full string) protoreflect.Name {
	return protoreflect.FullName(full).Name()
}

// prune returns the part of m that the diff d touches.
func prune(m, d protoreflect.Message) pruned {
	b := bodyOf(d.Descriptor())
	fields := m.Descriptor().Fields()
	touched := map[protoreflect.Name]bool{}
	d.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Name() != "clear" {
			touched[fd.Name()] = true
			return true
		}
		for i := 0; i < v.List().Len(); i++ {
			if f := fields.ByNumber(protoreflect.FieldNumber(v.List().Get(i).Int())); f != nil {
				touched[f.Name()] = true
			}
		}
		return true
	})
	given := m.New()
	var restores []func(protoreflect.Message)
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if !touched[fd.Name()] {
			return true
		}
		dfd := d.Descriptor().Fields().ByName(fd.Name())
		if dfd == nil {
			given.Set(fd, v)
			return true
		}
		df, dv := b.fields[fd.Name()], d.Get(dfd)
		order := b.ordered[fd.Name()]
		switch {
		case df.patch && dv.Message().IsValid():
			sub := prune(v.Message(), dv.Message())
			given.Set(fd, protoreflect.ValueOfMessage(sub.given))
			restores = append(restores, func(r protoreflect.Message) {
				if r.Has(fd) {
					sub.restore(r.Mutable(fd).Message())
				}
			})
		case df.list && !fd.IsMap() && (order == nil || !d.Has(order)):
			given.Set(fd, protoreflect.ValueOfList(pruneList(given, fd, v.List(), dv.List(), df, &restores)))
		default:
			given.Set(fd, v)
		}
		return true
	})
	return pruned{given: given, restore: func(r protoreflect.Message) {
		for _, f := range restores {
			f(r)
		}
		putBackUntouched(r, m, touched)
	}}
}

// pruneList gives apply the items of a keyed list whose keys the changes
// name, each patched one pruned, and adds the restore that puts the list
// back together: the base's items in order, each touched one as apply left
// it or gone, then the items apply added, in its order.
func pruneList(given protoreflect.Message, fd protoreflect.FieldDescriptor, base, changes protoreflect.List, df diffField, restores *[]func(protoreflect.Message)) protoreflect.List {
	keyFd := fd.Message().Fields().ByName(df.itemKey)
	patched := map[any]protoreflect.Message{}
	named := map[any]bool{}
	for i := 0; i < changes.Len(); i++ {
		w := changes.Get(i).Message()
		k := w.Get(df.key).Interface()
		named[k] = true
		if df.patchFd != nil && w.Has(df.patchFd) {
			patched[k] = w.Get(df.patchFd).Message()
		}
	}
	list := given.NewField(fd).List()
	subs := map[any]pruned{}
	for i := 0; i < base.Len(); i++ {
		item := base.Get(i).Message()
		k := item.Get(keyFd).Interface()
		if !named[k] {
			continue
		}
		if p, ok := patched[k]; ok {
			sub := prune(item, p)
			// apply finds the item by its key, which the patch leaves be.
			sub.given.Set(keyFd, item.Get(keyFd))
			subs[k] = sub
			list.Append(protoreflect.ValueOfMessage(sub.given))
			continue
		}
		list.Append(protoreflect.ValueOfMessage(item))
	}
	*restores = append(*restores, func(r protoreflect.Message) {
		got := r.Get(fd).List()
		byKey := map[any]protoreflect.Message{}
		var added []protoreflect.Value
		for i := 0; i < got.Len(); i++ {
			item := got.Get(i).Message()
			byKey[item.Get(keyFd).Interface()] = item
		}
		out := r.NewField(fd).List()
		inBase := map[any]bool{}
		for i := 0; i < base.Len(); i++ {
			item := base.Get(i).Message()
			k := item.Get(keyFd).Interface()
			inBase[k] = true
			switch res, ok := byKey[k]; {
			case !named[k]:
				out.Append(protoreflect.ValueOfMessage(item))
			case ok:
				if sub, isPatched := subs[k]; isPatched {
					sub.restore(res)
				}
				out.Append(protoreflect.ValueOfMessage(res))
			}
		}
		for i := 0; i < got.Len(); i++ {
			if item := got.Get(i).Message(); !inBase[item.Get(keyFd).Interface()] {
				added = append(added, protoreflect.ValueOfMessage(item))
			}
		}
		for _, v := range added {
			out.Append(v)
		}
		if out.Len() > 0 {
			r.Set(fd, protoreflect.ValueOfList(out))
		} else {
			r.Clear(fd)
		}
	})
	return list
}

// putBackUntouched sets in r each field of m the diff didn't touch, unless
// the diff touched another field of its oneof: then the diff chose the
// case, and the old one is gone.
func putBackUntouched(r, m protoreflect.Message, touched map[protoreflect.Name]bool) {
	chosen := map[protoreflect.FullName]bool{}
	fields := m.Descriptor().Fields()
	for name := range touched {
		if f := fields.ByName(name); f != nil {
			if o := f.ContainingOneof(); o != nil && !o.IsSynthetic() {
				chosen[o.FullName()] = true
			}
		}
	}
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if touched[fd.Name()] {
			return true
		}
		if o := fd.ContainingOneof(); o != nil && !o.IsSynthetic() && chosen[o.FullName()] {
			return true
		}
		r.Set(fd, v)
		return true
	})
}
