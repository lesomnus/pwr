package lab

import (
	"bytes"
	"fmt"
	"github.com/lesomnus/cr/api"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func Check() error {
	fixtures := Fixtures()
	for _, b := range Backends {
		for _, count := range []int{1, 4} {
			newSession, err := b.Prepare(Fields[:count])
			if err != nil {
				return fmt.Errorf("%s prepare: %w", b.Name, err)
			}
			p, err := Program(count, false)
			if err != nil {
				return err
			}
			for _, f := range fixtures {
				m := &api.Manifest{}
				if err := proto.Unmarshal(f.Raw, m); err != nil {
					return err
				}
				s, err := newSession(f.Raw)
				if err != nil {
					return fmt.Errorf("%s/%s create: %w", b.Name, f.Name, err)
				}
				for _, fd := range Fields[:count] {
					got, e := s.Get(fd)
					want := m.ProtoReflect().Get(fd).String()
					if e != nil || got != want {
						return fmt.Errorf("%s/%s field %s: %q != %q / %v", b.Name, f.Name, fd.Name(), got, want, e)
					}
				}
				want := f.Name != "missing"
				got, e := Eval(p, f.Event, s)
				s.Close()
				if e != nil || got != want {
					return fmt.Errorf("%s/%s CEL: %v != %v / %v", b.Name, f.Name, got, want, e)
				}
			}
			raw := bytes.Clone(fixtures[0].Raw)
			raw = protowire.AppendTag(raw, 11, protowire.BytesType)
			raw = protowire.AppendString(raw, "changed")
			s, e := newSession(raw)
			if e != nil {
				return e
			}
			got, e := s.Get(Fields[0])
			s.Close()
			want := "changed"
			if b.FirstWins {
				want = Artifact
			}
			if e != nil || got != want {
				return fmt.Errorf("%s duplicate %q != %q / %v", b.Name, got, want, e)
			}
		}
	}
	return nil
}
