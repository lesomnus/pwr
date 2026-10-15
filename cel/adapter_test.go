package pwrcel

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"github.com/lesomnus/pwr"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/typepb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func encoded(t testing.TB, m proto.Message) []byte {
	t.Helper()
	b, e := proto.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func schema(t testing.TB, md protoreflect.MessageDescriptor) *pwr.Schema {
	t.Helper()
	s, e := pwr.Compile(md)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func adapter(t testing.TB, md ...protoreflect.MessageDescriptor) *Adapter {
	t.Helper()
	a, e := New(md...)
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func program(t testing.TB, a *Adapter, expr string, options ...cel.EnvOption) cel.Program {
	t.Helper()
	opts := append(a.EnvOptions(), options...)
	env, e := cel.NewEnv(opts...)
	if e != nil {
		t.Fatal(e)
	}
	ast, issues := env.Compile(expr)
	if issues.Err() != nil {
		t.Fatal(issues.Err())
	}
	p, e := env.Program(ast)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func truth(t testing.TB, p cel.Program, vars map[string]any) {
	t.Helper()
	v, _, e := p.Eval(vars)
	if e != nil || v != types.True {
		t.Fatalf("got %v, err %v", v, e)
	}
}

func TestGeneratedMessages(t *testing.T) {
	m := &typepb.Type{Name: "Node", Syntax: typepb.Syntax_SYNTAX_PROTO3, Fields: []*typepb.Field{{Name: "count", Number: 7}}, Oneofs: []string{"a", "b"}}
	md := m.ProtoReflect().Descriptor()
	s := schema(t, md)
	a := adapter(t, md)
	raw := encoded(t, m)
	for _, expr := range []string{
		`m.name == "Node" && m.syntax == google.protobuf.Syntax.SYNTAX_PROTO3`,
		`m.fields.exists(f, f.number == 7 && f.name == "count") && m.oneofs == ["a", "b"]`,
		`has(m.name) && !has(m.source_context) && m.source_context.file_name == ""`,
		`type(m) == google.protobuf.Type && size(m.fields) == 1`,
		`m.?name.orValue("missing") == "Node" && !m.?source_context.hasValue()`,
	} {
		t.Run(expr, func(t *testing.T) {
			p := program(t, a, expr, cel.Variable("m", cel.ObjectType(string(md.FullName()))), cel.OptionalTypes())
			for _, v := range []any{m, s.NewReader(raw), s.NewReader(raw, pwr.AssumeUniqueScalars())} {
				truth(t, p, map[string]any{"m": v})
			}
		})
	}
}

func fixture(t testing.TB) protoreflect.MessageDescriptor {
	t.Helper()
	f := func(name string, n int32, kind descriptorpb.FieldDescriptorProto_Type, typename string) *descriptorpb.FieldDescriptorProto {
		v := &descriptorpb.FieldDescriptorProto{Name: proto.String(name), Number: proto.Int32(n), Type: kind.Enum(), Label: descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()}
		if typename != "" {
			v.TypeName = proto.String(typename)
		}
		return v
	}
	msg := &descriptorpb.DescriptorProto{Name: proto.String("Event")}
	msg.Field = []*descriptorpb.FieldDescriptorProto{
		f("name", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
		f("child", 2, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".test.Event"),
		f("values", 3, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".test.Event.ValuesEntry"),
		f("numbers", 4, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
		f("payload", 5, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".google.protobuf.Any"),
		f("wrapped", 6, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".google.protobuf.Int64Value"),
		f("time", 7, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".google.protobuf.Timestamp"),
		f("null_value", 8, descriptorpb.FieldDescriptorProto_TYPE_ENUM, ".google.protobuf.NullValue"),
		f("choice", 9, descriptorpb.FieldDescriptorProto_TYPE_STRING, ""),
		f("other", 10, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
		f("optional", 11, descriptorpb.FieldDescriptorProto_TYPE_INT64, ""),
		f("nan", 12, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, ""),
	}
	msg.Field[2].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	msg.Field[3].Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	msg.OneofDecl = []*descriptorpb.OneofDescriptorProto{{Name: proto.String("pick")}, {Name: proto.String("_optional")}}
	msg.Field[8].OneofIndex = proto.Int32(0)
	msg.Field[9].OneofIndex = proto.Int32(0)
	msg.Field[10].OneofIndex = proto.Int32(1)
	msg.Field[10].Proto3Optional = proto.Bool(true)
	msg.NestedType = []*descriptorpb.DescriptorProto{{Name: proto.String("ValuesEntry"), Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)}, Field: []*descriptorpb.FieldDescriptorProto{f("key", 1, descriptorpb.FieldDescriptorProto_TYPE_INT32, ""), f("value", 2, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, ".test.Event")}}}
	fd, e := protodesc.NewFile(&descriptorpb.FileDescriptorProto{Name: proto.String("event.proto"), Package: proto.String("test"), Syntax: proto.String("proto3"), Dependency: []string{"google/protobuf/any.proto", "google/protobuf/wrappers.proto", "google/protobuf/timestamp.proto", "google/protobuf/struct.proto"}, MessageType: []*descriptorpb.DescriptorProto{msg}}, protoregistry.GlobalFiles)
	if e != nil {
		t.Fatal(e)
	}
	return fd.Messages().Get(0)
}
func bytesField(b []byte, n protowire.Number, v []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(b, n, protowire.BytesType), v)
}
func intField(b []byte, n protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(b, n, protowire.VarintType), v)
}
func TestCollectionsPresenceAndMerge(t *testing.T) {
	md := fixture(t)
	a := adapter(t, md)
	s := schema(t, md)
	child := bytesField(nil, 1, []byte("nested"))
	raw := bytesField(nil, 2, child)
	raw = bytesField(raw, 2, intField(nil, 11, 0))
	raw = bytesField(raw, 3, bytesField(intField(nil, 1, 7), 2, child))
	raw = intField(raw, 4, 1)
	raw = bytesField(raw, 4, []byte{2, 3})
	raw = bytesField(raw, 9, []byte("lost"))
	raw = intField(raw, 10, 0)
	raw = intField(raw, 11, 0)
	native := dynamicpb.NewMessage(md)
	if e := proto.Unmarshal(raw, native); e != nil {
		t.Fatal(e)
	}
	for _, expr := range []string{
		`m.child.name == "nested" && has(m.child.optional)`,
		`m.values[7].name == "nested" && 7 in m.values && !(8 in m.values)`,
		`m.numbers == [1,2,3] && m.numbers.all(n, n > 0)`,
		`!has(m.choice) && has(m.other) && has(m.optional) && m.optional == 0`,
		`m.wrapped == null && m.null_value == 0 && !has(m.time)`,
	} {
		t.Run(expr, func(t *testing.T) {
			p := program(t, a, expr, cel.Variable("m", cel.ObjectType("test.Event")))
			for _, v := range []any{native, s.NewReader(raw), s.NewReader(raw, pwr.AssumeUniqueScalars())} {
				truth(t, p, map[string]any{"m": v})
			}
		})
	}
}

func TestSelectiveReadsAndErrors(t *testing.T) {
	md := (&typepb.Type{}).ProtoReflect().Descriptor()
	a := adapter(t, md)
	s := schema(t, md)
	raw := append(bytesField(nil, 1, []byte("ok")), 0x80)
	for _, expr := range []string{`has(m.name)`, `m.name == "ok"`, `m.?name.hasValue()`} {
		p := program(t, a, expr, cel.Variable("m", cel.ObjectType(string(md.FullName()))), cel.OptionalTypes())
		if _, _, e := p.Eval(map[string]any{"m": s.NewReader(raw)}); e == nil {
			t.Fatalf("%s swallowed wire error", expr)
		}
		truth(t, p, map[string]any{"m": s.NewReader(raw, pwr.AssumeUniqueScalars())})
	}
	p := program(t, a, `type(m) == google.protobuf.Type`, cel.Variable("m", cel.ObjectType(string(md.FullName()))))
	truth(t, p, map[string]any{"m": s.NewReader([]byte{0x80})})
	p = program(t, a, `m.name == "ok" || m.fields.size() > 0`, cel.Variable("m", cel.ObjectType(string(md.FullName()))))
	truth(t, p, map[string]any{"m": s.NewReader(raw, pwr.AssumeUniqueScalars())})
}

func TestAny(t *testing.T) {
	md := fixture(t)
	a := adapter(t, md)
	s := schema(t, md)
	payload := &anypb.Any{TypeUrl: "types.test/test.Event", Value: bytesField(nil, 1, []byte("inside"))}
	raw := bytesField(nil, 5, encoded(t, payload))
	p := program(t, a, `type(m.payload) == test.Event && m.payload.name == "inside"`, cel.Variable("m", cel.ObjectType("test.Event")))
	truth(t, p, map[string]any{"m": s.NewReader(raw)})
	payload.Value = append(payload.Value, 0x80)
	raw = bytesField(nil, 5, encoded(t, payload))
	if _, _, e := p.Eval(map[string]any{"m": s.NewReader(raw)}); e == nil {
		t.Fatal("invalid payload accepted")
	}
	truth(t, p, map[string]any{"m": s.NewReader(raw, pwr.AssumeUniqueScalars())})
	as := schema(t, (&anypb.Any{}).ProtoReflect().Descriptor())
	if v := a.NativeToValue(as.NewReader(encoded(t, &anypb.Any{TypeUrl: "unknown"}))); !types.IsError(v) {
		t.Fatal(v)
	}
	var nested proto.Message = &typepb.Type{Name: "x"}
	for i := 0; i < 8; i++ {
		nested, _ = anypb.New(nested)
	}
	if v := a.NativeToValue(as.NewReader(encoded(t, nested), pwr.WithLimits(pwr.Limits{MaxDepth: 4}))); !types.IsError(v) || !strings.Contains(fmt.Sprint(v), "limit") {
		t.Fatal(v)
	}
}

func TestWellKnown(t *testing.T) {
	st, _ := structpb.NewStruct(map[string]any{"s": "x", "list": []any{nil, true, 3.0}})
	for _, m := range []proto.Message{wrapperspb.Int64(42), wrapperspb.String("hello"), wrapperspb.Bytes([]byte("b")), wrapperspb.Double(1.25), wrapperspb.Bool(true), timestamppb.New(time.Unix(123, 456)), durationpb.New(3 * time.Second), st, structpb.NewNullValue(), structpb.NewListValue(&structpb.ListValue{Values: []*structpb.Value{structpb.NewNullValue()}})} {
		t.Run(string(m.ProtoReflect().Descriptor().FullName()), func(t *testing.T) {
			md := m.ProtoReflect().Descriptor()
			a := adapter(t, md)
			wire := a.NativeToValue(schema(t, md).NewReader(encoded(t, m)))
			native := a.NativeToValue(m)
			if eq := wire.Equal(native); eq != types.True {
				t.Fatalf("wire %v native %v eq %v", wire, native, eq)
			}
		})
	}
}

func TestNativeAnyRemainsLazy(t *testing.T) {
	md := fixture(t)
	a := adapter(t, md)
	payload := &anypb.Any{TypeUrl: "types.test/test.Event", Value: bytesField(nil, 1, []byte("inside"))}
	for _, value := range []any{payload, payload.ProtoReflect()} {
		p := program(t, a, `type(m) == test.Event && m.name == "inside"`, cel.Variable("m", cel.DynType))
		truth(t, p, map[string]any{"m": value})
	}
	generated := &typepb.Option{Value: payload}
	ga := adapter(t, md, generated.ProtoReflect().Descriptor())
	gp := program(t, ga, `type(m.value) == test.Event && m.value.name == "inside"`, cel.Variable("m", cel.ObjectType("google.protobuf.Option")))
	truth(t, gp, map[string]any{"m": generated})
	// The dynamic type exists only in this adapter, not in the global registry.
	outer := dynamicpb.NewMessage(md)
	outer.Set(md.Fields().ByNumber(5), protoreflect.ValueOfMessage(payload.ProtoReflect()))
	p := program(t, a, `type(m.payload) == test.Event && m.payload.name == "inside"`, cel.Variable("m", cel.ObjectType("test.Event")))
	truth(t, p, map[string]any{"m": outer})
	// Type-only evaluation must not decode an otherwise malformed payload.
	payload.Value = append(payload.Value, 0x80)
	p = program(t, a, `type(m.payload) == test.Event`, cel.Variable("m", cel.ObjectType("test.Event")))
	truth(t, p, map[string]any{"m": outer})
	p = program(t, a, `has(m.payload.name)`, cel.Variable("m", cel.ObjectType("test.Event")))
	if _, _, err := p.Eval(map[string]any{"m": outer}); err == nil {
		t.Fatal("presence swallowed a wire error")
	}
	if v := a.NativeToValue((*anypb.Any)(nil)); !types.IsError(v) {
		t.Fatal(v)
	}
	var deep proto.Message = &typepb.Type{Name: "deep"}
	for i := 0; i < 103; i++ {
		deep, _ = anypb.New(deep)
	}
	if v := a.NativeToValue(deep); !types.IsError(v) || !strings.Contains(fmt.Sprint(v), "limit") {
		t.Fatal(v)
	}
}

func TestEqualityAndConversion(t *testing.T) {
	md := (&typepb.Type{}).ProtoReflect().Descriptor()
	a := adapter(t, md)
	s := schema(t, md)
	m := &typepb.Type{Name: "x"}
	raw := encoded(t, m)
	for _, expr := range []string{`a == b`, `b == a`, `a == google.protobuf.Type{name: "x"}`, `google.protobuf.Type{name: "x"} == a`} {
		p := program(t, a, expr, cel.Variable("a", cel.ObjectType(string(md.FullName()))), cel.Variable("b", cel.ObjectType(string(md.FullName()))))
		for _, b := range []any{m, s.NewReader(raw)} {
			truth(t, p, map[string]any{"a": s.NewReader(raw), "b": b})
		}
	}
	v := a.NativeToValue(s.NewReader(raw))
	converted, e := v.ConvertToNative(reflect.TypeOf(m))
	if e != nil || !proto.Equal(converted.(proto.Message), m) {
		t.Fatal(converted, e)
	}
	reader, e := v.ConvertToNative(reflect.TypeOf((*pwr.Reader)(nil)))
	if e != nil || reader != v.Value() {
		t.Fatal(e)
	}
	// Equality must retain CEL's NaN behavior, not proto.Equal's NaN equality.
	md = fixture(t)
	a = adapter(t, md)
	s = schema(t, md)
	raw = protowire.AppendFixed64(protowire.AppendTag(nil, 12, protowire.Fixed64Type), math.Float64bits(math.NaN()))
	p := program(t, a, `a != b`, cel.Variable("a", cel.ObjectType("test.Event")), cel.Variable("b", cel.ObjectType("test.Event")))
	truth(t, p, map[string]any{"a": s.NewReader(raw), "b": s.NewReader(raw)})
	// Native fallback must accept dynamic parent messages containing generated Any.
	packed, _ := anypb.New(&typepb.Type{Name: "inside"})
	raw = bytesField(nil, 5, encoded(t, packed))
	eq := a.NativeToValue(s.NewReader(raw)).Equal(a.NativeToValue(s.NewReader(raw)))
	if eq != types.True {
		t.Fatal(eq)
	}
}

func TestConcurrentAdapter(t *testing.T) {
	m := &typepb.Type{Name: "x"}
	md := m.ProtoReflect().Descriptor()
	a := adapter(t, md)
	s := schema(t, md)
	raw := encoded(t, m)
	p := program(t, a, `m.name == "x"`, cel.Variable("m", cel.ObjectType(string(md.FullName()))))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				truth(t, p, map[string]any{"m": s.NewReader(raw)})
			}
		}()
	}
	wg.Wait()
}

