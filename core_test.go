package pwr

import (
	"errors"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"math"
	"testing"
)

func fieldProto(name string, num int32, k descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{Name: proto.String(name), Number: proto.Int32(num), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(), Type: k.Enum()}
}
func compileFile(t testing.TB, f *descriptorpb.FileDescriptorProto) *Schema {
	t.Helper()
	fd, err := protodesc.NewFile(f, nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := Compile(fd.Messages().Get(0))
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func nestedSchema(t testing.TB) *Schema {
	t.Helper()
	m := &descriptorpb.DescriptorProto{Name: proto.String("Node"), OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("choice")}}}
	m.Field = []*descriptorpb.FieldDescriptorProto{fieldProto("name", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING), fieldProto("count", 2, descriptorpb.FieldDescriptorProto_TYPE_INT64), fieldProto("child", 3, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE), fieldProto("choice_child", 4, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE), fieldProto("choice_text", 5, descriptorpb.FieldDescriptorProto_TYPE_STRING)}
	for _, i := range []int{2, 3} {
		m.Field[i].TypeName = proto.String(".Node")
	}
	for _, i := range []int{3, 4} {
		m.Field[i].OneofIndex = proto.Int32(0)
	}
	return compileFile(t, &descriptorpb.FileDescriptorProto{Name: proto.String("node.proto"), Syntax: proto.String("proto3"), MessageType: []*descriptorpb.DescriptorProto{m}})
}
func varint(b []byte, n protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(b, n, protowire.VarintType), v)
}
func TestAllScalars(t *testing.T) {
	kinds := []descriptorpb.FieldDescriptorProto_Type{descriptorpb.FieldDescriptorProto_TYPE_BOOL, descriptorpb.FieldDescriptorProto_TYPE_ENUM, descriptorpb.FieldDescriptorProto_TYPE_INT32, descriptorpb.FieldDescriptorProto_TYPE_SINT32, descriptorpb.FieldDescriptorProto_TYPE_SFIXED32, descriptorpb.FieldDescriptorProto_TYPE_INT64, descriptorpb.FieldDescriptorProto_TYPE_SINT64, descriptorpb.FieldDescriptorProto_TYPE_SFIXED64, descriptorpb.FieldDescriptorProto_TYPE_UINT32, descriptorpb.FieldDescriptorProto_TYPE_FIXED32, descriptorpb.FieldDescriptorProto_TYPE_UINT64, descriptorpb.FieldDescriptorProto_TYPE_FIXED64, descriptorpb.FieldDescriptorProto_TYPE_FLOAT, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, descriptorpb.FieldDescriptorProto_TYPE_BYTES, descriptorpb.FieldDescriptorProto_TYPE_STRING}
	m := &descriptorpb.DescriptorProto{Name: proto.String("Scalars"), EnumType: []*descriptorpb.EnumDescriptorProto{{Name: proto.String("E"), Value: []*descriptorpb.EnumValueDescriptorProto{{Name: proto.String("ZERO"), Number: proto.Int32(0)}}}}}
	for i, k := range kinds {
		fd := fieldProto("f"+string(rune('a'+i)), int32(i+1), k)
		if k == descriptorpb.FieldDescriptorProto_TYPE_ENUM {
			fd.TypeName = proto.String(".Scalars.E")
		}
		m.Field = append(m.Field, fd)
	}
	s := compileFile(t, &descriptorpb.FileDescriptorProto{Name: proto.String("scalars.proto"), Syntax: proto.String("proto3"), MessageType: []*descriptorpb.DescriptorProto{m}})
	for _, u := range []uint64{0, 1, 127, 128, math.MaxUint32, math.MaxUint64, 0x8000000000000000} {
		var raw []byte
		for i, f := range s.fields {
			n := protowire.Number(i + 1)
			raw = protowire.AppendTag(raw, n, f.wire)
			switch f.wire {
			case protowire.VarintType:
				raw = protowire.AppendVarint(raw, u)
			case protowire.Fixed32Type:
				raw = protowire.AppendFixed32(raw, uint32(u))
			case protowire.Fixed64Type:
				raw = protowire.AppendFixed64(raw, u)
			case protowire.BytesType:
				raw = protowire.AppendString(raw, "한글")
			}
		}
		native := dynamicpb.NewMessage(s.Descriptor())
		if err := proto.Unmarshal(raw, native); err != nil {
			t.Fatal(err)
		}
		for _, opts := range [][]ReaderOption{nil, {AssumeUniqueScalars()}} {
			r := s.NewReader(raw, opts...)
			for i, f := range s.fields {
				n := protoreflect.FieldNumber(i + 1)
				v, err := r.scalar(n)
				if err != nil {
					t.Fatal(err)
				}
				want := native.Get(f.descriptor)
				if !v.Equal(want) {
					t.Fatalf("%s got %v want %v", f.descriptor.Name(), v, want)
				}
				has, err := r.Has(n)
				if err != nil || has != native.Has(f.descriptor) {
					t.Fatalf("presence %d: %v %v", n, has, err)
				}
			}
			if _, err := r.Int32(1); !errors.Is(err, ErrUnsupportedField) {
				t.Fatal(err)
			}
		}
	}
}
func TestNestedMergeAndOneof(t *testing.T) {
	s := nestedSchema(t)
	if s.fields[2].child != s {
		t.Fatal("recursive graph was not memoized")
	}
	a := put(nil, 1, "before")
	b := varint(nil, 2, 7)
	raw := put(put(nil, 3, string(a)), 3, string(b))
	raw = put(raw, 4, string(a))
	raw = put(raw, 5, "switch")
	raw = put(raw, 4, string(b))
	raw = put(raw, 4, string(put(nil, 1, "after")))
	for _, opts := range [][]ReaderOption{nil, {AssumeUniqueScalars()}} {
		r := s.NewReader(raw, opts...)
		c, err := r.Message(3)
		if err != nil {
			t.Fatal(err)
		}
		if v, e := c.String(1); e != nil || v != "before" {
			t.Fatal(v, e)
		}
		if v, e := c.Int64(2); e != nil || v != 7 {
			t.Fatal(v, e)
		}
		c, err = r.Message(4)
		if err != nil {
			t.Fatal(err)
		}
		if v, e := c.String(1); e != nil || v != "after" {
			t.Fatal(v, e)
		}
		if v, e := c.Int64(2); e != nil || v != 7 {
			t.Fatal(v, e)
		}
		if v, e := r.String(5); e != nil || v != "" {
			t.Fatal(v, e)
		}
		if n, e := r.WhichOneof("choice"); e != nil || n != 4 {
			t.Fatal(n, e)
		}
		if ok, e := r.Has(4); e != nil || !ok {
			t.Fatal(ok, e)
		}
		if ok, e := r.Has(5); e != nil || ok {
			t.Fatal(ok, e)
		}
	}
	// Replacing a oneof member must forget the earlier child, even if selected again.
	r := s.NewReader(put(put(put(nil, 4, string(a)), 5, "switch"), 4, string(b)))
	c, e := r.Message(4)
	if e != nil {
		t.Fatal(e)
	}
	if v, e := c.String(1); e != nil || v != "" {
		t.Fatal(v, e)
	}
	// Unique scalars do not suppress merging across distinct embedded occurrences.
	r = s.NewReader(put(put(nil, 3, string(a)), 3, string(put(nil, 1, "last"))), AssumeUniqueScalars())
	c, e = r.Message(3)
	if e != nil {
		t.Fatal(e)
	}
	if v, e := c.String(1); e != nil || v != "last" {
		t.Fatal(v, e)
	}
}
func TestPresenceAndLimits(t *testing.T) {
	s, _ := fixtureSchema(t)
	r := s.NewReader(put(put(nil, 1, ""), 8, ""))
	if b, e := r.Has(1); e != nil || b {
		t.Fatal(b, e)
	}
	if b, e := r.Has(8); e != nil || !b {
		t.Fatal(b, e)
	}
	raw := put(put(nil, 1, "one"), 2, "two")
	for _, l := range []Limits{{MaxFields: 1}, {MaxBytes: int64(len(raw) - 1)}} {
		r = s.NewReader(raw, WithLimits(l))
		_, e := r.String(1)
		if !errors.Is(e, ErrLimitExceeded) {
			t.Fatal(e)
		}
	}
	r = s.NewReader(raw, WithLimits(Limits{MaxFields: -1}))
	if _, e := r.String(1); !errors.Is(e, ErrInvalidOption) {
		t.Fatal(e)
	}
	ns := nestedSchema(t)
	r = ns.NewReader(put(nil, 3, string(put(nil, 3, ""))), WithLimits(Limits{MaxDepth: 1}))
	c, e := r.Message(3)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Message(3); !errors.Is(e, ErrLimitExceeded) {
		t.Fatal(e)
	}
}
func TestGroups(t *testing.T) {
	child := &descriptorpb.DescriptorProto{Name: proto.String("Child"), Field: []*descriptorpb.FieldDescriptorProto{fieldProto("name", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING)}}
	f := fieldProto("child", 1, descriptorpb.FieldDescriptorProto_TYPE_GROUP)
	f.TypeName = proto.String(".Root.Child")
	s := compileFile(t, &descriptorpb.FileDescriptorProto{Name: proto.String("group.proto"), Syntax: proto.String("proto2"), MessageType: []*descriptorpb.DescriptorProto{{Name: proto.String("Root"), Field: []*descriptorpb.FieldDescriptorProto{f}, NestedType: []*descriptorpb.DescriptorProto{child}}}})
	raw := protowire.AppendTag(nil, 1, protowire.StartGroupType)
	raw = protowire.AppendGroup(raw, 1, put(nil, 1, "ok"))
	c, e := s.NewReader(raw).Message(1)
	if e != nil {
		t.Fatal(e)
	}
	if v, e := c.String(1); e != nil || v != "ok" {
		t.Fatal(v, e)
	}
	for _, b := range [][]byte{raw[:len(raw)-1], append(append([]byte{}, raw[:len(raw)-1]...), byte(protowire.EncodeTag(2, protowire.EndGroupType)))} {
		if _, e := s.NewReader(b).Message(1); !errors.Is(e, ErrInvalidWire) {
			t.Fatal(e)
		}
	}
}

