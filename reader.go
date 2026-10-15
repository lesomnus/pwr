package pwr

import (
	"fmt"
	"math"
	"unicode/utf8"
	"unsafe"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ReaderOption configures a newly constructed Reader.
type ReaderOption func(*Reader)

// AssumeUniqueScalars lets a reader stop at the first occurrence of a requested
// scalar. The caller promises that singular scalar fields do not repeat. This
// option does not check that promise or imply any ordering of field numbers.
// Repeated fields and embedded-message merge semantics are not covered by it.
func AssumeUniqueScalars() ReaderOption {
	return func(r *Reader) { r.first = true }
}

// AssumeValidUTF8 skips validation of returned strings. The caller promises
// that string fields contain valid UTF-8. Bounds checks remain enabled.
func AssumeValidUTF8() ReaderOption {
	return func(r *Reader) { r.assumeUTF8 = true }
}

// Reader holds a message's scan cursor, cached offsets, and EOF/error state.
// It must not be used concurrently. The input and all strings returned from it
// share storage; do not mutate or recycle the input while either is in use.
type Reader struct {
	schema     *Schema
	raw        []byte
	index      []uint64
	cursor     int
	first      bool
	assumeUTF8 bool
	done       bool
	err        error
}

// NewReader borrows raw without scanning it. By default the first String call
// scans to EOF to select the last scalar occurrence. Wire errors are returned
// by String. A nil/zero Schema or oversized input also fails on String.
func (s *Schema) NewReader(raw []byte, options ...ReaderOption) *Reader {
	r := &Reader{schema: s, raw: raw}
	for _, option := range options {
		if option != nil {
			option(r)
		}
	}
	if s == nil || s.descriptor == nil {
		r.err = ErrInvalidSchema
	} else if uint64(len(raw)) > math.MaxInt32 {
		r.err = ErrInputTooLarge
	}
	return r
}

// String returns a singular string field by number. Missing fields return
// their descriptor default, which is usually "". Presence is not reported.
// Repeated, message, numeric, and real-oneof fields return ErrUnsupportedField.
// Unknown numbers return ErrUnknownField without poisoning the reader.
//
// Validation is selective: it checks encountered wire boundaries and the UTF-8
// of the returned string, not the complete message or skipped nested payloads.
func (r *Reader) String(number protoreflect.FieldNumber) (string, error) {
	if r == nil || r.schema == nil {
		return "", ErrInvalidSchema
	}
	if r.err != nil {
		return "", r.err
	}
	slot := r.schema.slot(number)
	if slot < 0 {
		return "", fmt.Errorf("%w: %d", ErrUnknownField, number)
	}
	f := r.schema.fields[slot]
	if !f.stringValue {
		return "", fmt.Errorf("%w: %d is not a singular string outside a real oneof", ErrUnsupportedField, number)
	}
	if r.index == nil {
		r.index = make([]uint64, len(r.schema.fields))
	}
	if !r.done && (!r.first || r.index[slot] == 0) {
		if err := r.scan(slot); err != nil {
			return "", err
		}
	}
	x := r.index[slot]
	if x == 0 {
		return f.defaultValue, nil
	}
	start, end := int(uint32(x>>32)), int(uint32(x))
	value, n := protowire.ConsumeBytes(r.raw[start:end])
	if n < 0 {
		return "", r.failWire(protowire.ParseError(n))
	}
	if !r.assumeUTF8 && !utf8.Valid(value) {
		r.err = fmt.Errorf("%w: field %d", ErrInvalidUTF8, number)
		return "", r.err
	}
	return unsafe.String(unsafe.SliceData(value), len(value)), nil
}

func (r *Reader) failWire(err error) error {
	r.err = fmt.Errorf("%w at byte %d: %v", ErrInvalidWire, r.cursor, err)
	return r.err
}

func (r *Reader) scan(target int) error {
	for r.cursor < len(r.raw) {
		num, typ, n := protowire.ConsumeTag(r.raw[r.cursor:])
		if n < 0 {
			return r.failWire(protowire.ParseError(n))
		}
		if !num.IsValid() {
			return r.failWire(fmt.Errorf("invalid field number %d", num))
		}
		start := r.cursor + n
		k := protowire.ConsumeFieldValue(num, typ, r.raw[start:])
		if k < 0 {
			return r.failWire(protowire.ParseError(k))
		}
		r.cursor = start + k
		slot := r.schema.slot(num)
		if slot < 0 {
			continue
		}
		// As in the standard decoder, an incompatible wire type does not
		// replace the string's value; treat that record as unknown.
		if r.schema.fields[slot].stringValue && typ != protowire.BytesType {
			continue
		}
		if !r.first || r.index[slot] == 0 {
			r.index[slot] = uint64(uint32(start))<<32 | uint64(uint32(r.cursor))
		}
		if r.first && slot == target {
			return nil
		}
	}
	r.done = true
	return nil
}
