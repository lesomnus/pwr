package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"testing"

	lab "example.com/cel-wire-lab"
)

var sink bool

type sample struct {
	Backend  string  `json:"backend"`
	Fixture  string  `json:"fixture"`
	Scenario string  `json:"scenario"`
	Stage    string  `json:"stage"`
	Rep      int     `json:"rep"`
	N        int     `json:"n"`
	NS       float64 `json:"ns"`
	Bytes    int64   `json:"bytes"`
	Allocs   int64   `json:"allocs"`
}
type benchCase struct {
	base sample
	run  func(*testing.B)
}

func main() {
	testing.Init()
	mode := flag.String("mode", "bench", "bench or check")
	duration := flag.String("duration", "100ms", "per sample")
	count := flag.Int("count", 3, "repetitions")
	seed := flag.Int64("seed", 1, "shuffle seed")
	backends := flag.String("backend", "wire_first_trusted,wire_last_trusted,descriptor_table_first,descriptor_table_last,incremental_index_first,incremental_index_last,go_generated_unmarshal", "exact backend names")
	flag.Parse()
	runtime.GOMAXPROCS(1)
	if *mode == "check" {
		if err := lab.CheckScenarios(); err != nil {
			panic(err)
		}
		fmt.Println("PASS: all scenarios match native CEL")
		return
	}
	flag.Set("test.benchtime", *duration)
	var cases []benchCase
	for _, c := range lab.ScanScenarios() {
		p, err := lab.Compile(c.Query, false)
		if err != nil {
			panic(err)
		}
		for _, f := range lab.ScanFixtures() {
			for _, backend := range lab.Backends {
				if !strings.Contains(","+*backends+",", ","+backend.Name+",") {
					continue
				}
				newSession, err := backend.Prepare(c.Fields)
				if err != nil {
					panic(err)
				}
				for _, stage := range []string{"direct", "cel"} {
					cases = append(cases, benchCase{sample{Backend: backend.Name, Fixture: f.Name, Scenario: c.Name, Stage: stage}, func(b *testing.B) {
						b.ReportAllocs()
						for b.Loop() {
							s, err := newSession(f.Raw)
							if err != nil {
								b.Fatal(err)
							}
							var ok bool
							if stage == "cel" {
								ok, err = lab.Eval(p, f.Event, s)
							} else {
								ok, err = c.Direct(f.Event, s)
							}
							s.Close()
							if err != nil {
								b.Fatal(err)
							}
							sink = ok
						}
					}})
				}
			}
		}
	}
	enc := json.NewEncoder(os.Stdout)
	rng := rand.New(rand.NewSource(*seed))
	for rep := 0; rep < *count; rep++ {
		rng.Shuffle(len(cases), func(i, j int) { cases[i], cases[j] = cases[j], cases[i] })
		for i, c := range cases {
			r := testing.Benchmark(c.run)
			v := c.base
			v.Rep = rep
			v.N = r.N
			v.NS = float64(r.T.Nanoseconds()) / float64(r.N)
			v.Bytes = r.AllocedBytesPerOp()
			v.Allocs = r.AllocsPerOp()
			if err := enc.Encode(v); err != nil {
				panic(err)
			}
			if i%25 == 0 {
				fmt.Fprintf(os.Stderr, "rep %d case %d/%d\n", rep+1, i+1, len(cases))
			}
		}
	}
}
