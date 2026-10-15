package pwr

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/typepb"
)

func TestCompileMessageIgnoresContents(t *testing.T) {
	_, md := fixtureSchema(t)
	dynamic := dynamicpb.NewMessage(md)
	dynamic.Set(md.Fields().ByNumber(1), protoreflect.ValueOfString("prototype"))
	for _, message := range []proto.Message{&typepb.Type{Name: "prototype"}, dynamic} {
		before := proto.Clone(message)
		s, err := CompileMessage(message)
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(message, before) {
			t.Fatal("compilation changed the prototype")
		}
		proto.Reset(message)
		for _, raw := range [][]byte{nil, put(nil, 1, "from wire")} {
			want := ""
			if raw != nil {
				want = "from wire"
			}
			got, err := s.NewReader(raw).String(1)
			if err != nil || got != want {
				t.Fatalf("%T: got %q, want %q, error %v", message, got, want, err)
			}
		}
	}
}

func TestCompileMessageInvalid(t *testing.T) {
	for _, message := range []proto.Message{nil, (*typepb.Type)(nil), (*dynamicpb.Message)(nil), &dynamicpb.Message{}} {
		s, err := CompileMessage(message)
		if s != nil || !errors.Is(err, ErrInvalidSchema) {
			t.Fatalf("%T: schema %v, error %v", message, s, err)
		}
	}
}
