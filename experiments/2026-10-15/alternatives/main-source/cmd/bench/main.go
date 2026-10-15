package main

import (
	"encoding/json"
	lab "example.com/cel-wire-lab"
	"flag"
	"fmt"
	"github.com/lesomnus/cr/api"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

var sink bool

type sample struct {
	Backend   string  `json:"backend"`
	Fixture   string  `json:"fixture"`
	Stage     string  `json:"stage"`
	Fields    int     `json:"fields"`
	Rep       int     `json:"rep"`
	N         int     `json:"n"`
	NS        float64 `json:"ns"`
	Bytes     int64   `json:"bytes"`
	Allocs    int64   `json:"allocs"`
	PrepareNS int64   `json:"prepare_ns"`
	Payload   int     `json:"payload"`
	Error     string  `json:"error,omitempty"`
}
type benchCase struct {
	base sample
	fn   func(*testing.B)
}

func main() {
	testing.Init()
	mode := flag.String("mode", "bench", "check, list, bench")
	duration := flag.String("duration", "100ms", "per case")
	count := flag.Int("count", 3, "samples")
	seed := flag.Int64("seed", 1, "order seed")
	filter := flag.String("backend", "", "comma-separated exact names")
	fixtures := flag.String("fixtures", "", "comma-separated fixture names")
	stages := flag.String("stages", "direct,cel", "stages")
	flag.Parse()
	runtime.GOMAXPROCS(1)
	enc := json.NewEncoder(os.Stdout)
	if *mode == "list" {
		for _, b := range lab.Backends {
			enc.Encode(map[string]any{"backend": b.Name, "first_wins": b.FirstWins, "valid_utf8_assumed": b.ValidUTF8Assumed, "note": b.Note})
		}
		return
	}
	if *mode == "check" {
		if e := lab.Check(); e != nil {
			fmt.Fprintln(os.Stderr, e)
			os.Exit(1)
		}
		fmt.Println("PASS: fixtures, fields, CEL and duplicate semantics")
		return
	}
	flag.Set("test.benchtime", *duration)
	matches := func(list, name string) bool { return list == "" || strings.Contains(","+list+",", ","+name+",") }
	var cases []benchCase
	for _, n := range []int{1, 4} {
		countFields := n
		program, e := lab.Program(n, false)
		if e != nil {
			panic(e)
		}
		native, e := lab.Program(n, true)
		if e != nil {
			panic(e)
		}
		for _, f := range lab.Fixtures() {
			f := f
			if !matches(*fixtures, f.Name) {
				continue
			}
			for _, backend := range lab.Backends {
				backend := backend
				if !matches(*filter, backend.Name) {
					continue
				}
				start := time.Now()
				newSession, e := backend.Prepare(lab.Fields[:n])
				prep := time.Since(start).Nanoseconds()
				if e != nil {
					panic(e)
				}
				for _, stage := range []string{"direct", "cel"} {
					stage := stage
					if !matches(*stages, stage) {
						continue
					}
					base := sample{Backend: backend.Name, Fixture: f.Name, Stage: stage, Fields: n, PrepareNS: prep, Payload: len(f.Raw)}
					cases = append(cases, benchCase{base, func(b *testing.B) {
						b.ReportAllocs()
						for b.Loop() {
							s, e := newSession(f.Raw)
							if e != nil {
								b.Fatal(e)
							}
							var ok bool
							if stage == "direct" {
								ok, e = lab.Direct(s, countFields)
							} else {
								ok, e = lab.Eval(program, f.Event, s)
							}
							s.Close()
							if e != nil {
								b.Fatal(e)
							}
							sink = ok
						}
					}})
				}
			}
			if matches(*filter, "native_cel_any") && matches(*stages, "cel") {
				cases = append(cases, benchCase{sample{Backend: "native_cel_any", Fixture: f.Name, Stage: "cel", Fields: n, Payload: len(f.Raw)}, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						ok, e := lab.Eval(native, f.Event, nil)
						if e != nil {
							b.Fatal(e)
						}
						sink = ok
					}
				}})
			}
			if matches(*filter, "decoded_reuse") {
				m := &api.Manifest{}
				if e := proto.Unmarshal(f.Raw, m); e != nil {
					panic(e)
				}
				s := &decoded{m.ProtoReflect()}
				for _, stage := range []string{"direct", "cel"} {
					stage := stage
					if !matches(*stages, stage) {
						continue
					}
					cases = append(cases, benchCase{sample{Backend: "decoded_reuse", Fixture: f.Name, Stage: stage, Fields: n, Payload: len(f.Raw)}, func(b *testing.B) {
						b.ReportAllocs()
						for b.Loop() {
							var ok bool
							var e error
							if stage == "direct" {
								ok, e = lab.Direct(s, countFields)
							} else {
								ok, e = lab.Eval(program, f.Event, s)
							}
							if e != nil {
								b.Fatal(e)
							}
							sink = ok
						}
					}})
				}
			}
		}
	}
	rng := rand.New(rand.NewSource(*seed))
	for rep := 0; rep < *count; rep++ {
		rng.Shuffle(len(cases), func(i, j int) { cases[i], cases[j] = cases[j], cases[i] })
		for i, c := range cases {
			r := testing.Benchmark(c.fn)
			v := c.base
			v.Rep = rep
			v.N = r.N
			v.NS = float64(r.T.Nanoseconds()) / float64(r.N)
			v.Bytes = r.AllocedBytesPerOp()
			v.Allocs = r.AllocsPerOp()
			if e := enc.Encode(v); e != nil {
				panic(e)
			}
			if i%24 == 0 {
				fmt.Fprintf(os.Stderr, "rep %d case %d/%d\n", rep+1, i+1, len(cases))
			}
		}
	}
}

type decoded struct{ m protoreflect.Message }

func (s *decoded) Get(f protoreflect.FieldDescriptor) (string, error) {
	return s.m.Get(f).String(), nil
}
func (s *decoded) Close() {}
