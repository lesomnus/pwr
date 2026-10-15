package lab

import (
	"github.com/lesomnus/cr/api"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

type messageSession struct{ m protoreflect.Message }

func (s *messageSession) Get(fd protoreflect.FieldDescriptor) (string, error) {
	return s.m.Get(fd).String(), nil
}
func (s *messageSession) Close() {}
func init() {
	Register(Backend{Name: "go_generated_unmarshal", Prepare: func(_ []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
		return func(raw []byte) (Session, error) {
			m := &api.Manifest{}
			if err := proto.Unmarshal(raw, m); err != nil {
				return nil, err
			}
			return &messageSession{m.ProtoReflect()}, nil
		}, nil
	}, Note: "generated protobuf decode once per event, reused by CEL"})
	Register(Backend{Name: "go_dynamicpb", Prepare: func(_ []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
		return func(raw []byte) (Session, error) {
			m := dynamicpb.NewMessage(Descriptor)
			if err := proto.Unmarshal(raw, m); err != nil {
				return nil, err
			}
			return &messageSession{m}, nil
		}, nil
	}, Note: "standard descriptor reused; full dynamic decode"})
}
