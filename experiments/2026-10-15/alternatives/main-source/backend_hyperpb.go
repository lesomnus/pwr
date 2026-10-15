//go:build all || hyperpb

package lab

import (
	"buf.build/go/hyperpb"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func init() {
	for _, alias := range []bool{false, true} {
		name := "hyperpb_copy"
		if alias {
			name = "hyperpb_alias"
		}
		Register(Backend{Name: name, Prepare: func(_ []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
			ty := hyperpb.CompileMessageDescriptor(Descriptor)
			return func(raw []byte) (Session, error) {
				m := hyperpb.NewMessage(ty)
				if e := m.Unmarshal(raw, hyperpb.WithAllowAlias(alias)); e != nil {
					return nil, e
				}
				return &messageSession{m}, nil
			}, nil
		}, Note: "parser compiled from existing descriptor; full decode"})
	}
}