func TestNativeCollectionEquality(t *testing.T) {
	m := &typepb.Type{Fields: []*typepb.Field{{Name: "x"}}}
	md := m.ProtoReflect().Descriptor()
	a := adapter(t, md)
	s := schema(t, md)
	raw := encoded(t, m)
	for _, expr := range []string{`n.fields == w.fields`, `n.fields[0] == w.fields[0]`, `w.fields[0] in n.fields`, `xs[0] == w.fields[0]`, `xs == w.fields`} {
		p := program(t, a, expr, cel.Variable("n", cel.ObjectType(string(md.FullName()))), cel.Variable("w", cel.ObjectType(string(md.FullName()))), cel.Variable("xs", cel.ListType(cel.ObjectType("google.protobuf.Field"))))
		truth(t, p, map[string]any{"n": m, "w": s.NewReader(raw), "xs": m.Fields})
	}
}

func TestNativeFallbackLimits(t *testing.T) {
	md := fixture(t)
	a := adapter(t, md)
	s := schema(t, md)
	var nested proto.Message = &typepb.Type{Name: "x"}
	for i := 0; i < 8; i++ {
		nested, _ = anypb.New(nested)
	}
	raw := bytesField(nil, 5, encoded(t, nested))
	v := a.NativeToValue(s.NewReader(raw, pwr.WithLimits(pwr.Limits{MaxDepth: 4})))
	if got := v.Equal(a.NativeToValue(s.NewReader(raw))); !types.IsError(got) {
		t.Fatal(got)
	}
	if _, err := v.ConvertToNative(reflect.TypeOf((*dynamicpb.Message)(nil))); err == nil {
		t.Fatal("conversion bypassed limits")
	}
	bad := &anypb.Any{TypeUrl: "types/test.Event", Value: []byte{0x80}}
	raw = bytesField(nil, 5, encoded(t, bad))
	v = a.NativeToValue(s.NewReader(raw))
	if got := v.Equal(a.NativeToValue(s.NewReader(raw))); !types.IsError(got) {
		t.Fatal(got)
	}
}

