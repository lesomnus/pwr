package pwr

import (
	"fmt"
	"google.golang.org/protobuf/reflect/protoreflect"
	"math"
	"sort"
)

// ReaderOption configures a newly constructed Reader.
type ReaderOption func(*Reader)

// AssumeUniqueScalars permits early return for singular scalars outside real
// oneofs. The caller promises no duplicate scalar records. Merged submessages
// with multiple occurrences still select the last scalar across those fragments.
func AssumeUniqueScalars() ReaderOption { return func(r *Reader) { r.first = true } }

// AssumeValidUTF8 skips validation of strings. Wire boundary checks remain.
func AssumeValidUTF8() ReaderOption { return func(r *Reader) { r.assumeUTF8 = true } }

// Limits bounds work across a reader and its descendants. MaxDepth defaults to
// 100; the root has depth zero. Zero MaxFields/MaxBytes means unlimited. Counts
// include records revisited while extracting a composite field. MaxBytes counts
// wire record lengths, including skipped payloads, not physical memory reads.
type Limits struct {
	MaxDepth            int
	MaxFields, MaxBytes int64
}

// WithLimits configures limits. Negative values fail on the first access.
func WithLimits(l Limits) ReaderOption { return func(r *Reader) { r.limits = l } }

type readBudget struct {
	fields, bytes int64
	err           error
}
type segment struct {
	data  []byte
	start int
}

// Reader indexes borrowed wire bytes. It and its descendants must not be used
// concurrently. Returned strings/bytes also borrow the input: keep it immutable
// while any of these values is in use.
type Reader struct {
	owner                   *Pool
	returned                bool
	schema                  *Schema
	raw                     []byte
	parts                   []segment
	size                    int
	index                   []uint64
	cursor                  int
	first, assumeUTF8, done bool
	err                     error
	active                  []int
	cache                   map[int]any
	limits                  Limits
	budget                  *readBudget
	depth                   int
}

// NewReader borrows raw without scanning. Errors are reported by accessors.
func (s *Schema) NewReader(raw []byte, options ...ReaderOption) *Reader {
	r := &Reader{schema: s, raw: raw, size: len(raw)}
	for _, o := range options {
		if o != nil {
			o(r)
		}
	}
	r.initialize()
	return r
}
func (r *Reader) initialize() {
	if r.schema == nil || r.schema.descriptor == nil {
		r.err = ErrInvalidSchema
		return
	}
	if uint64(r.size) > math.MaxInt32 {
		r.err = ErrInputTooLarge
		return
	}
	if r.limits.MaxDepth < 0 || r.limits.MaxFields < 0 || r.limits.MaxBytes < 0 {
		r.err = ErrInvalidOption
		return
	}
	if r.limits.MaxDepth == 0 {
		r.limits.MaxDepth = 100
	}
	if r.limits.MaxFields > 0 || r.limits.MaxBytes > 0 {
		r.budget = &readBudget{}
	}
}

// Schema returns this reader's compiled descriptor graph.
func (r *Reader) Schema() *Schema {
	if r == nil {
		return nil
	}
	return r.schema
}

