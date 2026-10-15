//go:build incremental

package lab

import (
	"math/rand"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func testSchema(t *testing.T) protoreflect.MessageDescriptor {
	t.Helper()
	f := &descriptorpb.FileDescriptorProto{Name: proto.String("scan.proto"), Syntax: proto.String("proto3"), MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("Record")}}}
	for i, name := range []string{"one", "two", "three", "missing"} {
		f.MessageType[0].Field = append(f.MessageType[0].Field, &descriptorpb.FieldDescriptorProto{Name: proto.String(name), Number: proto.Int32(int32(i + 1)), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()})
	}
	fd, err := protodesc.NewFile(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	return fd.Messages().Get(0)
}
func addString(raw []byte, num protowire.Number, v string) []byte {
	return protowire.AppendString(protowire.AppendTag(raw, num, protowire.BytesType), v)
}
func TestIncrementalCursorAndCache(t *testing.T) {
	md := testSchema(t)
	p, err := newIndexPlan(md)
	if err != nil {
		t.Fatal(err)
	}
	raw := addString(nil, 1, "one")
	raw = addString(raw, 2, "two")
	afterTwo := len(raw)
	raw = addString(raw, 3, "three")
	x, err := p.session(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	s := x.(*incrementalSession)
	if s.cursor != 0 || s.index != nil {
		t.Fatal("construction scanned or allocated index")
	}
	for _, step := range []struct {
		num    int
		want   string
		cursor int
	}{{2, "two", afterTwo}, {1, "one", afterTwo}, {2, "two", afterTwo}, {3, "three", len(raw)}, {4, "", len(raw)}, {4, "", len(raw)}} {
		got, err := s.Get(md.Fields().Get(step.num - 1))
		if err != nil || got != step.want || s.cursor != step.cursor {
			t.Fatalf("field%d: got%q cursor%d err%v", step.num, got, s.cursor, err)
		}
	}
	if !s.done {
		t.Fatal("missing field did not record EOF")
	}
	// The last-wins variant must finish the wire stream before its first result.
	x, _ = p.session(raw, false)
	s = x.(*incrementalSession)
	if _, err := s.Get(md.Fields().Get(1)); err != nil || s.cursor != len(raw) || !s.done {
		t.Fatal("last-wins failed to finish scan", err)
	}
}
func TestIncrementalDifferential(t *testing.T) {
	md := testSchema(t)
	p, _ := newIndexPlan(md)
	rng := rand.New(rand.NewSource(81297))
	for i := 0; i < 2000; i++ {
		var raw []byte
		first := map[int]string{}
		seen := map[int]bool{}
		for j, n := 0, rng.Intn(40); j < n; j++ {
			field := rng.Intn(3) + 1
			v := []string{"", "a", "hello", "한글"}[rng.Intn(4)]
			raw = addString(raw, protowire.Number(field), v)
			if !seen[field] {
				first[field] = v
				seen[field] = true
			}
			if j%3 == 0 {
				raw = protowire.AppendVarint(protowire.AppendTag(raw, 500, protowire.VarintType), uint64(rng.Intn(65536)))
			}
		}
		m := dynamicpb.NewMessage(md)
		if err := proto.Unmarshal(raw, m); err != nil {
			t.Fatal(err)
		}
		for _, firstMode := range []bool{false, true} {
			x, _ := p.session(raw, firstMode)
			for k := 0; k < 3; k++ {
				for _, idx := range rng.Perm(4) {
					fd := md.Fields().Get(idx)
					want := m.Get(fd).String()
					if firstMode {
						want = first[idx+1]
					}
					got, err := x.Get(fd)
					if err != nil || got != want {
						t.Fatalf("case%d first%v field%d got%q want%q err%v", i, firstMode, idx+1, got, want, err)
					}
				}
			}
		}
	}
}
func TestIncrementalMalformedBoundary(t *testing.T) {
	md := testSchema(t)
	p, _ := newIndexPlan(md)
	raw := append(addString(nil, 1, "ok"), 0x1a, 0x80)
	x, _ := p.session(raw, true)
	s := x.(*incrementalSession)
	if got, err := s.Get(md.Fields().Get(0)); err != nil || got != "ok" {
		t.Fatal(got, err)
	}
	if _, err := s.Get(md.Fields().Get(2)); err == nil {
		t.Fatal("truncated trailing field accepted")
	}
	if _, err := s.Get(md.Fields().Get(0)); err == nil {
		t.Fatal("error must remain sticky after discovery")
	}
	x, _ = p.session(raw, false)
	if _, err := x.Get(md.Fields().Get(0)); err == nil {
		t.Fatal("last-wins accepted trailing truncation")
	}
	for i := 0; i < len(raw); i++ {
		for _, first := range []bool{false, true} {
			x, _ = p.session(raw[:i], first)
			_, _ = x.Get(md.Fields().Get(2))
		}
	}
}

type requestTracker struct {
	Session
	numbers []protoreflect.FieldNumber
}

func (s *requestTracker) Get(fd protoreflect.FieldDescriptor) (string, error) {
	s.numbers = append(s.numbers, fd.Number())
	return s.Session.Get(fd)
}

func TestIncrementalCELShortCircuit(t *testing.T) {
	plan, err := newIndexPlan(Descriptor)
	if err != nil {
		t.Fatal(err)
	}
	var fixture Fixture
	for _, f := range ScanFixtures() {
		if f.Name == "map256_late" {
			fixture = f
		}
	}
	for _, c := range ScanScenarios() {
		if c.Name != "short_false" && c.Name != "metadata_false" {
			continue
		}
		program, err := Compile(c.Query, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, first := range []bool{false, true} {
			x, _ := plan.session(fixture.Raw, first)
			s := x.(*incrementalSession)
			tracker := &requestTracker{Session: s}
			got, err := Eval(program, fixture.Event, tracker)
			if err != nil || got {
				t.Fatal(got, err)
			}
			if c.Name == "metadata_false" {
				if len(tracker.numbers) != 0 || s.cursor != 0 || s.index != nil {
					t.Fatal("metadata rejection accessed payload")
				}
			} else {
				if len(tracker.numbers) != 1 || tracker.numbers[0] != 8 {
					t.Fatal("CEL did not short circuit", tracker.numbers)
				}
				if first && s.cursor >= len(fixture.Raw) {
					t.Fatal("first mode scanned unneeded tail")
				}
				if !first && s.cursor != len(fixture.Raw) {
					t.Fatal("last mode did not inspect duplicate possibilities")
				}
			}
		}
	}
}
