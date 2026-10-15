//go:build all || (cffi && rustffi)

package lab

import (
	"example.com/cel-wire-lab/internal/cbridge"
	"example.com/cel-wire-lab/internal/rbridge"
	"google.golang.org/protobuf/encoding/protowire"
	"math/rand"
	"testing"
)

func TestFFIBounds(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 5000; i++ {
		raw := make([]byte, rng.Intn(80))
		rng.Read(raw)
		for _, first := range []bool{false, true} {
			cbridge.Single(raw, 11, first)
			rbridge.Single(raw, 11, first)
			cbridge.Project(raw, 15, first)
			rbridge.Project(raw, 15, first)
		}
	}
}
func TestFFIValidDifferential(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for i := 0; i < 1000; i++ {
		var raw []byte
		var wantFirst, wantLast [4]string
		var seen [4]bool
		for j := 0; j < rng.Intn(50); j++ {
			idx := rng.Intn(4)
			v := string([]byte{byte('a' + rng.Intn(26)), byte('a' + rng.Intn(26))})
			raw = protowire.AppendTag(raw, protowire.Number(Numbers[idx]), protowire.BytesType)
			raw = protowire.AppendString(raw, v)
			if !seen[idx] {
				wantFirst[idx] = v
				seen[idx] = true
			}
			wantLast[idx] = v
		}
		for _, first := range []bool{false, true} {
			want := wantLast
			if first {
				want = wantFirst
			}
			for _, mask := range []uint32{1, 15} {
				c, e := cbridge.Project(raw, mask, first)
				if e != nil {
					t.Fatal(e)
				}
				r, e := rbridge.Project(raw, mask, first)
				if e != nil {
					t.Fatal(e)
				}
				for idx := 0; idx < 4; idx++ {
					if mask&(1<<idx) == 0 {
						continue
					}
					for _, p := range [][2]int{c[idx], r[idx]} {
						got := string(raw[p[0] : p[0]+p[1]])
						if got != want[idx] {
							t.Fatalf("first=%v idx=%d got=%q want=%q", first, idx, got, want[idx])
						}
					}
				}
			}
		}
	}
}
