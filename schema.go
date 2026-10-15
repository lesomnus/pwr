package pwr

import (
	"errors"
	"fmt"

	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	ErrInvalidSchema    = errors.New("pwr: invalid schema")
	ErrUnknownField     = errors.New("pwr: unknown field")
	ErrUnsupportedField = errors.New("pwr: unsupported field")
	ErrInvalidWire      = errors.New("pwr: invalid wire data")
	ErrInvalidUTF8      = errors.New("pwr: invalid UTF-8")
	ErrInputTooLarge    = errors.New("pwr: input must be smaller than 2 GiB")
)

type field struct {
	stringValue  bool
	defaultValue string
}

// Schema is an immutable field-number dispatch plan. Use Compile to construct
// it. A Schema can be shared by readers in different goroutines.
type Schema struct {
	descriptor protoreflect.MessageDescriptor
	dense      []int
	sparse     map[protoreflect.FieldNumber]int
	fields     []field
}

// Compile prepares a dispatch plan from an already instantiated descriptor.
// It does not parse a .proto file, generate code, or decode a message.
// Unsupported fields may coexist with readable strings in the same message.
func Compile(md protoreflect.MessageDescriptor) (*Schema, error) {
	if md == nil || md.IsPlaceholder() {
		return nil, ErrInvalidSchema
	}
	s := &Schema{descriptor: md, fields: make([]field, md.Fields().Len())}
	max := protoreflect.FieldNumber(0)
	for i := range s.fields {
		fd := md.Fields().Get(i)
		if fd.Number() <= 0 || fd.Number() > 536870911 {
			return nil, fmt.Errorf("%w: field number %d", ErrInvalidSchema, fd.Number())
		}
		if fd.Number() > max {
			max = fd.Number()
		}
		oneof := fd.ContainingOneof()
		if fd.Kind() == protoreflect.StringKind && !fd.IsList() && (oneof == nil || oneof.IsSynthetic()) {
			s.fields[i] = field{stringValue: true, defaultValue: fd.Default().String()}
		}
	}
	if max <= 4096 {
		s.dense = make([]int, int(max)+1)
		for i := range s.fields {
			s.dense[md.Fields().Get(i).Number()] = i + 1
		}
	} else {
		s.sparse = make(map[protoreflect.FieldNumber]int, len(s.fields))
		for i := range s.fields {
			s.sparse[md.Fields().Get(i).Number()] = i + 1
		}
	}
	return s, nil
}

func (s *Schema) slot(number protoreflect.FieldNumber) int {
	if number <= 0 {
		return -1
	}
	if s.sparse != nil {
		return s.sparse[number] - 1
	}
	if int64(number) < int64(len(s.dense)) {
		return s.dense[number] - 1
	}
	return -1
}
