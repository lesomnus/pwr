package pwrcel_test

import (
	"fmt"

	"cel.dev/cel-go/cel"
	"github.com/lesomnus/pwr"
	pwrcel "github.com/lesomnus/pwr/cel"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/typepb"
)

func ExampleAdapter() {
	schema, err := pwr.CompileMessage(&typepb.Type{})
	if err != nil {
		panic(err)
	}
	md := schema.Descriptor()
	adapter, err := pwrcel.New(md)
	if err != nil {
		panic(err)
	}
	options := append(adapter.EnvOptions(), cel.Variable("message", cel.ObjectType(string(md.FullName()))))
	env, err := cel.NewEnv(options...)
	if err != nil {
		panic(err)
	}
	ast, issues := env.Compile(`message.name == "Artifact" && message.fields.exists(f, f.name == "id")`)
	if issues.Err() != nil {
		panic(issues.Err())
	}
	program, err := env.Program(ast)
	if err != nil {
		panic(err)
	}

	raw, err := proto.Marshal(&typepb.Type{Name: "Artifact", Fields: []*typepb.Field{{Name: "id"}}})
	if err != nil {
		panic(err)
	}
	result, _, err := program.Eval(map[string]any{"message": schema.NewReader(raw)})
	if err != nil {
		panic(err)
	}
	fmt.Println(result)
	// Output: true
}
