package lab

import (
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
	"unicode/utf8"
)

type wireSession struct {
	raw                   []byte
	first, alias, skipUTF bool
}

func (s *wireSession) Close() {}
func (s *wireSession) Get(fd protoreflect.FieldDescriptor) (string, error) {
	var value []byte
	for d := s.raw; len(d) > 0; {
		num, typ, n := protowire.ConsumeTag(d)
		if n < 0 {
			return "", protowire.ParseError(n)
		}
		d = d[n:]
		if num == fd.Number() && typ == protowire.BytesType {
			x, n := protowire.ConsumeBytes(d)
			if n < 0 {
				return "", protowire.ParseError(n)
			}
			if !s.skipUTF && !utf8.Valid(x) {
				return "", fmt.Errorf("invalid UTF8")
			}
			value = x
			d = d[n:]
			if s.first {
				break
			}
		} else {
			n := protowire.ConsumeFieldValue(num, typ, d)
			if n < 0 {
				return "", protowire.ParseError(n)
			}
			d = d[n:]
		}
	}
	if s.alias {
		return StringView(value), nil
	}
	return string(value), nil
}

// planSession scans once and caches offsets for fields resolved from descriptors.
type planSession struct {
	raw    []byte
	values [4]string
	fields []protoreflect.FieldDescriptor
}

func (s *planSession) Close() {}
func (s *planSession) Get(fd protoreflect.FieldDescriptor) (string, error) {
	for i, f := range s.fields {
		if f.Number() == fd.Number() {
			return s.values[i], nil
		}
	}
	return "", fmt.Errorf("unplanned field")
}
func preparePlan(fields []protoreflect.FieldDescriptor, first bool) func([]byte) (Session, error) {
	return func(raw []byte) (Session, error) {
		s := &planSession{raw: raw, fields: fields}
		seen := uint(0)
		all := uint(1<<len(fields)) - 1
		for d := raw; len(d) > 0; {
			num, typ, n := protowire.ConsumeTag(d)
			if n < 0 {
				return nil, protowire.ParseError(n)
			}
			d = d[n:]
			idx := -1
			for i, fd := range fields {
				if fd.Number() == num && typ == protowire.BytesType {
					idx = i
					break
				}
			}
			if idx >= 0 && (!first || seen&(1<<idx) == 0) {
				x, n := protowire.ConsumeBytes(d)
				if n < 0 {
					return nil, protowire.ParseError(n)
				}
				if !utf8.Valid(x) {
					return nil, fmt.Errorf("invalid UTF8")
				}
				s.values[idx] = StringView(x)
				seen |= 1 << idx
				d = d[n:]
				if first && seen == all {
					break
				}
			} else {
				n := protowire.ConsumeFieldValue(num, typ, d)
				if n < 0 {
					return nil, protowire.ParseError(n)
				}
				d = d[n:]
			}
		}
		return s, nil
	}
}
