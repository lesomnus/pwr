package pwr

import (
	"errors"
	"math/rand"
	"sync"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

func fixtureSchema(t testing.TB) (*Schema, protoreflect.MessageDescriptor) {
	t.Helper()
	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
	str := descriptorpb.FieldDescriptorProto_TYPE_STRING
	f := &descriptorpb.FileDescriptorProto{Name: proto.String("record.proto"), Syntax: proto.String("proto3"), MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("Record"), OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("choice")}, {Name: proto.String("_opt")}}, NestedType: []*descriptorpb.DescriptorProto{{Name: proto.String("Child")}}}}}
	m := f.MessageType[0]
	for i, name := range []string{"one", "two", "three"} {
		m.Field = append(m.Field, &descriptorpb.FieldDescriptorProto{Name: proto.String(name), Number: proto.Int32(int32(i + 1)), Label: &optional, Type: &str})
	}
	m.Field = append(m.Field,
		&descriptorpb.FieldDescriptorProto{Name: proto.String("child"), Number: proto.Int32(4), Label: &optional, Type: descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum(), TypeName: proto.String(".Record.Child")},
		&descriptorpb.FieldDescriptorProto{Name: proto.String("list"), Number: proto.Int32(5), Label: descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum(), Type: &str},
		&descriptorpb.FieldDescriptorProto{Name: proto.String("choice_string"), Number: proto.Int32(6), Label: &optional, Type: &str, OneofIndex: proto.Int32(0)},
		&descriptorpb.FieldDescriptorProto{Name: proto.String("data"), Number: proto.Int32(7), Label: &optional, Type: descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum()},
		&descriptorpb.FieldDescriptorProto{Name: proto.String("opt"), Number: proto.Int32(8), Label: &optional, Type: &str, Proto3Optional: proto.Bool(true), OneofIndex: proto.Int32(1)},
	)
	file, err := protodesc.NewFile(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := file.Messages().Get(0)
	s, err := Compile(md)
	if err != nil {
		t.Fatal(err)
	}
	return s, md
}
func put(raw []byte, n protowire.Number, value string) []byte {
	return protowire.AppendString(protowire.AppendTag(raw, n, protowire.BytesType), value)
}

func TestCursorAndCache(t *testing.T) {
	s, _ := fixtureSchema(t)
	raw := put(put(nil, 1, "one"), 2, "two")
	middle := len(raw)
	raw = put(raw, 3, "three")
	r := s.NewReader(raw, AssumeUniqueScalars())
	if r.cursor != 0 || r.index != nil {
		t.Fatal("reader eagerly scanned")
	}
	for _, step := range []struct {
		number protoreflect.FieldNumber
		value  string
		cursor int
	}{{2, "two", middle}, {1, "one", middle}, {2, "two", middle}, {3, "three", len(raw)}, {8, "", len(raw)}, {8, "", len(raw)}} {
		got, err := r.String(step.number)
		if err != nil || got != step.value || r.cursor != step.cursor {
			t.Fatalf("field %d: %q, cursor %d, err %v", step.number, got, r.cursor, err)
		}
	}
	if !r.done {
		t.Fatal("EOF was not cached")
	}
	r = s.NewReader(raw)
	if _, err := r.String(2); err != nil || r.cursor != len(raw) || !r.done {
		t.Fatal("default did not finish scan", err)
	}
}

func TestDuplicateAndOrder(t *testing.T) {
	s, md := fixtureSchema(t)
	rng := rand.New(rand.NewSource(81297))
	for trial := 0; trial < 2000; trial++ {
		var raw []byte
		first := map[int]string{}
		seen := map[int]bool{}
		for j, n := 0, rng.Intn(30); j < n; j++ {
			num := rng.Intn(3) + 1
			v := []string{"", "a", "hello", "한글"}[rng.Intn(4)]
			raw = put(raw, protowire.Number(num), v)
			if !seen[num] {
				first[num] = v
				seen[num] = true
			}
			if j%3 == 0 {
				raw = protowire.AppendVarint(protowire.AppendTag(raw, 500, protowire.VarintType), 12345)
			}
		}
		m := dynamicpb.NewMessage(md)
		if err := proto.Unmarshal(raw, m); err != nil {
			t.Fatal(err)
		}
		for _, unique := range []bool{false, true} {
			r := s.NewReader(raw)
			if unique {
				r = s.NewReader(raw, AssumeUniqueScalars())
			}
			for k := 0; k < 3; k++ {
				for _, i := range rng.Perm(3) {
					num := i + 1
					want := m.Get(md.Fields().ByNumber(protoreflect.FieldNumber(num))).String()
					if unique {
						want = first[num]
					}
					got, err := r.String(protoreflect.FieldNumber(num))
					if err != nil || got != want {
						t.Fatalf("trial%d field%d unique%v got%q want%q err%v", trial, num, unique, got, want, err)
					}
				}
			}
		}
	}
}

