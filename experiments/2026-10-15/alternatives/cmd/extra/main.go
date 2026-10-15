package main

import (
	"encoding/json"
	lab "example.com/cel-wire-lab"
	"example.com/cel-wire-lab/internal/cbridge"
	"example.com/cel-wire-lab/internal/rbridge"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"strings"
	"testing"
)

var sink bool

type row struct {
	Backend     string  `json:"backend"`
	Fixture     string  `json:"fixture"`
	Kind        string  `json:"kind"`
	Fields      int     `json:"fields"`
	Fanout      int     `json:"fanout"`
	ReuseFrame  bool    `json:"reuse_frame"`
	ShareDecode bool    `json:"share_decode"`
	Rep         int     `json:"rep"`
	NS          float64 `json:"ns"`
	Bytes       int64   `json:"bytes"`
	Allocs      int64   `json:"allocs"`
	N           int     `json:"n"`
}
type item struct {
	row row
	fn  func(*testing.B)
}

func main() {
	testing.Init()
	duration := flag.String("duration", "100ms", "")
	count := flag.Int("count", 3, "")
	seed := flag.Int64("seed", 1, "")
	flag.Parse()
	flag.Set("test.benchtime", *duration)
	runtime.GOMAXPROCS(1)
	var cases []item
	for _, which := range []string{"c", "rust"} {
		which := which
		cases = append(cases, item{row{Backend: which, Kind: "ffi_noop"}, func(b *testing.B) {
			for b.Loop() {
				if which == "c" {
					cbridge.Noop()
				} else {
					rbridge.Noop()
				}
			}
		}})
	}
	for _, f := range lab.Fixtures() {
		f := f
		if f.Name != "small" && f.Name != "map256" && f.Name != "blob8k" {
			continue
		}
		for _, n := range []int{1, 4} {
			n := n
			mask := uint32(1<<n) - 1
			for _, which := range []string{"c", "rust", "go"} {
				which := which
				for _, first := range []bool{false, true} {
					first := first
					for _, batch := range []int{1, 32} {
						batch := batch
						raw := make([]byte, 0, len(f.Raw)*batch)
						offsets := make([]uintptr, batch+1)
						for i := 0; i < batch; i++ {
							offsets[i] = uintptr(len(raw))
							raw = append(raw, f.Raw...)
						}
						offsets[batch] = uintptr(len(raw))
						out := make([]uintptr, batch*8)
						name := which + "_last"
						if first {
							name = which + "_first"
						}
						cases = append(cases, item{row{Backend: name, Fixture: f.Name, Kind: "batch", Fields: n, Fanout: batch}, func(b *testing.B) {
							b.ReportAllocs()
							for b.Loop() {
								if which == "go" {
									for i := 0; i < batch; i++ {
										v, e := lab.GeneratedScan(raw[offsets[i]:offsets[i+1]], mask, first)
										if e != nil {
											b.Fatal(e)
										}
										sink = match(v, n)
									}
								} else {
									var e error
									if which == "c" {
										e = cbridge.Batch(raw, offsets, mask, first, out)
									} else {
										e = rbridge.Batch(raw, offsets, mask, first, out)
									}
									if e != nil {
										b.Fatal(e)
									}
									for i := 0; i < batch; i++ {
										var v [4]string
										for k := 0; k < n; k++ {
											o, l := out[i*8+k*2], out[i*8+k*2+1]
											v[k] = lab.StringView(raw[offsets[i]+o : offsets[i]+o+l])
										}
										sink = match(v, n)
									}
								}
							}
						}})
					}
				}
			}
			p, e := lab.Program(n, false)
			if e != nil {
				panic(e)
			}
			for _, backend := range lab.Backends {
				backend := backend
				if backend.Name != "go_generated_unmarshal" && backend.Name != "go_codegen_first" && backend.Name != "plan_last_alias" {
					continue
				}
				newSession, e := backend.Prepare(lab.Fields[:n])
				if e != nil {
					panic(e)
				}
				for _, fanout := range []int{1, 32} {
					fanout := fanout
					for _, reuse := range []bool{false, true} {
						reuse := reuse
						for _, share := range []bool{false, true} {
							share := share
							if fanout == 1 && share {
								continue
							}
							frames := make([]*lab.Frame, fanout)
							for i := range frames {
								frames[i] = lab.NewFrame(p, f.Event)
							}
							cases = append(cases, item{row{Backend: backend.Name, Fixture: f.Name, Kind: "fanout", Fields: n, Fanout: fanout, ReuseFrame: reuse, ShareDecode: share}, func(b *testing.B) {
								b.ReportAllocs()
								for b.Loop() {
									var shared lab.Session
									var e error
									if share {
										shared, e = newSession(f.Raw)
										if e != nil {
											b.Fatal(e)
										}
									}
									for i := 0; i < fanout; i++ {
										s := shared
										if !share {
											s, e = newSession(f.Raw)
											if e != nil {
												b.Fatal(e)
											}
										}
										var ok bool
										if reuse {
											ok, e = frames[i].Eval(s)
										} else {
											ok, e = lab.Eval(p, f.Event, s)
										}
										if !share {
											s.Close()
										}
										if e != nil {
											b.Fatal(e)
										}
										sink = ok
									}
									if share {
										shared.Close()
									}
								}
							}})
						}
					}
				}
			}
		}
	}
	enc := json.NewEncoder(os.Stdout)
	rng := rand.New(rand.NewSource(*seed))
	for rep := 0; rep < *count; rep++ {
		rng.Shuffle(len(cases), func(i, j int) { cases[i], cases[j] = cases[j], cases[i] })
		for i, c := range cases {
			r := testing.Benchmark(c.fn)
			v := c.row
			v.Rep = rep
			v.N = r.N
			v.NS = float64(r.T.Nanoseconds()) / float64(r.N)
			v.Bytes = r.AllocedBytesPerOp()
			v.Allocs = r.AllocsPerOp()
			enc.Encode(v)
			if i%24 == 0 {
				fmt.Fprintf(os.Stderr, "rep %d case %d/%d\n", rep+1, i+1, len(cases))
			}
		}
	}
}
func match(v [4]string, n int) bool {
	if v[0] != lab.Artifact {
		return false
	}
	return n == 1 || (v[1] == "stage" && strings.HasPrefix(v[2], "sha256:") && v[3] == "application/vnd.oci.image.manifest.v1+json")
}
