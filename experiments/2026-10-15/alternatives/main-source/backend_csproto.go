//go:build all || csproto

package lab

import (
	"errors"
	"github.com/CrowdStrike/csproto"
	"github.com/CrowdStrike/csproto/lazyproto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type csSession struct{ r *lazyproto.DecodeResult }

func (s *csSession) Get(fd protoreflect.FieldDescriptor) (string, error) {
	v, e := s.r.FieldData(int(fd.Number()))
	if errors.Is(e, lazyproto.ErrTagNotFound) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	return v.StringValue()
}
func (s *csSession) Close() { s.r.Close() }
func init() {
	for _, fast := range []bool{false, true} {
		name := "csproto_safe"
		if fast {
			name = "csproto_fast"
		}
		Register(Backend{Name: name, ValidUTF8Assumed: true, Prepare: func(fields []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
			nums := []int{}
			for _, f := range fields {
				nums = append(nums, int(f.Number()))
			}
			mode := csproto.DecoderModeSafe
			if fast {
				mode = csproto.DecoderModeFast
			}
			dec, e := lazyproto.NewDecoder(lazyproto.NewDef(nums...), lazyproto.WithMode(mode))
			if e != nil {
				return nil, e
			}
			return func(raw []byte) (Session, error) {
				r, e := dec.Decode(raw)
				if e != nil {
					return nil, e
				}
				return &csSession{r}, nil
			}, nil
		}, Note: "selected raw fields; full outer scan; Fast controls buffer ownership"})
	}
}
