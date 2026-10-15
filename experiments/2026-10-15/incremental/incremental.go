//go:build incremental

package lab

import (
	"fmt"
	"math"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// indexPlan is shared by sessions. It covers every known top-level field, not
// just fields declared by a particular filter. Unknown fields are skipped.
// This prototype has the same dense-number limit as the earlier table reader.
type indexPlan struct {
	descriptor protoreflect.MessageDescriptor
	slots      []int
	strings    []bool
}

func newIndexPlan(md protoreflect.MessageDescriptor) (*indexPlan, error) {
	max := 0
	for i := 0; i < md.Fields().Len(); i++ {
		n := int(md.Fields().Get(i).Number())
		if n > 4096 {
			return nil, fmt.Errorf("field number exceeds dense index limit")
		}
		if n > max {
			max = n
		}
	}
	p := &indexPlan{descriptor: md, slots: make([]int, max+1), strings: make([]bool, md.Fields().Len())}
	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		p.slots[fd.Number()] = i + 1
		p.strings[i] = fd.Kind() == protoreflect.StringKind
	}
	return p, nil
}

// An entry packs start/end offsets of the raw field value (including its length
// prefix) into 64 bits. The wire type is checked against the descriptor before
// storing a known string field. Non-string fields are indexed as raw values but
// their protobuf semantics are intentionally not exposed by Get in this POC.
type incrementalSession struct {
	raw    []byte
	plan   *indexPlan
	index  []uint64
	cursor int
	first  bool
	done   bool
	err    error
}

func (p *indexPlan) session(raw []byte, first bool) (Session, error) {
	if uint64(len(raw)) > math.MaxUint32 {
		return nil, fmt.Errorf("input exceeds 32-bit offsets")
	}
	return &incrementalSession{raw: raw, plan: p, first: first}, nil
}

func (s *incrementalSession) Close() {}

func (s *incrementalSession) fail(err error) error { s.err = err; return err }

func (s *incrementalSession) scan(target int) error {
	for s.cursor < len(s.raw) {
		d := s.raw[s.cursor:]
		num, typ, n := protowire.ConsumeTag(d)
		if n < 0 {
			return s.fail(protowire.ParseError(n))
		}
		start := s.cursor + n
		k := protowire.ConsumeFieldValue(num, typ, s.raw[start:])
		if k < 0 {
			return s.fail(protowire.ParseError(k))
		}
		s.cursor = start + k
		slot := -1
		if int(num) < len(s.plan.slots) {
			slot = s.plan.slots[num] - 1
		}
		if slot >= 0 {
			if s.plan.strings[slot] && typ != protowire.BytesType {
				return s.fail(fmt.Errorf("wrong wire type for string field %d", num))
			}
			if !s.first || s.index[slot] == 0 {
				s.index[slot] = uint64(uint32(start))<<32 | uint64(uint32(s.cursor))
			}
			if s.first && slot == target {
				return nil
			}
		}
	}
	s.done = true
	return nil
}

func (s *incrementalSession) Get(fd protoreflect.FieldDescriptor) (string, error) {
	if fd.ContainingMessage() != s.plan.descriptor || fd.Kind() != protoreflect.StringKind || fd.IsList() || fd.IsMap() || fd.ContainingOneof() != nil {
		return "", fmt.Errorf("only singular non-oneof strings are supported")
	}
	if s.err != nil {
		return "", s.err
	}
	if s.index == nil {
		s.index = make([]uint64, len(s.plan.strings))
	}
	slot := s.plan.slots[fd.Number()] - 1
	if !s.done && (!s.first || s.index[slot] == 0) {
		if err := s.scan(slot); err != nil {
			return "", err
		}
	}
	x := s.index[slot]
	if x == 0 {
		return "", nil
	}
	start, end := int(uint32(x>>32)), int(uint32(x))
	b, n := protowire.ConsumeBytes(s.raw[start:end])
	if n < 0 {
		return "", s.fail(protowire.ParseError(n))
	}
	// Same immutable-input / valid-UTF8 contract as descriptor_table_*.
	return StringView(b), nil
}

func init() {
	for _, first := range []bool{false, true} {
		name := "incremental_index_last"
		if first {
			name = "incremental_index_first"
		}
		Register(Backend{Name: name, FirstWins: first, ValidUTF8Assumed: true, Note: "on-demand cursor, offsets of every known field, no query field list needed", Prepare: func(_ []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
			p, err := newIndexPlan(Descriptor)
			if err != nil {
				return nil, err
			}
			return func(raw []byte) (Session, error) { return p.session(raw, first) }, nil
		}})
	}
}
