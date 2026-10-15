package pwrcel

import (
	"fmt"
	"reflect"

	"cel.dev/cel-go/common/types"
	"cel.dev/cel-go/common/types/ref"
	"cel.dev/cel-go/common/types/traits"
	"github.com/lesomnus/pwr"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

type object struct {
	adapter *Adapter
	reader  *pwr.Reader
	typ     *types.Type
}

func (o *object) Type() ref.Type { return o.typ }

// Value exposes the borrowed Reader; ConvertToNative explicitly materializes.
func (o *object) Value() any { return o.reader }
func (o *object) ConvertToType(t ref.Type) ref.Val {
	if t == types.TypeType {
		return o.typ
	}
	if t.TypeName() == o.typ.TypeName() {
		return o
	}
	return types.NewErr("pwrcel: cannot convert %s to %s", o.typ, t)
}
func (o *object) field(index ref.Val) (protoreflect.FieldDescriptor, error) {
	n, ok := index.(types.String)
	if !ok {
		return nil, fmt.Errorf("pwrcel: field name must be a string")
	}
	fd := o.reader.Schema().Descriptor().Fields().ByName(protoreflect.Name(n))
	if fd == nil {
		return nil, fmt.Errorf("pwrcel: no such field %s", n)
	}
	return fd, nil
}
func (o *object) Get(index ref.Val) ref.Val {
	fd, err := o.field(index)
	if err != nil {
		return types.NewErr("%v", err)
	}
	v, err := o.reader.Get(fd.Number())
	if err != nil {
		return types.NewErr("%v", err)
	}
	if fd.Message() != nil && !fd.IsList() && !fd.IsMap() && v.Message() == nil {
		// Wrappers have nullable CEL types; ordinary unset messages have a zero view.
		if isWrapper(fd.Message().FullName()) {
			return types.NullValue
		}
		s, err := o.adapter.schema(fd.Message())
		if err != nil {
			return types.NewErr("%v", err)
		}
		child, err := o.reader.View(s, nil)
		if err != nil {
			return types.NewErr("%v", err)
		}
		return o.adapter.NativeToValue(child)
	}
	return o.adapter.NativeToValue(v)
}
func (o *object) IsSet(index ref.Val) ref.Val {
	fd, err := o.field(index)
	if err != nil {
		return types.NewErr("%v", err)
	}
	ok, err := o.reader.Has(fd.Number())
	if err != nil {
		return types.NewErr("%v", err)
	}
	return types.Bool(ok)
}
func (o *object) ConvertToNative(t reflect.Type) (any, error) {
	if t == reflect.TypeOf((*pwr.Reader)(nil)) {
		return o.reader, nil
	}
	m, err := o.adapter.materialize(o.reader)
	if err != nil {
		return nil, err
	}
	return o.adapter.registry.NativeToValue(m).ConvertToNative(t)
}
func (o *object) Equal(other ref.Val) ref.Val {
	if types.IsUnknownOrError(other) {
		return other
	}
	if other.Type().TypeName() != o.Type().TypeName() {
		return types.False
	}
	left, err := o.adapter.materialize(o.reader)
	if err != nil {
		return types.NewErr("%v", err)
	}
	var right proto.Message
	if w, ok := other.(*object); ok {
		right, err = w.adapter.materialize(w.reader)
	} else {
		right, _ = other.Value().(proto.Message)
	}
	if err != nil {
		return types.NewErr("%v", err)
	}
	if right == nil {
		return types.False
	}
	// Match CEL's proto equality, including NaN and Any semantics.
	return o.adapter.registry.NativeToValue(left).Equal(o.adapter.registry.NativeToValue(promote(proto.Clone(right))))
}

// Native protobuf values need the symmetric equality bridge as well. Otherwise
// CEL's native object would reject a Reader before the wire operand can decode.
type nativeObject struct {
	ref.Val
	adapter *Adapter
}

func (n *nativeObject) Get(k ref.Val) ref.Val {
	name, ok := k.(types.String)
	if !ok {
		return types.MaybeNoSuchOverloadErr(k)
	}
	fd, ok := n.adapter.registry.FindStructFieldType(n.Type().TypeName(), string(name))
	if !ok {
		return types.NewErr("no such field %s", name)
	}
	if fd.Type.Kind() == types.AnyKind {
		// CEL's normal field accessor unwraps Any before calling the adapter.
		// Keep the reflected envelope so its payload can remain a wire view.
		m := n.Value().(proto.Message).ProtoReflect()
		field := m.Descriptor().Fields().ByName(protoreflect.Name(name))
		if field != nil && !field.IsList() && !field.IsMap() && field.Message() != nil && field.Message().FullName() == "google.protobuf.Any" {
			return n.adapter.NativeToValue(m.Get(field).Message())
		}
	}
	v, err := fd.GetFrom(n.Value())
	if err != nil {
		return types.NewErr("%v", err)
	}
	return n.adapter.NativeToValue(v)
}
func (n *nativeObject) IsSet(k ref.Val) ref.Val { return n.Val.(traits.FieldTester).IsSet(k) }

func (n *nativeObject) Equal(other ref.Val) ref.Val {
	if w, ok := other.(*object); ok {
		return w.Equal(n.Val)
	}
	return n.Val.Equal(other)
}
func (a *Adapter) wrapNative(v ref.Val) ref.Val {
	switch v.(type) {
	case *object, *nativeObject:
		return v
	}
	if typ, ok := v.Type().(*types.Type); ok && typ.Kind() == types.StructKind {
		if _, ok := v.Value().(proto.Message); !ok {
			return v
		}
		return &nativeObject{Val: v, adapter: a}
	}
	return v
}
func isWrapper(n protoreflect.FullName) bool {
	switch n {
	case "google.protobuf.BoolValue", "google.protobuf.BytesValue", "google.protobuf.DoubleValue", "google.protobuf.FloatValue", "google.protobuf.Int32Value", "google.protobuf.Int64Value", "google.protobuf.StringValue", "google.protobuf.UInt32Value", "google.protobuf.UInt64Value":
		return true
	}
	return false
}

func (a *Adapter) materialize(r *pwr.Reader) (proto.Message, error) {
	m, err := r.Materialize()
	if err != nil {
		return nil, err
	}
	if err = a.guardAny(r); err != nil {
		return nil, err
	}
	return promote(m), nil
}

// CEL's native proto equality expects generated Any values. Promote registered
// generated subtypes in the independently owned materialization, without changing
// the caller's native messages or global registries.
func promote(m proto.Message) proto.Message {
	mr := m.ProtoReflect()
	md := mr.Descriptor()
	if mt, err := protoregistry.GlobalTypes.FindMessageByName(md.FullName()); err == nil && mt.Descriptor() == md {
		dst := mt.New().Interface()
		proto.Merge(dst, m)
		return dst
	}
	mr.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsMap() && fd.MapValue().Message() != nil:
			v.Map().Range(func(k protoreflect.MapKey, x protoreflect.Value) bool {
				v.Map().Set(k, protoreflect.ValueOfMessage(promote(x.Message().Interface()).ProtoReflect()))
				return true
			})
		case fd.IsList() && fd.Message() != nil:
			for i := 0; i < v.List().Len(); i++ {
				v.List().Set(i, protoreflect.ValueOfMessage(promote(v.List().Get(i).Message().Interface()).ProtoReflect()))
			}
		case fd.Message() != nil && !fd.IsMap() && !fd.IsList():
			mr.Set(fd, protoreflect.ValueOfMessage(promote(v.Message().Interface()).ProtoReflect()))
		}
		return true
	})
	return m
}

