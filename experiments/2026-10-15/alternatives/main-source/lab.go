package lab

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"
	"unsafe"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"github.com/lesomnus/cr/api"
	"github.com/lesomnus/payday/pdpb"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const Artifact = "application/vnd.dev.sigstore.bundle.v0.3+json"

var Descriptor = (&api.Manifest{}).ProtoReflect().Descriptor()
var ObjectType = types.NewObjectType(string(Descriptor.FullName()))
var Fields = []protoreflect.FieldDescriptor{Descriptor.Fields().ByName("artifact_type"), Descriptor.Fields().ByName("repo"), Descriptor.Fields().ByName("digest"), Descriptor.Fields().ByName("media_type")}
var Numbers = []int{int(Fields[0].Number()), int(Fields[1].Number()), int(Fields[2].Number()), int(Fields[3].Number())}

type Session interface {
	Get(protoreflect.FieldDescriptor) (string, error)
	Close()
}
type Backend struct {
	Name string
	// Prepare receives already-instantiated descriptors; compilation is outside timing.
	Prepare          func([]protoreflect.FieldDescriptor) (func([]byte) (Session, error), error)
	FirstWins        bool
	ValidUTF8Assumed bool
	Note             string
}

var Backends []Backend

func Register(b Backend)         { Backends = append(Backends, b) }
func StringView(b []byte) string { return unsafe.String(unsafe.SliceData(b), len(b)) }

type Fixture struct {
	Name  string
	Event *pdpb.WatchEvent
	Raw   []byte
}

func Fixtures() []Fixture {
	var out []Fixture
	for _, name := range []string{"small", "blob8k", "map256", "blob1m", "map256_late", "missing"} {
		at := timestamppb.New(time.Unix(1700000000, 0))
		m := api.Manifest_builder{Id: []byte("0123456789abcdef"), Repo: "stage", Digest: "sha256:" + strings.Repeat("a", 64), MediaType: "application/vnd.oci.image.manifest.v1+json", ArtifactType: Artifact, Size: 1024, DateCreated: at, DateUpdated: at}.Build()
		if name == "blob8k" || name == "blob1m" {
			n := 8192
			if name == "blob1m" {
				n = 1 << 20
			}
			m.SetAnnotations(map[string]string{"example.test/payload": strings.Repeat("x", n)})
		}
		if name == "map256" || name == "map256_late" {
			a := map[string]string{}
			for i := 0; i < 256; i++ {
				a[fmt.Sprintf("example.test/key%03d", i)] = strings.Repeat("x", 12)
			}
			m.SetAnnotations(a)
		}
		if name == "missing" {
			m.SetArtifactType("")
		}
		raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
		if err != nil {
			panic(err)
		}
		if name == "map256_late" {
			var head, tail []byte
			for d := raw; len(d) > 0; {
				num, typ, n := protowire.ConsumeTag(d)
				count := protowire.ConsumeFieldValue(num, typ, d[n:])
				if num == 11 {
					tail = append(tail, d[:n+count]...)
				} else {
					head = append(head, d[:n+count]...)
				}
				d = d[n+count:]
			}
			raw = append(head, tail...)
		}
		event := pdpb.WatchEvent_builder{Id: []byte("fedcba9876543210"), Value: &anypb.Any{TypeUrl: "type.googleapis.com/app.Manifest", Value: raw}, Kind: pdpb.ChangeKind_CHANGE_KIND_CREATED, Method: "/cr.index.Manifest/Put", CallMethod: "/cr.index.Manifest/Put", DateCreated: at}.Build()
		out = append(out, Fixture{name, event, raw})
	}
	return out
}
func Query(count int) string {
	s := `has(event.value) && type(event.value) == app.Manifest && event.kind == payday.CHANGE_KIND_CREATED && event.value.artifact_type == "` + Artifact + `"`
	if count == 4 {
		s += ` && event.value.repo == "stage" && event.value.digest.startsWith("sha256:") && event.value.media_type == "application/vnd.oci.image.manifest.v1+json"`
	}
	return s
}

