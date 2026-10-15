package pwr

import (
	"errors"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"testing"
)

func collectionSchema(t testing.TB) *Schema {
	t.Helper()
	node := protodesc.ToDescriptorProto(nestedSchema(t).Descriptor())
	root := &descriptorpb.DescriptorProto{Name: proto.String("Collection")}
	for i, k := range []descriptorpb.FieldDescriptorProto_Type{descriptorpb.FieldDescriptorProto_TYPE_INT32, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE} {
		f := fieldProto("f"+string(rune('a'+i)), int32(i+1), k)
		f.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
		if i == 3 {
			f.TypeName = proto.String(".Node")
		}
		root.Field = append(root.Field, f)
	}
	for i, pair := range [][2]descriptorpb.FieldDescriptorProto_Type{{descriptorpb.FieldDescriptorProto_TYPE_STRING, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE}, {descriptorpb.FieldDescriptorProto_TYPE_SINT32, descriptorpb.FieldDescriptorProto_TYPE_BYTES}, {descriptorpb.FieldDescriptorProto_TYPE_BOOL, descriptorpb.FieldDescriptorProto_TYPE_INT64}} {
		entry := &descriptorpb.DescriptorProto{Name: proto.String("Entry" + string(rune('A'+i))), Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)}, Field: []*descriptorpb.FieldDescriptorProto{fieldProto("key", 1, pair[0]), fieldProto("value", 2, pair[1])}}
		if pair[1] == descriptorpb.FieldDescriptorProto_TYPE_MESSAGE {
			entry.Field[1].TypeName = proto.String(".Node")
		}
		// Map entry type names must match the generated CamelCase field name.
		name := []string{"entry_a", "entry_b", "entry_c"}[i]
		entry.Name = proto.String([]string{"EntryAEntry", "EntryBEntry", "EntryCEntry"}[i])
		root.NestedType = append(root.NestedType, entry)
		f := fieldProto(name, int32(5+i), descriptorpb.FieldDescriptorProto_TYPE_MESSAGE)
		f.TypeName = proto.String(".Collection." + entry.GetName())
		f.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
		root.Field = append(root.Field, f)
	}
	return compileFile(t, &descriptorpb.FileDescriptorProto{Name: proto.String("collection.proto"), Syntax: proto.String("proto3"), MessageType: []*descriptorpb.DescriptorProto{root, node}})
}
func compareElement(t testing.TB, v Value, want protoreflect.Value, fd protoreflect.FieldDescriptor) {
	t.Helper()
	if fd.Message() != nil {
		if v.Message() == nil {
			t.Fatal("missing message value")
		}
		compareReader(t, v.Message(), want.Message())
		return
	}
	if !v.Scalar().Equal(want) {
		t.Fatalf("%s got %v want %v", fd.FullName(), v.Scalar(), want)
	}
}
func compareReader(t testing.TB, r *Reader, m protoreflect.Message) {
	t.Helper()
	for i := 0; i < m.Descriptor().Fields().Len(); i++ {
		fd := m.Descriptor().Fields().Get(i)
		has, e := r.Has(fd.Number())
		if e != nil || has != m.Has(fd) {
			t.Fatalf("%s has=%v expected=%v err=%v", fd.FullName(), has, m.Has(fd), e)
		}
		v, e := r.Get(fd.Number())
		if e != nil {
			t.Fatal(e)
		}
		want := m.Get(fd)
		switch {
		case fd.IsMap():
			if v.Map().Len() != want.Map().Len() {
				t.Fatal("map length")
			}
			want.Map().Range(func(k protoreflect.MapKey, w protoreflect.Value) bool {
				got, ok := v.Map().Get(k)
				if !ok {
					t.Fatal("missing map key")
				}
				compareElement(t, got, w, fd.MapValue())
				return true
			})
		case fd.IsList():
			if v.List().Len() != want.List().Len() {
				t.Fatal("list length")
			}
			for j := 0; j < v.List().Len(); j++ {
				got, e := v.List().Get(j)
				if e != nil {
					t.Fatal(e)
				}
				compareElement(t, got, want.List().Get(j), fd)
			}
		case fd.Message() != nil:
			if has {
				compareElement(t, v, want, fd)
			} else if v.Message() != nil {
				t.Fatal("absent message")
			}
		default:
			compareElement(t, v, want, fd)
		}
	}
}
func TestCollectionsMatchNative(t *testing.T) {
	s := collectionSchema(t)
	packed := protowire.AppendVarint(nil, 1)
	packed = protowire.AppendVarint(packed, ^uint64(0))
	raw := put(nil, 1, string(packed))
	raw = varint(raw, 1, 123)
	raw = put(raw, 1, "")
	raw = protowire.AppendFixed64(protowire.AppendTag(raw, 2, protowire.Fixed64Type), 0x7ff8000000000000)
	raw = put(raw, 2, string(protowire.AppendFixed64(nil, 0x8000000000000000)))
	raw = put(put(raw, 3, ""), 3, "한글")
	raw = put(put(raw, 4, string(put(nil, 1, "a"))), 4, string(varint(nil, 2, 9)))
	raw = put(raw, 5, string(put(put(nil, 1, "key"), 2, string(put(nil, 1, "discard")))))
	entry := put(put(nil, 1, "wrong"), 1, "key")
	entry = put(entry, 2, string(varint(nil, 2, 42)))
	entry = put(entry, 2, string(put(nil, 1, "merged")))
	raw = put(raw, 5, string(entry))
	raw = put(raw, 5, "")
	raw = put(raw, 6, string(put(varint(nil, 1, protowire.EncodeZigZag(-2)), 2, string([]byte{0xff, 0}))))
	raw = put(raw, 7, string(varint(varint(nil, 1, 3), 2, 99)))
	for _, opts := range [][]ReaderOption{nil, {AssumeUniqueScalars()}} {
		m := dynamicpb.NewMessage(s.Descriptor())
		if e := proto.Unmarshal(raw, m); e != nil {
			t.Fatal(e)
		}
		r := s.NewReader(raw, opts...)
		compareReader(t, r, m)
		a, e := r.List(1)
		if e != nil {
			t.Fatal(e)
		}
		b, _ := r.List(1)
		if a != b {
			t.Fatal("collection not cached")
		}
		if _, e = a.Get(-1); !errors.Is(e, ErrIndex) {
			t.Fatal(e)
		}
		if _, e = a.Get(a.Len()); !errors.Is(e, ErrIndex) {
			t.Fatal(e)
		}
		count := 0
		a.Range(func(i int, v Value) bool { count++; return false })
		if count != 1 {
			t.Fatal(count)
		}
	}
}
func TestPackedErrorsAndBudget(t *testing.T) {
	s := collectionSchema(t)
	for _, raw := range [][]byte{put(nil, 1, string([]byte{0x80})), put(nil, 2, string([]byte{0, 1, 2})), put(nil, 3, string([]byte{0xff}))} {
		n := protoreflect.FieldNumber(1)
		if raw[0] == 0x12 {
			n = 2
		}
		if raw[0] == 0x1a {
			n = 3
		}
		r := s.NewReader(raw)
		_, e := r.List(n)
		if e == nil {
			t.Fatal("invalid list accepted")
		}
		if _, again := r.List(n); again != e {
			t.Fatal("error not sticky")
		}
	}
	r := s.NewReader(put(nil, 1, string(make([]byte, 10))), WithLimits(Limits{MaxFields: 5}))
	if _, e := r.List(1); !errors.Is(e, ErrLimitExceeded) {
		t.Fatal(e)
	}
	if b, e := s.NewReader(put(nil, 1, "")).Has(1); e != nil || b {
		t.Fatal(b, e)
	}
}
func FuzzCollections(f *testing.F) {
	s := collectionSchema(f)
	for _, b := range [][]byte{nil, put(nil, 1, string([]byte{1, 2, 3})), put(nil, 5, ""), put(nil, 4, string(put(nil, 1, "node"))), {0x80}} {
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 4096 {
			t.Skip()
		}
		m := dynamicpb.NewMessage(s.Descriptor())
		e := (proto.UnmarshalOptions{RecursionLimit: 16}).Unmarshal(b, m)
		r := s.NewReader(b, WithLimits(Limits{MaxDepth: 16, MaxFields: 100000}))
		if e == nil {
			compareReader(t, r, m)
		} else {
			for i := 1; i <= 7; i++ {
				_, _ = r.Get(protoreflect.FieldNumber(i))
			}
		}
	})
}
