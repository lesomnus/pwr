//go:build all || dynamicgo

package lab

import (
	"context"
	"fmt"
	dp "github.com/cloudwego/dynamicgo/proto"
	"github.com/cloudwego/dynamicgo/proto/generic"
	"google.golang.org/protobuf/reflect/protoreflect"
	"strings"
)

type dynamicSession struct{ v generic.Value }

func (s *dynamicSession) Close() {}
func (s *dynamicSession) Get(fd protoreflect.FieldDescriptor) (string, error) {
	v := s.v.GetByPath(generic.NewPathFieldId(dp.FieldNumber(fd.Number())))
	if v.IsErrNotFound() {
		return "", nil
	}
	return v.String()
}
func init() {
	Register(Backend{Name: "dynamicgo_path", FirstWins: true, ValidUTF8Assumed: true, Prepare: func(fields []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
		var src strings.Builder
		src.WriteString(`syntax="proto3";package app;message Manifest{`)
		for _, f := range fields {
			fmt.Fprintf(&src, "string %s=%d;", f.Name(), f.Number())
		}
		src.WriteString(`} service Probe{rpc Read(Manifest)returns(Manifest);}`)
		sd, e := dp.NewDescritorFromContent(context.Background(), "probe.proto", src.String(), map[string]string{})
		if e != nil {
			return nil, e
		}
		desc := sd.LookupMethodByName("Read").Input()
		return func(raw []byte) (Session, error) { return &dynamicSession{generic.NewRootValue(desc, raw)}, nil }, nil
	}, Note: "one-time projection schema conversion from registered descriptors to DynamicGo descriptor"})
}