type binding struct {
	event *pdpb.WatchEvent
	value *view
}
type view struct{ session Session }

func (v *view) Type() ref.Type { return ObjectType }
func (v *view) Value() any     { return v }
func (v *view) ConvertToNative(t reflect.Type) (any, error) {
	return nil, fmt.Errorf("unsupported native conversion %v", t)
}
func (v *view) ConvertToType(t ref.Type) ref.Val {
	if t == types.TypeType {
		return ObjectType
	}
	if t.TypeName() == ObjectType.TypeName() {
		return v
	}
	return types.NewErr("unsupported conversion")
}
func (v *view) Equal(ref.Val) ref.Val { return types.NewErr("unsupported message equality") }
func (v *view) Get(key ref.Val) ref.Val {
	s, ok := key.(types.String)
	if !ok {
		return types.NewErr("invalid field")
	}
	fd := Descriptor.Fields().ByName(protoreflect.Name(s))
	if fd == nil {
		return types.NewErr("unknown field")
	}
	x, err := v.session.Get(fd)
	if err != nil {
		return types.NewErr("%v", err)
	}
	return types.String(x)
}
func (v *view) IsSet(key ref.Val) ref.Val {
	v1 := v.Get(key)
	if types.IsError(v1) {
		return v1
	}
	return types.Bool(v1 != types.String(""))
}

type provider struct{ *types.Registry }

func (p *provider) FindStructFieldType(name, field string) (*types.FieldType, bool) {
	base, ok := p.Registry.FindStructFieldType(name, field)
	if !ok || name != "payday.WatchEvent" {
		return base, ok
	}
	out := *base
	out.GetFrom = func(obj any) (any, error) {
		b, ok := obj.(*binding)
		if !ok {
			return base.GetFrom(obj)
		}
		if field == "value" {
			return b.value, nil
		}
		return base.GetFrom(b.event)
	}
	out.IsSet = func(obj any) bool {
		if b, ok := obj.(*binding); ok {
			return base.IsSet(b.event)
		}
		return base.IsSet(obj)
	}
	return &out, true
}
func Program(count int, native bool) (cel.Program, error) {
	reg, err := types.NewRegistry(&pdpb.WatchEvent{}, &api.Manifest{})
	if err != nil {
		return nil, err
	}
	opts := []cel.EnvOption{cel.Variable("event", cel.ObjectType("payday.WatchEvent")), cel.Constant("payday.CHANGE_KIND_CREATED", cel.IntType, types.Int(1)), cel.ParserExpressionSizeLimit(16384), cel.ParserRecursionLimit(64)}
	if native {
		opts = append([]cel.EnvOption{cel.Types(&pdpb.WatchEvent{}, &api.Manifest{})}, opts...)
	} else {
		p := &provider{reg}
		opts = append([]cel.EnvOption{cel.CustomTypeProvider(p), cel.CustomTypeAdapter(p)}, opts...)
	}
	env, err := cel.NewEnv(opts...)
	if err != nil {
		return nil, err
	}
	a, issues := env.Compile(Query(count))
	if issues.Err() != nil {
		return nil, issues.Err()
	}
	return env.Program(a, cel.CostLimit(100000), cel.InterruptCheckFrequency(64))
}
func Eval(p cel.Program, event *pdpb.WatchEvent, s Session) (bool, error) {
	var input any = event
	if s != nil {
		input = &binding{event, &view{s}}
	}
	v, _, err := p.ContextEval(context.Background(), map[string]any{"event": input})
	return v == types.True, err
}
func Direct(s Session, count int) (bool, error) {
	a, err := s.Get(Fields[0])
	if err != nil || a != Artifact {
		return false, err
	}
	if count == 1 {
		return true, nil
	}
	r, err := s.Get(Fields[1])
	if err != nil || r != "stage" {
		return false, err
	}
	d, err := s.Get(Fields[2])
	if err != nil || !strings.HasPrefix(d, "sha256:") {
		return false, err
	}
	m, err := s.Get(Fields[3])
	return m == "application/vnd.oci.image.manifest.v1+json", err
}
