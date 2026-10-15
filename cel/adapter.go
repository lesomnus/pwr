// Package pwrcel integrates pwr readers with cel-go without unpacking ordinary
// message payloads. It is a separate module so the core reader does not depend on CEL.
package pwrcel

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"cel.dev/cel-go/cel"
	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/pb"
	"cel.dev/cel-go/common/types/ref"
	"github.com/lesomnus/pwr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// Adapter provides immutable CEL type metadata and lazily cached pwr schemas.
// It may be shared by environments/programs. Each evaluation must own its Readers.
type Adapter struct {
	registry    *types.Registry
	descriptors map[protoreflect.FullName]protoreflect.MessageDescriptor
	schemas     sync.Map
}

// New registers the supplied descriptors and all messages in their files and
// dependencies. Common well-known types are available automatically.
func New(descriptors ...protoreflect.MessageDescriptor) (*Adapter, error) {
	registry, err := types.NewRegistry()
	if err != nil {
		return nil, err
	}
	a := &Adapter{registry: registry, descriptors: make(map[protoreflect.FullName]protoreflect.MessageDescriptor)}
	visited := make(map[protoreflect.FileDescriptor]bool)
	var messages func(protoreflect.MessageDescriptors) error
	messages = func(ms protoreflect.MessageDescriptors) error {
		for i := 0; i < ms.Len(); i++ {
			md := ms.Get(i)
			if prev := a.descriptors[md.FullName()]; prev != nil && prev != md {
				return fmt.Errorf("pwrcel: conflicting descriptor %s", md.FullName())
			}
			a.descriptors[md.FullName()] = md
			if err := messages(md.Messages()); err != nil {
				return err
			}
		}
		return nil
	}
	var file func(protoreflect.FileDescriptor) error
	file = func(fd protoreflect.FileDescriptor) error {
		if visited[fd] {
			return nil
		}
		visited[fd] = true
		for i := 0; i < fd.Imports().Len(); i++ {
			if err := file(fd.Imports().Get(i).FileDescriptor); err != nil {
				return err
			}
		}
		if err := registry.RegisterDescriptor(fd); err != nil {
			return err
		}
		return messages(fd.Messages())
	}
	for _, m := range []proto.Message{&anypb.Any{}, &durationpb.Duration{}, &timestamppb.Timestamp{}, &structpb.Struct{}, &wrapperspb.BoolValue{}} {
		if err = file(m.ProtoReflect().Descriptor().ParentFile()); err != nil {
			return nil, err
		}
	}
	for _, md := range descriptors {
		if md == nil || md.IsPlaceholder() {
			return nil, pwr.ErrInvalidSchema
		}
		if err = file(md.ParentFile()); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// EnvOptions installs this adapter and provider. Add cel.Variable declarations
// and other environment options as usual.
func (a *Adapter) EnvOptions() []cel.EnvOption {
	return []cel.EnvOption{func(env *cel.Env) (*cel.Env, error) {
		// CEL options such as OptionalTypes register additional types. Keep those
		// mutations local to environment construction so Adapter remains shareable.
		copy := &Adapter{registry: a.registry.Copy(), descriptors: make(map[protoreflect.FullName]protoreflect.MessageDescriptor, len(a.descriptors))}
		for n, md := range a.descriptors {
			copy.descriptors[n] = md
		}
		p := &environmentProvider{copy}
		var err error
		env, err = cel.CustomTypeAdapter(copy)(env)
		if err != nil {
			return nil, err
		}
		return cel.CustomTypeProvider(p)(env)
	}}
}

type environmentProvider struct{ *Adapter }

func (p *environmentProvider) RegisterType(ts ...ref.Type) error {
	return p.registry.RegisterType(ts...)
}
func (p *environmentProvider) RegisterDescriptor(fd protoreflect.FileDescriptor) error {
	if err := p.registry.RegisterDescriptor(fd); err != nil {
		return err
	}
	visited := make(map[protoreflect.FileDescriptor]bool)
	var messages func(protoreflect.MessageDescriptors) error
	messages = func(ms protoreflect.MessageDescriptors) error {
		for i := 0; i < ms.Len(); i++ {
			md := ms.Get(i)
			if old := p.descriptors[md.FullName()]; old != nil && old != md {
				return fmt.Errorf("pwrcel: conflicting descriptor %s", md.FullName())
			}
			p.descriptors[md.FullName()] = md
			if err := messages(md.Messages()); err != nil {
				return err
			}
		}
		return nil
	}
	var file func(protoreflect.FileDescriptor) error
	file = func(f protoreflect.FileDescriptor) error {
		if visited[f] {
			return nil
		}
		visited[f] = true
		for i := 0; i < f.Imports().Len(); i++ {
			if err := file(f.Imports().Get(i).FileDescriptor); err != nil {
				return err
			}
		}
		return messages(f.Messages())
	}
	return file(fd)
}
func (a *Adapter) EnumValue(n string) ref.Val                  { return a.registry.EnumValue(n) }
func (a *Adapter) FindIdent(n string) (ref.Val, bool)          { return a.registry.FindIdent(n) }
func (a *Adapter) FindStructType(n string) (*types.Type, bool) { return a.registry.FindStructType(n) }
func (a *Adapter) FindStructFieldNames(n string) ([]string, bool) {
	return a.registry.FindStructFieldNames(n)
}
func (a *Adapter) FindStructFieldType(n, f string) (*types.FieldType, bool) {
	t, ok := a.registry.FindStructFieldType(n, f)
	if !ok {
		return nil, false
	}
	// CEL's optimized FieldTester returns bool and cannot carry a wire error.
	// Use traits.FieldTester instead so malformed has()/optional access is an error.
	return &types.FieldType{Type: t.Type, IsJSONField: t.IsJSONField}, true
}
func (a *Adapter) NewValue(n string, fields map[string]ref.Val) ref.Val {
	return a.wrapNative(a.registry.NewValue(n, fields))
}

func (a *Adapter) schema(md protoreflect.MessageDescriptor) (*pwr.Schema, error) {
	if s, ok := a.schemas.Load(md); ok {
		return s.(*pwr.Schema), nil
	}
	s, err := pwr.Compile(md)
	if err != nil {
		return nil, err
	}
	v, _ := a.schemas.LoadOrStore(md, s)
	return v.(*pwr.Schema), nil
}
func (a *Adapter) resolve(name protoreflect.FullName) (*pwr.Schema, error) {
	md := a.descriptors[name]
	if md == nil {
		mt, err := protoregistry.GlobalTypes.FindMessageByName(name)
		if err != nil {
			return nil, fmt.Errorf("pwrcel: unknown Any type %s", name)
		}
		md = mt.Descriptor()
	}
	return a.schema(md)
}

// NativeToValue recognizes Readers, Values, Lists and Maps in addition to normal
// CEL/protobuf inputs. Any dispatch is lazy; other WKT conversions use CEL's
// standard behavior via an eager native fallback.
func (a *Adapter) NativeToValue(value any) ref.Val {
	switch v := value.(type) {
	case ref.Val:
		return v
	case *anypb.Any:
		if v == nil {
			return types.UnsupportedRefValConversionErr(v)
		}
		return a.nativeAny(v.GetTypeUrl(), v.GetValue())
	case protoreflect.Message:
		if v.Descriptor().FullName() == "google.protobuf.Any" {
			fields := v.Descriptor().Fields()
			return a.nativeAny(v.Get(fields.ByNumber(1)).String(), v.Get(fields.ByNumber(2)).Bytes())
		}
		return a.wrapNative(a.registry.NativeToValue(v.Interface()))
	case protoreflect.Value:
		return a.NativeToValue(v.Interface())
	case protoreflect.List:
		return types.NewProtoList(a, v)
	case *pb.Map:
		return types.NewProtoMap(a, v)
	case *pwr.Reader:
		if v == nil {
			return types.NullValue
		}
		if err := v.Err(); err != nil {
			return types.NewErr("%v", err)
		}
		name := v.Schema().Descriptor().FullName()
		if name == "google.protobuf.Any" {
			url, err := v.String(1)
			if err != nil {
				return types.NewErr("%v", err)
			}
			data, err := v.Bytes(2)
			if err != nil {
				return types.NewErr("%v", err)
			}
			name := protoreflect.FullName(url[strings.LastIndex(url, "/")+1:])
			s, err := a.resolve(name)
			if err != nil {
				return types.NewErr("%v", err)
			}
			child, err := v.View(s, data)
			if err != nil {
				return types.NewErr("%v", err)
			}
			return a.NativeToValue(child)
		}
		if wellKnown(name) {
			m, err := v.Materialize()
			if err != nil {
				return types.NewErr("%v", err)
			}
			return a.registry.NativeToValue(m)
		}
		return &object{adapter: a, reader: v, typ: types.NewObjectType(string(name))}
	case pwr.Value:
		if v.Map() != nil {
			return a.NativeToValue(v.Map())
		}
		if v.List() != nil {
			return a.NativeToValue(v.List())
		}
		if v.Kind() == protoreflect.MessageKind || v.Kind() == protoreflect.GroupKind {
			return a.NativeToValue(v.Message())
		}
		if !v.Scalar().IsValid() {
			return types.NewErr("pwrcel: invalid value")
		}
		return a.registry.NativeToValue(v.Scalar().Interface())
	case *pwr.List:
		values := make([]pwr.Value, v.Len())
		v.Range(func(i int, x pwr.Value) bool { values[i] = x; return true })
		return types.NewDynamicList(a, values)
	case *pwr.Map:
		values := make(map[any]any, v.Len())
		v.Range(func(k protoreflect.MapKey, x pwr.Value) bool {
			key := k.Interface()
			switch n := key.(type) {
			case int32:
				key = int64(n)
			case uint32:
				key = uint64(n)
			}
			values[key] = x
			return true
		})
		return types.NewDynamicMap(a, values)
	default:
		// Retain this adapter on native collections for symmetric object equality.
		if _, bytes := value.([]byte); !bytes && value != nil {
			switch reflect.TypeOf(value).Kind() {
			case reflect.Array, reflect.Slice:
				return types.NewDynamicList(a, value)
			case reflect.Map:
				return types.NewDynamicMap(a, value)
			}
		}
		return a.wrapNative(a.registry.NativeToValue(value))
	}
}

// A decoded envelope still carries an encoded Any payload. Keep that payload
// as a wire view instead of using the registry's eager Any.UnmarshalNew path.
func (a *Adapter) nativeAny(url string, raw []byte) ref.Val {
	name := protoreflect.FullName(url[strings.LastIndex(url, "/")+1:])
	s, err := a.resolve(name)
	if err != nil {
		return types.NewErr("%v", err)
	}
	return a.NativeToValue(s.NewReader(raw))
}

func wellKnown(n protoreflect.FullName) bool {
	switch n {
	case "google.protobuf.Timestamp", "google.protobuf.Duration", "google.protobuf.Struct", "google.protobuf.Value", "google.protobuf.ListValue", "google.protobuf.BoolValue", "google.protobuf.BytesValue", "google.protobuf.DoubleValue", "google.protobuf.FloatValue", "google.protobuf.Int32Value", "google.protobuf.Int64Value", "google.protobuf.StringValue", "google.protobuf.UInt32Value", "google.protobuf.UInt64Value":
		return true
	}
	return false
}
