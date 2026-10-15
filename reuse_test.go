package pwr

import (
	"errors"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"sync"
	"testing"
)

func TestResetAndPool(t *testing.T) {
	s := nestedSchema(t)
	old := put(nil, 3, string(put(nil, 1, "old")))
	r := s.NewReader(old, WithLimits(Limits{MaxFields: 20}))
	child, e := r.Message(3)
	if e != nil {
		t.Fatal(e)
	}
	index := &r.index[0]
	if e = r.Reset(put(nil, 1, "new")); e != nil {
		t.Fatal(e)
	}
	if &r.index[0] != index {
		t.Fatal("index allocation was not reused")
	}
	if v, e := r.String(1); e != nil || v != "new" {
		t.Fatal(v, e)
	}
	if v, e := child.String(1); e != nil || v != "old" {
		t.Fatal("old child lost its input", v, e)
	}
	if e = child.Reset(nil); !errors.Is(e, ErrPoolOwnership) {
		t.Fatal(e)
	}
	if e = r.Reset([]byte{0x80}); e != nil {
		t.Fatal(e)
	}
	if _, e = r.String(1); e == nil {
		t.Fatal("bad wire accepted")
	}
	if e = r.Reset(nil); e != nil {
		t.Fatal(e)
	}
	if v, e := r.String(1); e != nil || v != "" {
		t.Fatal(v, e)
	}
	p := s.NewPool(AssumeUniqueScalars(), WithLimits(Limits{MaxFields: 20}))
	r = p.Get(old)
	child, e = r.Message(3)
	if e != nil {
		t.Fatal(e)
	}
	if e = p.Put(r); e != nil {
		t.Fatal(e)
	}
	if e = p.Put(r); !errors.Is(e, ErrPoolOwnership) {
		t.Fatal(e)
	}
	if _, e = r.String(1); !errors.Is(e, ErrPoolOwnership) {
		t.Fatal(e)
	}
	if r.raw != nil || r.parts != nil || len(r.cache) != 0 {
		t.Fatal("pool retained borrowed input")
	}
	if v, e := child.String(1); e != nil || v != "old" {
		t.Fatal(v, e)
	}
	if e = p.Put(s.NewReader(nil)); !errors.Is(e, ErrPoolOwnership) {
		t.Fatal(e)
	}
	r = p.Get(nil)
	if _, e = r.String(1); e != nil {
		t.Fatal(e)
	}
	if e = p.Put(r); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r := p.Get(put(nil, 1, "test"))
				v, e := r.String(1)
				if e != nil || v != "test" {
					t.Error(v, e)
				}
				if e = p.Put(r); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	wg.Wait()
}
func TestMaterializeAndValidate(t *testing.T) {
	s := collectionSchema(t)
	raw := put(put(nil, 1, string([]byte{1, 2, 3})), 4, string(put(nil, 1, "child")))
	raw = protowire.AppendVarint(protowire.AppendTag(raw, 500, protowire.VarintType), 123)
	r := s.NewReader(raw)
	got, e := r.Materialize()
	if e != nil {
		t.Fatal(e)
	}
	want := dynamicpb.NewMessage(s.Descriptor())
	if e = proto.Unmarshal(raw, want); e != nil {
		t.Fatal(e)
	}
	if !proto.Equal(got, want) {
		t.Fatal("full decode differs, including unknown fields")
	}
	// Native materialization owns independent bytes, unlike borrowed field views.
	clear(raw)
	if !proto.Equal(got, want) {
		t.Fatal("native result aliases wire input")
	}
	ns := nestedSchema(t)
	r = ns.NewReader(put(put(nil, 1, string([]byte{0xff})), 1, "overwritten"), AssumeUniqueScalars(), AssumeValidUTF8())
	if e = r.Validate(); e == nil {
		t.Fatal("strict validation trusted invalid overwritten UTF-8")
	}
	r = ns.NewReader(put(put(nil, 3, string([]byte{0x80})), 1, "ok"))
	if v, e := r.String(1); e != nil || v != "ok" {
		t.Fatal(v, e)
	}
	if e = r.Validate(); e == nil {
		t.Fatal("strict validation skipped malformed child")
	}
	raw = put(nil, 3, string(varint(varint(nil, 2, 1), 2, 2)))
	if e = ns.NewReader(raw, WithLimits(Limits{MaxFields: 2})).Validate(); !errors.Is(e, ErrLimitExceeded) {
		t.Fatal(e)
	}
	// Required fields may be split across duplicate embedded occurrences.
	child := &descriptorpb.DescriptorProto{Name: proto.String("Child"), Field: []*descriptorpb.FieldDescriptorProto{fieldProto("a", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32), fieldProto("b", 2, descriptorpb.FieldDescriptorProto_TYPE_INT32)}}
	for _, f := range child.Field {
		f.Label = descriptorpb.FieldDescriptorProto_LABEL_REQUIRED.Enum()
	}
	f := fieldProto("child", 1, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE)
	f.TypeName = proto.String(".Required.Child")
	schema := compileFile(t, &descriptorpb.FileDescriptorProto{Name: proto.String("required.proto"), Syntax: proto.String("proto2"), MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("Required"), Field: []*descriptorpb.FieldDescriptorProto{f}, NestedType: []*descriptorpb.DescriptorProto{child}}}})
	a := put(nil, 1, string(varint(nil, 1, 1)))
	b := put(nil, 1, string(varint(nil, 2, 2)))
	if e = schema.NewReader(a).Validate(); e == nil {
		t.Fatal("missing required field")
	}
	if e = schema.NewReader(append(a, b...)).Validate(); e != nil {
		t.Fatal(e)
	}
}

func TestPlanAndView(t *testing.T) {
	s, _ := fixtureSchema(t)
	numbers := []protoreflect.FieldNumber{2, 1}
	plan, e := s.Plan(numbers...)
	if e != nil {
		t.Fatal(e)
	}
	numbers[0] = 99
	prefix := put(put(nil, 1, "one"), 2, "two")
	raw := append(append([]byte{}, prefix...), put(nil, 3, "three")...)
	r, e := plan.Read(raw, AssumeUniqueScalars())
	if e != nil {
		t.Fatal(e)
	}
	if r.cursor != len(prefix) {
		t.Fatal(r.cursor)
	}
	if v, e := r.String(1); e != nil || v != "one" {
		t.Fatal(v, e)
	}
	if e = r.Prefetch(99); !errors.Is(e, ErrUnknownField) {
		t.Fatal(e)
	}
	if r.Err() != nil {
		t.Fatal(r.Err())
	}
	if _, e = s.Plan(99); !errors.Is(e, ErrUnknownField) {
		t.Fatal(e)
	}
	r = s.NewReader(nil, WithLimits(Limits{MaxDepth: 1}))
	v, e := r.View(s, put(nil, 1, "view"))
	if e != nil {
		t.Fatal(e)
	}
	if name, e := v.String(1); e != nil || name != "view" {
		t.Fatal(name, e)
	}
	if _, e = v.View(s, nil); !errors.Is(e, ErrLimitExceeded) {
		t.Fatal(e)
	}
}
