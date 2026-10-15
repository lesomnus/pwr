package pwr

import (
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
	"math"
	"unicode/utf8"
	"unsafe"
)

func (r *Reader) scalar(n protoreflect.FieldNumber, kinds ...protoreflect.Kind) (protoreflect.Value, error) {
	slot, f, err := r.lookup(n)
	if err != nil {
		return protoreflect.Value{}, err
	}
	match := len(kinds) == 0
	for _, k := range kinds {
		if f.descriptor.Kind() == k {
			match = true
		}
	}
	if !match || f.composite() {
		return protoreflect.Value{}, fmt.Errorf("%w: %d", ErrUnsupportedField, n)
	}
	if err = r.ensure(slot, f.oneof != 0); err != nil {
		return protoreflect.Value{}, err
	}
	if r.index[slot] == 0 {
		v := f.descriptor.Default()
		// Descriptor-owned defaults must not be exposed as mutable shared bytes.
		if f.descriptor.Kind() == protoreflect.BytesKind {
			return protoreflect.ValueOfBytes(append([]byte(nil), v.Bytes()...)), nil
		}
		return v, nil
	}
	return r.decode(f.descriptor.Kind(), r.bytes(r.index[slot]))
}
func (r *Reader) decode(kind protoreflect.Kind, b []byte) (protoreflect.Value, error) {
	var u uint64
	switch wireType(kind) {
	case protowire.VarintType:
		u, _ = protowire.ConsumeVarint(b)
	case protowire.Fixed32Type:
		v, _ := protowire.ConsumeFixed32(b)
		u = uint64(v)
	case protowire.Fixed64Type:
		u, _ = protowire.ConsumeFixed64(b)
	}
	switch kind {
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(u != 0), nil
	case protoreflect.EnumKind:
		return protoreflect.ValueOfEnum(protoreflect.EnumNumber(int32(u))), nil
	case protoreflect.Int32Kind, protoreflect.Sfixed32Kind:
		return protoreflect.ValueOfInt32(int32(u)), nil
	case protoreflect.Sint32Kind:
		return protoreflect.ValueOfInt32(int32(protowire.DecodeZigZag(uint64(uint32(u))))), nil
	case protoreflect.Int64Kind, protoreflect.Sfixed64Kind:
		return protoreflect.ValueOfInt64(int64(u)), nil
	case protoreflect.Sint64Kind:
		return protoreflect.ValueOfInt64(protowire.DecodeZigZag(u)), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		return protoreflect.ValueOfUint32(uint32(u)), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		return protoreflect.ValueOfUint64(u), nil
	case protoreflect.FloatKind:
		return protoreflect.ValueOfFloat32(math.Float32frombits(uint32(u))), nil
	case protoreflect.DoubleKind:
		return protoreflect.ValueOfFloat64(math.Float64frombits(u)), nil
	case protoreflect.BytesKind:
		return protoreflect.ValueOfBytes(b[:len(b):len(b)]), nil
	case protoreflect.StringKind:
		if !r.assumeUTF8 && !utf8.Valid(b) {
			r.err = ErrInvalidUTF8
			return protoreflect.Value{}, r.err
		}
		return protoreflect.ValueOfString(unsafe.String(unsafe.SliceData(b), len(b))), nil
	default:
		return protoreflect.Value{}, ErrUnsupportedField
	}
}

// String reads a singular string, including a real-oneof member.
func (r *Reader) String(n protoreflect.FieldNumber) (string, error) {
	v, e := r.scalar(n, protoreflect.StringKind)
	if e != nil {
		return "", e
	}
	return v.String(), nil
}

// Bytes returns borrowed, read-only bytes. Copy before modifying them.
func (r *Reader) Bytes(n protoreflect.FieldNumber) ([]byte, error) {
	v, e := r.scalar(n, protoreflect.BytesKind)
	if e != nil {
		return nil, e
	}
	return v.Bytes(), nil
}

// Bool reads a singular bool.
func (r *Reader) Bool(n protoreflect.FieldNumber) (bool, error) {
	v, e := r.scalar(n, protoreflect.BoolKind)
	if e != nil {
		return false, e
	}
	return v.Bool(), nil
}

// Enum returns the numeric enum value, including unknown numbers.
func (r *Reader) Enum(n protoreflect.FieldNumber) (protoreflect.EnumNumber, error) {
	v, e := r.scalar(n, protoreflect.EnumKind)
	if e != nil {
		return 0, e
	}
	return v.Enum(), nil
}

// Int32 reads int32, sint32, or sfixed32.
func (r *Reader) Int32(n protoreflect.FieldNumber) (int32, error) {
	v, e := r.scalar(n, protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind)
	if e != nil {
		return 0, e
	}
	return int32(v.Int()), nil
}

// Int64 reads int64, sint64, or sfixed64.
func (r *Reader) Int64(n protoreflect.FieldNumber) (int64, error) {
	v, e := r.scalar(n, protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind)
	if e != nil {
		return 0, e
	}
	return v.Int(), nil
}

// Uint32 reads uint32 or fixed32.
func (r *Reader) Uint32(n protoreflect.FieldNumber) (uint32, error) {
	v, e := r.scalar(n, protoreflect.Uint32Kind, protoreflect.Fixed32Kind)
	if e != nil {
		return 0, e
	}
	return uint32(v.Uint()), nil
}

// Uint64 reads uint64 or fixed64.
func (r *Reader) Uint64(n protoreflect.FieldNumber) (uint64, error) {
	v, e := r.scalar(n, protoreflect.Uint64Kind, protoreflect.Fixed64Kind)
	if e != nil {
		return 0, e
	}
	return v.Uint(), nil
}

// Float32 reads float.
func (r *Reader) Float32(n protoreflect.FieldNumber) (float32, error) {
	v, e := r.scalar(n, protoreflect.FloatKind)
	if e != nil {
		return 0, e
	}
	return float32(v.Float()), nil
}

// Float64 reads double.
func (r *Reader) Float64(n protoreflect.FieldNumber) (float64, error) {
	v, e := r.scalar(n, protoreflect.DoubleKind)
	if e != nil {
		return 0, e
	}
	return v.Float(), nil
}
