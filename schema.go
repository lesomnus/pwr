package pwr

import (
	"errors"
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	ErrInvalidSchema    = errors.New("pwr: invalid schema")
	ErrUnknownField     = errors.New("pwr: unknown field")
	ErrUnsupportedField = errors.New("pwr: incompatible field access")
	ErrInvalidWire      = errors.New("pwr: invalid wire data")
	ErrInvalidUTF8      = errors.New("pwr: invalid UTF-8")
	ErrInputTooLarge    = errors.New("pwr: input must be smaller than 2 GiB")
	ErrLimitExceeded    = errors.New("pwr: read limit exceeded")
	ErrInvalidOption    = errors.New("pwr: invalid option")
	ErrIndex            = errors.New("pwr: index out of range")
)

type field struct {
	descriptor protoreflect.FieldDescriptor
	wire       protowire.Type
	child      *Schema
	oneof      int
}

func (f field) composite() bool {
	return f.descriptor.IsList() || f.descriptor.IsMap() || f.child != nil
}

// Schema is an immutable descriptor graph and field-number dispatch plan.
// Use Compile to construct it. Schemas may be shared across goroutines.
type Schema struct {
	descriptor protoreflect.MessageDescriptor
	dense      []int
	sparse     map[protoreflect.FieldNumber]int
	fields     []field
}

// Compile reuses existing descriptors, including recursive message graphs.
// It does not parse .proto files or decode messages.
func Compile(md protoreflect.MessageDescriptor) (*Schema, error) {
	return compile(md, make(map[protoreflect.MessageDescriptor]*Schema))
}
func compile(md protoreflect.MessageDescriptor, memo map[protoreflect.MessageDescriptor]*Schema) (*Schema, error) {
	if md == nil || md.IsPlaceholder() {
		return nil, ErrInvalidSchema
	}
	if s := memo[md]; s != nil {
		return s, nil
	}
	s := &Schema{descriptor: md, fields: make([]field, md.Fields().Len())}
	memo[md] = s
	var max protoreflect.FieldNumber
	for i := range s.fields {
		fd := md.Fields().Get(i)
		if !protowire.Number(fd.Number()).IsValid() {
			return nil, fmt.Errorf("%w: field %d", ErrInvalidSchema, fd.Number())
		}
		f := field{descriptor: fd, wire: wireType(fd.Kind())}
		if o := fd.ContainingOneof(); o != nil && !o.IsSynthetic() {
			f.oneof = o.Index() + 1
		}
		if fd.Message() != nil {
			var err error
			f.child, err = compile(fd.Message(), memo)
			if err != nil {
				return nil, err
			}
		}
		s.fields[i] = f
		if fd.Number() > max {
			max = fd.Number()
		}
	}
	if max <= 4096 {
		s.dense = make([]int, int(max)+1)
		for i, f := range s.fields {
			s.dense[f.descriptor.Number()] = i + 1
		}
	} else {
		s.sparse = make(map[protoreflect.FieldNumber]int, len(s.fields))
		for i, f := range s.fields {
			s.sparse[f.descriptor.Number()] = i + 1
		}
	}
	return s, nil
}

// Descriptor returns the descriptor used to compile this schema.
func (s *Schema) Descriptor() protoreflect.MessageDescriptor {
	if s == nil {
		return nil
	}
	return s.descriptor
}
func (s *Schema) slot(n protoreflect.FieldNumber) int {
	if n <= 0 {
		return -1
	}
	if s.sparse != nil {
		return s.sparse[n] - 1
	}
	if int64(n) < int64(len(s.dense)) {
		return s.dense[n] - 1
	}
	return -1
}
func wireType(k protoreflect.Kind) protowire.Type {
	switch k {
	case protoreflect.DoubleKind, protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind:
		return protowire.Fixed64Type
	case protoreflect.FloatKind, protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind:
		return protowire.Fixed32Type
	case protoreflect.StringKind, protoreflect.BytesKind, protoreflect.MessageKind:
		return protowire.BytesType
	case protoreflect.GroupKind:
		return protowire.StartGroupType
	default:
		return protowire.VarintType
	}
}
func (f field) accepts(t protowire.Type) bool {
	return t == f.wire || (f.descriptor.IsList() && f.wire != protowire.BytesType && f.child == nil && t == protowire.BytesType)
}