func TestSelectiveErrors(t *testing.T) {
	s, _ := fixtureSchema(t)
	raw := append(put(nil, 1, "ok"), 0x1a, 0x80)
	r := s.NewReader(raw, AssumeUniqueScalars())
	if got, err := r.String(1); got != "ok" || err != nil {
		t.Fatal(got, err)
	}
	_, err := r.String(3)
	if !errors.Is(err, ErrInvalidWire) {
		t.Fatal(err)
	}
	_, again := r.String(1)
	if again != err {
		t.Fatal("wire error not sticky")
	}
	_, err = s.NewReader(raw).String(1)
	if !errors.Is(err, ErrInvalidWire) {
		t.Fatal(err)
	}
	r = s.NewReader(put(nil, 1, string([]byte{0xff})))
	_, err = r.String(1)
	if !errors.Is(err, ErrInvalidUTF8) {
		t.Fatal(err)
	}
	if _, again := r.String(1); again != err {
		t.Fatal("UTF-8 error not sticky")
	}
	got, err := s.NewReader(put(nil, 1, string([]byte{0xff})), AssumeValidUTF8()).String(1)
	if err != nil || got != string([]byte{0xff}) {
		t.Fatal(got, err)
	}
	// Valid outer length, malformed child body: skip it without child access.
	r = s.NewReader(put(put(nil, 4, string([]byte{0x80})), 1, "ok"))
	if got, err := r.String(1); err != nil || got != "ok" {
		t.Fatal(got, err)
	}
	for _, num := range []protoreflect.FieldNumber{4, 5, 7} {
		if _, err := r.String(num); !errors.Is(err, ErrUnsupportedField) {
			t.Fatal(num, err)
		}
	}
	if _, err := r.String(99); !errors.Is(err, ErrUnknownField) {
		t.Fatal(err)
	}
	if got, err := r.String(1); err != nil || got != "ok" {
		t.Fatal("unsupported query poisoned reader", got, err)
	}
	if _, err := Compile(nil); !errors.Is(err, ErrInvalidSchema) {
		t.Fatal(err)
	}
	if _, err := new(Schema).NewReader(nil).String(1); !errors.Is(err, ErrInvalidSchema) {
		t.Fatal(err)
	}
	if _, err := (*Schema)(nil).NewReader(nil).String(1); !errors.Is(err, ErrInvalidSchema) {
		t.Fatal(err)
	}
	if _, err := (*Reader)(nil).String(1); !errors.Is(err, ErrInvalidSchema) {
		t.Fatal(err)
	}
}

func TestIncompatibleWireType(t *testing.T) {
	s, md := fixtureSchema(t)
	raw := put(nil, 1, "ok")
	raw = protowire.AppendVarint(protowire.AppendTag(raw, 1, protowire.VarintType), 7)
	m := dynamicpb.NewMessage(md)
	if err := proto.Unmarshal(raw, m); err != nil {
		t.Fatal(err)
	}
	got, err := s.NewReader(raw).String(1)
	if err != nil || got != m.Get(md.Fields().ByNumber(1)).String() {
		t.Fatal(got, err)
	}
	raw = protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 7)
	raw = put(raw, 1, "later")
	got, err = s.NewReader(raw, AssumeUniqueScalars()).String(1)
	if err != nil || got != "later" {
		t.Fatal(got, err)
	}
}

func TestSparseAndDefault(t *testing.T) {
	f := &descriptorpb.FileDescriptorProto{Name: proto.String("sparse.proto"), Syntax: proto.String("proto2"), MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("Sparse"), Field: []*descriptorpb.FieldDescriptorProto{{Name: proto.String("text"), Number: proto.Int32(500000001), Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum(), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), DefaultValue: proto.String("fallback")}}}}}
	file, err := protodesc.NewFile(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Compile(file.Messages().Get(0))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.dense) != 0 {
		t.Fatal("large number allocated dense dispatch")
	}
	for _, raw := range [][]byte{nil, put(nil, 500000001, "")} {
		want := "fallback"
		if len(raw) > 0 {
			want = ""
		}
		got, err := s.NewReader(raw).String(500000001)
		if err != nil || got != want {
			t.Fatal(got, err)
		}
	}
	if _, err := s.NewReader(nil).String(-1); !errors.Is(err, ErrUnknownField) {
		t.Fatal(err)
	}
}

func TestSharedSchema(t *testing.T) {
	s, _ := fixtureSchema(t)
	raw := put(nil, 8, "optional")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				v, e := s.NewReader(raw).String(8)
				if e != nil || v != "optional" {
					t.Error(v, e)
					return
				}
			}
		}()
	}
	wg.Wait()
}

func FuzzReader(f *testing.F) {
	s, md := fixtureSchema(f)
	for _, raw := range [][]byte{nil, put(nil, 1, "a"), put(put(nil, 2, "old"), 2, ""), {0x80}, {0x0a, 0xff}} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 65536 {
			t.Skip()
		}
		m := dynamicpb.NewMessage(md)
		nativeErr := proto.Unmarshal(raw, m)
		r := s.NewReader(raw)
		fast := s.NewReader(raw, AssumeUniqueScalars())
		for _, num := range []protoreflect.FieldNumber{2, 1, 3, 2} {
			got, err := r.String(num)
			if nativeErr == nil {
				want := m.Get(md.Fields().ByNumber(num)).String()
				if err != nil || got != want {
					t.Fatalf("field%d got%q want%q err%v", num, got, want, err)
				}
			}
			_, _ = fast.String(num)
		}
	})
}
