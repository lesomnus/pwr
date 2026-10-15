package pwr

import (
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// Materialize decodes a complete independent protobuf message, preserving unknown
// fields. This is an explicitly eager fallback for native conversion/equality.
// It validates all occurrences (including overwritten values), ignores the fast
// input assumptions, checks required fields after merging, and honors read limits.
func (r *Reader) Materialize() (proto.Message, error) {
	if err := r.status(); err != nil {
		return nil, err
	}
	if err := r.preflight(); err != nil {
		return nil, err
	}
	m := dynamicpb.NewMessage(r.schema.descriptor)
	opts := proto.UnmarshalOptions{Merge: true, AllowPartial: true, RecursionLimit: r.limits.MaxDepth - r.depth + 1}
	decode := func(b []byte) error { return opts.Unmarshal(b, m) }
	var err error
	if r.parts == nil {
		err = decode(r.raw)
	} else {
		for _, p := range r.parts {
			if err = decode(p.data); err != nil {
				break
			}
		}
	}
	if err != nil {
		return nil, r.failWire(err)
	}
	if err = proto.CheckInitialized(m); err != nil {
		return nil, r.failWire(err)
	}
	return m, nil
}

// Validate eagerly validates the full message with the standard Go protobuf
// decoder. Ordinary field access remains selective and does not call Validate.
func (r *Reader) Validate() error { _, err := r.Materialize(); return err }

// Preflight charges visits before the eager decoder can allocate values. It
// visits all known embedded occurrences, not only the final merged selection.
func (r *Reader) preflight() error {
	for pos := 0; pos < r.size; {
		rec, err := r.readRecord(pos)
		if err != nil {
			return err
		}
		pos = rec.end
		slot := r.schema.slot(rec.number)
		if slot < 0 {
			continue
		}
		f := r.schema.fields[slot]
		if !f.accepts(rec.wire) {
			continue
		}
		if f.child != nil {
			c, err := r.child(f.child, [][]byte{rec.data})
			if err != nil {
				return err
			}
			if err = c.preflight(); err != nil {
				r.err = err
				return err
			}
		} else if f.descriptor.IsList() && rec.wire == protowire.BytesType && f.descriptor.Kind() != protoreflect.StringKind && f.descriptor.Kind() != protoreflect.BytesKind {
			for b := rec.data; len(b) > 0; {
				n := protowire.ConsumeFieldValue(rec.number, f.wire, b)
				if n < 0 {
					return r.failWire(protowire.ParseError(n))
				}
				if err = r.charge(0); err != nil {
					return err
				}
				b = b[n:]
			}
		}
	}
	return nil
}
