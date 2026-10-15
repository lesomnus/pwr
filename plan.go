package pwr

import (
	"fmt"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Plan is an immutable set of fields to index in advance. It does not decode
// their values. This is useful when an application already knows its access set.
type Plan struct {
	schema *Schema
	fields []protoreflect.FieldNumber
}

// Plan validates and copies a field selection. Empty selections do no scanning.
func (s *Schema) Plan(numbers ...protoreflect.FieldNumber) (*Plan, error) {
	if s == nil || s.descriptor == nil {
		return nil, ErrInvalidSchema
	}
	p := &Plan{schema: s}
	seen := make(map[protoreflect.FieldNumber]bool)
	for _, n := range numbers {
		if s.slot(n) < 0 {
			return nil, fmt.Errorf("%w: %d", ErrUnknownField, n)
		}
		if !seen[n] {
			p.fields = append(p.fields, n)
			seen[n] = true
		}
	}
	return p, nil
}

// Read constructs a reader and indexes the selection. In unique-scalar mode it
// stops when every selected non-oneof scalar is found; composites need EOF.
func (p *Plan) Read(raw []byte, options ...ReaderOption) (*Reader, error) {
	if p == nil || p.schema == nil {
		return nil, ErrInvalidSchema
	}
	r := p.schema.NewReader(raw, options...)
	if err := r.Prefetch(p.fields...); err != nil {
		return nil, err
	}
	return r, nil
}

// Prefetch indexes selected fields. Requests are validated before scanning.
// The cursor only advances, so already passed fields do not cause a rescan.
func (r *Reader) Prefetch(numbers ...protoreflect.FieldNumber) error {
	if err := r.status(); err != nil {
		return err
	}
	for _, n := range numbers {
		if _, _, err := r.lookup(n); err != nil {
			return err
		}
	}
	for _, n := range numbers {
		slot, f, _ := r.lookup(n)
		if err := r.ensure(slot, f.composite() || f.oneof != 0); err != nil {
			return err
		}
	}
	return nil
}

// Err reports a sticky reader/budget error without accessing the wire.
func (r *Reader) Err() error { return r.status() }

// View interprets a borrowed embedded wire image with another schema, sharing
// this reader's depth/work limits. This supports envelopes such as protobuf.Any.
func (r *Reader) View(s *Schema, raw []byte) (*Reader, error) {
	if err := r.status(); err != nil {
		return nil, err
	}
	if s == nil || s.descriptor == nil {
		return nil, ErrInvalidSchema
	}
	if uint64(len(raw)) > 1<<31-1 {
		return nil, ErrInputTooLarge
	}
	return r.child(s, [][]byte{raw})
}
