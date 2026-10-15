//go:build all || molecule

package lab

import (
	"github.com/richardartoul/molecule"
	"github.com/richardartoul/molecule/src/codec"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type moleculeSession struct {
	raw   []byte
	first bool
}

func (s *moleculeSession) Close() {}
func (s *moleculeSession) Get(fd protoreflect.FieldDescriptor) (string, error) {
	buf := codec.NewBuffer(s.raw)
	var out string
	err := molecule.MessageEach(buf, func(num int32, v molecule.Value) (bool, error) {
		if num != int32(fd.Number()) {
			return true, nil
		}
		str, e := v.AsStringUnsafe()
		out = str
		return !s.first, e
	})
	return out, err
}
func init() {
	for _, first := range []bool{false, true} {
		name := "molecule_last"
		if first {
			name = "molecule_first"
		}
		Register(Backend{Name: name, FirstWins: first, ValidUTF8Assumed: true, Prepare: func(_ []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
			return func(raw []byte) (Session, error) { return &moleculeSession{raw, first}, nil }, nil
		}})
	}
}
