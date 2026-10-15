package pwrcel

import (
	"strings"
	"testing"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"github.com/lesomnus/pwr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/typepb"
)

var benchmarkResult ref.Val

func BenchmarkEvaluation(b *testing.B) {
	m := &typepb.Type{Name: "Artifact"}
	for i := 0; i < 64; i++ {
		m.Fields = append(m.Fields, &typepb.Field{Name: strings.Repeat("x", 64), Number: int32(i + 1), Kind: typepb.Field_TYPE_STRING})
	}
	md := m.ProtoReflect().Descriptor()
	a := adapter(b, md)
	s := schema(b, md)
	raw := encoded(b, m)
	for _, scenario := range []struct{ name, expr string }{
		{"name", `m.name == "Artifact"`},
		{"all_children", `m.fields.exists(f, f.number == 64)`},
	} {
		b.Run(scenario.name, func(b *testing.B) {
			decl := cel.Variable("m", cel.ObjectType(string(md.FullName())))
			p := program(b, a, scenario.expr, decl)
			env, err := cel.NewEnv(cel.Types(m), decl)
			if err != nil {
				b.Fatal(err)
			}
			ast, issues := env.Compile(scenario.expr)
			if issues.Err() != nil {
				b.Fatal(issues.Err())
			}
			native, err := env.Program(ast)
			if err != nil {
				b.Fatal(err)
			}
			for _, mode := range []string{"native_decoded", "native_unmarshal", "adapter_decoded", "reader", "unique", "reset_unique"} {
				b.Run(mode, func(b *testing.B) {
					eval := p
					if strings.HasPrefix(mode, "native_") {
						eval = native
					}
					vars := map[string]any{"m": m}
					r := s.NewReader(nil, pwr.AssumeUniqueScalars())
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						switch mode {
						case "native_decoded", "adapter_decoded":
							vars["m"] = m
						case "native_unmarshal":
							v := &typepb.Type{}
							if e := proto.Unmarshal(raw, v); e != nil {
								b.Fatal(e)
							}
							vars["m"] = v
						case "reader":
							vars["m"] = s.NewReader(raw)
						case "unique":
							vars["m"] = s.NewReader(raw, pwr.AssumeUniqueScalars())
						case "reset_unique":
							if e := r.Reset(raw); e != nil {
								b.Fatal(e)
							}
							vars["m"] = r
						}
						v, _, err := eval.Eval(vars)
						if err != nil || v != types.True {
							b.Fatal(v, err)
						}
						benchmarkResult = v
					}
				})
			}
		})
	}
}
