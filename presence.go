package pwr

import (
	"google.golang.org/protobuf/reflect/protoreflect"
	"math"
)

// Has reports protobuf presence: explicit-presence fields use occurrence;
// implicit-presence scalars use non-default value. Empty packed lists are absent.
func (r *Reader) Has(n protoreflect.FieldNumber) (bool, error) {
	slot, f, err := r.lookup(n)
	if err != nil {
		return false, err
	}
	if f.descriptor.IsMap() {
		v, e := r.Map(n)
		return v.Len() > 0, e
	}
	if f.descriptor.IsList() {
		v, e := r.List(n)
		return v.Len() > 0, e
	}
	if err = r.ensure(slot, f.composite() || f.oneof != 0); err != nil {
		return false, err
	}
	if r.index[slot] == 0 {
		return false, nil
	}
	if f.descriptor.HasPresence() {
		return true, nil
	}
	v, err := r.scalar(n)
	if err != nil {
		return false, err
	}
	switch f.descriptor.Kind() {
	case protoreflect.StringKind:
		return v.String() != "", nil
	case protoreflect.BytesKind:
		return len(v.Bytes()) != 0, nil
	case protoreflect.BoolKind:
		return v.Bool(), nil
	case protoreflect.EnumKind:
		return v.Enum() != 0, nil
	case protoreflect.FloatKind, protoreflect.DoubleKind:
		return v.Float() != 0 || math.Signbit(v.Float()), nil
	case protoreflect.Uint32Kind, protoreflect.Uint64Kind, protoreflect.Fixed32Kind, protoreflect.Fixed64Kind:
		return v.Uint() != 0, nil
	default:
		return v.Int() != 0, nil
	}
}