func (r *Reader) status() error {
	if r != nil && r.returned {
		return ErrPoolOwnership
	}
	if r == nil || r.schema == nil {
		return ErrInvalidSchema
	}
	if r.err != nil {
		return r.err
	}
	if r.budget != nil && r.budget.err != nil {
		return r.budget.err
	}
	return nil
}
func (r *Reader) lookup(n protoreflect.FieldNumber) (int, field, error) {
	if err := r.status(); err != nil {
		return 0, field{}, err
	}
	slot := r.schema.slot(n)
	if slot < 0 {
		return 0, field{}, fmt.Errorf("%w: %d", ErrUnknownField, n)
	}
	return slot, r.schema.fields[slot], nil
}
func (r *Reader) ensure(slot int, whole bool) error {
	if r.index == nil {
		r.index = make([]uint64, len(r.schema.fields))
	}
	if !r.done && (whole || !r.first || r.index[slot] == 0) {
		return r.scan(slot, whole)
	}
	return nil
}
func (r *Reader) full() error {
	if err := r.status(); err != nil {
		return err
	}
	if r.index == nil {
		r.index = make([]uint64, len(r.schema.fields))
	}
	if !r.done {
		return r.scan(-1, true)
	}
	return nil
}
func pack(start, end int) uint64 { return uint64(uint32(start+1))<<32 | uint64(uint32(end)) }
func unpack(x uint64) (int, int) { return int(uint32(x>>32)) - 1, int(uint32(x)) }
func (r *Reader) at(pos int) []byte {
	if r.parts == nil {
		return r.raw[pos:]
	}
	i := sort.Search(len(r.parts), func(i int) bool { return r.parts[i].start+len(r.parts[i].data) > pos })
	if i == len(r.parts) {
		return nil
	}
	return r.parts[i].data[pos-r.parts[i].start:]
}
func (r *Reader) bytes(x uint64) []byte { a, b := unpack(x); return r.at(a)[: b-a : b-a] }
func (r *Reader) remember(slot int, v any) {
	if r.cache == nil {
		r.cache = make(map[int]any)
	}
	r.cache[slot] = v
}
func (r *Reader) child(s *Schema, parts [][]byte) (*Reader, error) {
	if r.depth >= r.limits.MaxDepth {
		return nil, r.failLimit("message depth")
	}
	c := &Reader{schema: s, first: r.first, assumeUTF8: r.assumeUTF8, limits: r.limits, budget: r.budget, depth: r.depth + 1}
	if len(parts) > 1 {
		c.first = false
	}
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		c.parts = append(c.parts, segment{p, c.size})
		c.size += len(p)
	}
	if len(c.parts) == 1 {
		c.raw = c.parts[0].data
		c.parts = nil
	}
	return c, nil
}
func (r *Reader) failLimit(what string) error {
	r.err = fmt.Errorf("%w: %s", ErrLimitExceeded, what)
	if r.budget != nil {
		r.budget.err = r.err
	}
	return r.err
}
func (r *Reader) charge(n int) error {
	if r.budget == nil {
		return nil
	}
	b := r.budget
	b.fields++
	b.bytes += int64(n)
	if r.limits.MaxFields > 0 && b.fields > r.limits.MaxFields {
		return r.failLimit("wire records")
	}
	if r.limits.MaxBytes > 0 && b.bytes > r.limits.MaxBytes {
		return r.failLimit("wire bytes")
	}
	return nil
}

// WhichOneof returns the selected field number, or zero for an unset oneof.
// Synthetic proto3 optional oneofs are also accepted.
func (r *Reader) WhichOneof(name protoreflect.Name) (protoreflect.FieldNumber, error) {
	if err := r.status(); err != nil {
		return 0, err
	}
	o := r.schema.descriptor.Oneofs().ByName(name)
	if o == nil {
		return 0, fmt.Errorf("%w: oneof %s", ErrUnknownField, name)
	}
	if err := r.full(); err != nil {
		return 0, err
	}
	for i := 0; i < o.Fields().Len(); i++ {
		n := o.Fields().Get(i).Number()
		if r.index[r.schema.slot(n)] != 0 {
			return n, nil
		}
	}
	return 0, nil
}

// Message returns a lazily read child, or nil for an absent field. Duplicate
// singular message/group records are merged without concatenating their bytes.
func (r *Reader) Message(n protoreflect.FieldNumber) (*Reader, error) {
	slot, f, err := r.lookup(n)
	if err != nil {
		return nil, err
	}
	if f.child == nil || f.descriptor.IsList() || f.descriptor.IsMap() {
		return nil, fmt.Errorf("%w: %d is not a singular message", ErrUnsupportedField, n)
	}
	if err = r.ensure(slot, true); err != nil {
		return nil, err
	}
	if r.index[slot] == 0 {
		return nil, nil
	}
	if v, ok := r.cache[slot]; ok {
		return v.(*Reader), nil
	}
	var parts [][]byte
	err = r.records(slot, func(rec record) error { parts = append(parts, rec.data); return nil })
	if err != nil {
		return nil, err
	}
	child, err := r.child(f.child, parts)
	if err == nil {
		r.remember(slot, child)
	}
	return child, err
}