func compareSingular(t testing.TB, r *Reader, m protoreflect.Message) {
	t.Helper()
	for i := 0; i < m.Descriptor().Fields().Len(); i++ {
		fd := m.Descriptor().Fields().Get(i)
		if fd.IsList() || fd.IsMap() {
			continue
		}
		has, e := r.Has(fd.Number())
		if e != nil || has != m.Has(fd) {
			t.Fatalf("%s presence=%v expected=%v err=%v", fd.FullName(), has, m.Has(fd), e)
		}
		if fd.Message() != nil {
			child, e := r.Message(fd.Number())
			if e != nil {
				t.Fatal(e)
			}
			if has {
				compareSingular(t, child, m.Get(fd).Message())
			} else if child != nil {
				t.Fatal("absent child is non-nil")
			}
		} else {
			v, e := r.scalar(fd.Number())
			if e != nil || !v.Equal(m.Get(fd)) {
				t.Fatalf("%s value=%v expected=%v err=%v", fd.FullName(), v, m.Get(fd), e)
			}
		}
	}
}
func TestMergedMessagesMatchNative(t *testing.T) {
	s := nestedSchema(t)
	for trial := 0; trial < 500; trial++ {
		var raw []byte
		for i := 0; i < 20; i++ {
			// Deterministic varied interleaving, reset-to-default, nested merges and oneof changes.
			n := protowire.Number((trial*17+i*13+i*i)%5 + 1)
			switch n {
			case 1, 5:
				raw = put(raw, n, []string{"", "value", "한글"}[(trial+i)%3])
			case 2:
				raw = varint(raw, n, uint64(trial+i))
			case 3, 4:
				var child []byte
				if i%2 == 0 {
					child = put(child, 1, []string{"", "child"}[trial%2])
				} else {
					child = varint(child, 2, uint64(i))
				}
				if i%3 == 0 {
					child = put(child, 3, string(put(nil, 1, "grandchild")))
				}
				raw = put(raw, n, string(child))
			}
		}
		m := dynamicpb.NewMessage(s.Descriptor())
		if e := proto.Unmarshal(raw, m); e != nil {
			t.Fatal(e)
		}
		compareSingular(t, s.NewReader(raw), m)
	}
}
