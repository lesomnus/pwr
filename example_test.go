package pwr_test

import (
	"fmt"

	"github.com/lesomnus/pwr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/typepb"
)

func Example() {
	// Use a protobuf message to select its type. Type.name is field 1; Type.fields
	// is a repeated message. The latter can be skipped without decoding it.
	schema, err := pwr.CompileMessage(&typepb.Type{})
	if err != nil {
		panic(err)
	}
	raw, err := proto.Marshal(&typepb.Type{Name: "demo.Record", Fields: []*typepb.Field{{Name: "payload"}}})
	if err != nil {
		panic(err)
	}
	r := schema.NewReader(raw, pwr.AssumeUniqueScalars())
	name, err := r.String(1)
	if err != nil {
		panic(err)
	}
	fmt.Println(name)
	// Output: demo.Record
}