// Bound recursively packed Any images before CEL's eager equality/conversion
// fallback can unpack them outside the lazy reader's shared budget.
func (a *Adapter) guardAny(r *pwr.Reader) error {
	if r.Schema().Descriptor().FullName() == "google.protobuf.Any" {
		url, err := r.String(1)
		if err != nil {
			return err
		}
		data, err := r.Bytes(2)
		if err != nil {
			return err
		}
		name := protoreflect.FullName(url)
		for i := len(url) - 1; i >= 0; i-- {
			if url[i] == '/' {
				name = protoreflect.FullName(url[i+1:])
				break
			}
		}
		s, err := a.resolve(name)
		if err != nil {
			return nil
		} // native equality uses byte comparison for unresolved types
		c, err := r.View(s, data)
		if err != nil {
			return err
		}
		if err := c.Validate(); err != nil {
			return err
		}
		return a.guardAny(c)
	}
	fields := r.Schema().Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.Message() == nil {
			continue
		}
		if fd.IsMap() && fd.MapValue().Message() == nil {
			continue
		}
		v, err := r.Get(fd.Number())
		if err != nil {
			return err
		}
		if fd.IsMap() {
			var inner error
			v.Map().Range(func(_ protoreflect.MapKey, x pwr.Value) bool { inner = a.guardAny(x.Message()); return inner == nil })
			if inner != nil {
				return inner
			}
		} else if fd.IsList() {
			for j := 0; j < v.List().Len(); j++ {
				x, _ := v.List().Get(j)
				if err = a.guardAny(x.Message()); err != nil {
					return err
				}
			}
		} else if v.Message() != nil {
			if err = a.guardAny(v.Message()); err != nil {
				return err
			}
		}
	}
	return nil
}
