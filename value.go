package pwr

import "google.golang.org/protobuf/reflect/protoreflect"

// Value is a read-only protobuf value. Scalar methods follow protoreflect.Value's
// type contract and panic on an incompatible kind. Composite methods return nil
// when the value is not that category. String/bytes values borrow input storage.
type Value struct {
	kind    protoreflect.Kind
	scalar  protoreflect.Value
	message *Reader
	list    *List
	mapping *Map
}

func (v Value) Kind() protoreflect.Kind       { return v.kind }
func (v Value) IsValid() bool                 { return v.kind != 0 }
func (v Value) Bool() bool                    { return v.scalar.Bool() }
func (v Value) Int() int64                    { return v.scalar.Int() }
func (v Value) Uint() uint64                  { return v.scalar.Uint() }
func (v Value) Float() float64                { return v.scalar.Float() }
func (v Value) String() string                { return v.scalar.String() }
func (v Value) Bytes() []byte                 { return v.scalar.Bytes() }
func (v Value) Enum() protoreflect.EnumNumber { return v.scalar.Enum() }
func (v Value) Message() *Reader              { return v.message }
func (v Value) List() *List                   { return v.list }
func (v Value) Map() *Map                     { return v.mapping }

// Scalar returns a scalar reflection value, or an invalid value for composites.
func (v Value) Scalar() protoreflect.Value { return v.scalar }

// Get reads any declared field by number. Missing messages have Message()==nil;
// missing collections are empty. Scalar defaults come from the descriptor.
func (r *Reader) Get(n protoreflect.FieldNumber) (Value, error) {
	_, f, err := r.lookup(n)
	if err != nil {
		return Value{}, err
	}
	v := Value{kind: f.descriptor.Kind()}
	switch {
	case f.descriptor.IsMap():
		v.mapping, err = r.Map(n)
	case f.descriptor.IsList():
		v.list, err = r.List(n)
	case f.child != nil:
		v.message, err = r.Message(n)
	default:
		v.scalar, err = r.scalar(n)
	}
	return v, err
}
