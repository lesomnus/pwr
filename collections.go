package pwr

import (
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// List is an immutable collection. Message elements remain lazy Readers.
type List struct{ values []Value }

func (l *List) Len() int {
	if l == nil {
		return 0
	}
	return len(l.values)
}
func (l *List) Get(i int) (Value, error) {
	if i < 0 || i >= l.Len() {
		return Value{}, ErrIndex
	}
	return l.values[i], nil
}
func (l *List) Range(f func(int, Value) bool) {
	if l != nil {
		for i, v := range l.values {
			if !f(i, v) {
				return
			}
		}
	}
}

// Map is an immutable protobuf map. Keys must have the descriptor's native Go
// type (e.g. int32 versus int64). Duplicate keys select the last complete entry.
// Range order is unspecified. Message values remain lazy Readers.
type Map struct {
	descriptor protoreflect.FieldDescriptor
	values     map[any]Value
}

func (m *Map) Len() int {
	if m == nil {
		return 0
	}
	return len(m.values)
}
func (m *Map) Descriptor() protoreflect.FieldDescriptor {
	if m == nil {
		return nil
	}
	return m.descriptor
}
func (m *Map) Get(k protoreflect.MapKey) (Value, bool) {
	if m == nil || !k.IsValid() {
		return Value{}, false
	}
	v, ok := m.values[k.Interface()]
	return v, ok
}
func (m *Map) Range(f func(protoreflect.MapKey, Value) bool) {
	if m != nil {
		for k, v := range m.values {
			if !f(protoreflect.ValueOf(k).MapKey(), v) {
				return
			}
		}
	}
}

// List collects a repeated field, preserving wire order across packed and
// unpacked occurrences. Decoded collections are cached for subsequent requests.
func (r *Reader) List(n protoreflect.FieldNumber) (*List, error) {
	slot, f, err := r.lookup(n)
	if err != nil {
		return nil, err
	}
	if !f.descriptor.IsList() {
		return nil, fmt.Errorf("%w: %d is not repeated", ErrUnsupportedField, n)
	}
	if err = r.ensure(slot, true); err != nil {
		return nil, err
	}
	if v, ok := r.cache[slot]; ok {
		return v.(*List), nil
	}
	out := &List{}
	err = r.records(slot, func(rec record) error {
		if f.child != nil {
			child, e := r.child(f.child, [][]byte{rec.data})
			if e != nil {
				return e
			}
			out.values = append(out.values, Value{descriptor: f.descriptor, kind: f.descriptor.Kind(), message: child})
			return nil
		}
		appendScalar := func(b []byte) error {
			v, e := r.decode(f.descriptor.Kind(), b)
			if e == nil {
				out.values = append(out.values, Value{descriptor: f.descriptor, kind: f.descriptor.Kind(), scalar: v})
			}
			return e
		}
		if rec.wire == f.wire {
			return appendScalar(rec.data)
		}
		for b := rec.data; len(b) > 0; {
			k := protowire.ConsumeFieldValue(rec.number, f.wire, b)
			if k < 0 {
				return r.failWire(protowire.ParseError(k))
			}
			if e := r.charge(0); e != nil {
				return e
			}
			if e := appendScalar(b[:k]); e != nil {
				return e
			}
			b = b[k:]
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	r.remember(slot, out)
	return out, nil
}

// Map collects a map field. Each entry applies scalar defaults and singular
// message merge rules independently; duplicate outer entries replace values.
func (r *Reader) Map(n protoreflect.FieldNumber) (*Map, error) {
	slot, f, err := r.lookup(n)
	if err != nil {
		return nil, err
	}
	if !f.descriptor.IsMap() {
		return nil, fmt.Errorf("%w: %d is not a map", ErrUnsupportedField, n)
	}
	if err = r.ensure(slot, true); err != nil {
		return nil, err
	}
	if v, ok := r.cache[slot]; ok {
		return v.(*Map), nil
	}
	out := &Map{descriptor: f.descriptor, values: make(map[any]Value)}
	err = r.records(slot, func(rec record) error {
		entry, e := r.child(f.child, [][]byte{rec.data})
		if e != nil {
			return e
		}
		// Entry fields may repeat, regardless of scalar assumptions for user messages.
		entry.first = false
		key, e := entry.scalar(1)
		if e != nil {
			return e
		}
		value, e := entry.Get(2)
		if e != nil {
			return e
		}
		if f.descriptor.MapValue().Message() != nil && value.message == nil {
			value.message, e = entry.child(f.child.fields[f.child.slot(2)].child, nil)
			if e != nil {
				return e
			}
		}
		out.values[key.Interface()] = value
		return nil
	})
	if err != nil {
		r.err = err
		return nil, err
	}
	r.remember(slot, out)
	return out, nil
}
