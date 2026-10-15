//go:build all || rustffi

package lab

import (
	"example.com/cel-wire-lab/internal/rbridge"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type rbridgesession struct {
	raw   []byte
	first bool
}

func (s *rbridgesession) Close() {}
func (s *rbridgesession) Get(fd protoreflect.FieldDescriptor) (string, error) {
	start, n, e := rbridge.Single(s.raw, uint32(fd.Number()), s.first)
	if e != nil {
		return "", e
	}
	return StringView(s.raw[start : start+n]), nil
}
func init() {
	for _, first := range []bool{false, true} {
		suffix := "last"
		if first {
			suffix = "first"
		}
		Register(Backend{Name: "rust_generic_" + suffix, FirstWins: first, ValidUTF8Assumed: true, Prepare: func(_ []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
			return func(raw []byte) (Session, error) { return &rbridgesession{raw, first}, nil }, nil
		}, Note: "one cgo call per field; no UTF8 validation"})
		Register(Backend{Name: "rust_codegen_" + suffix, FirstWins: first, ValidUTF8Assumed: true, Prepare: func(fields []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
			mask := uint32(1<<len(fields)) - 1
			return func(raw []byte) (Session, error) {
				spans, e := rbridge.Project(raw, mask, first)
				if e != nil {
					return nil, e
				}
				s := &planSession{raw: raw, fields: fields}
				for i, p := range spans {
					s.values[i] = StringView(raw[p[0] : p[0]+p[1]])
				}
				return s, nil
			}, nil
		}, Note: "generated projection; one cgo call per event; no UTF8 validation"})
	}
}