func TestEnvironmentIsolation(t *testing.T) {
	a := adapter(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := program(t, a, `m.name == "x" && optional.of(1).value() == 1`, cel.Types(&typepb.Type{}), cel.OptionalTypes(), cel.Variable("m", cel.ObjectType("google.protobuf.Type")))
			truth(t, p, map[string]any{"m": &typepb.Type{Name: "x"}})
		}()
	}
	wg.Wait()
	if _, ok := a.FindStructType("google.protobuf.Type"); ok {
		t.Fatal("environment mutated source adapter")
	}
}

func FuzzEvaluation(f *testing.F) {
	md := (&typepb.Type{}).ProtoReflect().Descriptor()
	a := adapter(f, md)
	s := schema(f, md)
	p := program(f, a, `has(m.name) && m.name.size() > 1 || m.fields.exists(x, x.number > 3) || m.oneofs.exists(x, x == "choice")`, cel.Variable("m", cel.ObjectType(string(md.FullName()))))
	f.Add(encoded(f, &typepb.Type{Name: "node", Fields: []*typepb.Field{{Name: "x", Number: 7}}, Oneofs: []string{"choice"}}))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 4096 {
			t.Skip()
		}
		m := &typepb.Type{}
		if proto.Unmarshal(raw, m) != nil {
			t.Skip()
		}
		want, _, ne := p.Eval(map[string]any{"m": m})
		got, _, we := p.Eval(map[string]any{"m": s.NewReader(raw)})
		if (ne != nil) != (we != nil) || (ne == nil && got.Equal(want) != types.True) {
			t.Fatalf("native %v %v, wire %v %v", want, ne, got, we)
		}
	})
}

var _ ref.TypeAdapter = (*Adapter)(nil)
var _ types.Provider = (*Adapter)(nil)
