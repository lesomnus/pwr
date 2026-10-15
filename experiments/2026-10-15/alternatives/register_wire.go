//go:build all || wire

package lab

import "google.golang.org/protobuf/reflect/protoreflect"

func init() {
	for _, mode := range []struct {
		name               string
		first, alias, skip bool
	}{{"wire_last_copy", false, false, false}, {"wire_last_alias", false, true, false}, {"wire_first_alias", true, true, false}, {"wire_first_trusted", true, true, true}} {
		m := mode
		Register(Backend{Name: m.name, FirstWins: m.first, ValidUTF8Assumed: m.skip, Prepare: func(_ []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
			return func(raw []byte) (Session, error) { return &wireSession{raw, m.first, m.alias, m.skip}, nil }, nil
		}})
	}
	for _, first := range []bool{false, true} {
		name := "plan_last_alias"
		if first {
			name = "plan_first_alias"
		}
		Register(Backend{Name: name, FirstWins: first, Prepare: func(f []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
			return preparePlan(f, first), nil
		}})
	}
}
