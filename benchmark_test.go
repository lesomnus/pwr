package pwr_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lesomnus/pwr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/typepb"
)

var benchmarkName string

// Read just the leading name while varying the number of unrequested children.
// Setup and descriptor compilation are outside the timed region.
func BenchmarkName(b *testing.B) {
	for _, children := range []int{0, 64} {
		b.Run(fmt.Sprintf("children=%d", children), func(b *testing.B) {
			m := &typepb.Type{Name: "Artifact"}
			for i := 0; i < children; i++ {
				m.Fields = append(m.Fields, &typepb.Field{Name: strings.Repeat("x", 64), Number: int32(i + 1), Kind: typepb.Field_TYPE_STRING})
			}
			raw, err := proto.Marshal(m)
			if err != nil {
				b.Fatal(err)
			}
			s, err := pwr.Compile(m.ProtoReflect().Descriptor())
			if err != nil {
				b.Fatal(err)
			}
			for _, mode := range []string{"unmarshal", "reader", "unique", "reset_unique", "pool_unique"} {
				b.Run(mode, func(b *testing.B) {
					r := s.NewReader(nil, pwr.AssumeUniqueScalars())
					pool := s.NewPool(pwr.AssumeUniqueScalars())
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						var name string
						var err error
						switch mode {
						case "unmarshal":
							v := &typepb.Type{}
							err = proto.Unmarshal(raw, v)
							name = v.Name
						case "reader":
							name, err = s.NewReader(raw).String(1)
						case "unique":
							name, err = s.NewReader(raw, pwr.AssumeUniqueScalars()).String(1)
						case "reset_unique":
							err = r.Reset(raw)
							if err == nil {
								name, err = r.String(1)
							}
						case "pool_unique":
							r = pool.Get(raw)
							name, err = r.String(1)
							if e := pool.Put(r); e != nil {
								b.Fatal(e)
							}
						}
						if err != nil || name != "Artifact" {
							b.Fatal(name, err)
						}
						benchmarkName = name
					}
				})
			}
		})
	}
}
