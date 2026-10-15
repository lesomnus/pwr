package pwr

import (
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
)

type record struct {
	number                           protowire.Number
	wire                             protowire.Type
	start, end, valueStart, valueEnd int
	data                             []byte
}

func (r *Reader) failWire(err error) error {
	r.err = fmt.Errorf("%w at byte %d: %v", ErrInvalidWire, r.cursor, err)
	return r.err
}
func (r *Reader) readRecord(pos int) (record, error) {
	rec := record{start: pos}
	b := r.at(pos)
	num, typ, n := protowire.ConsumeTag(b)
	if n < 0 {
		return rec, r.failWire(protowire.ParseError(n))
	}
	if !num.IsValid() {
		return rec, r.failWire(fmt.Errorf("invalid field number %d", num))
	}
	rec.number, rec.wire = num, typ
	rec.valueStart = pos + n
	var k int
	switch typ {
	case protowire.BytesType:
		v, m := protowire.ConsumeBytes(b[n:])
		k = m
		if m >= 0 {
			rec.data = v
			rec.valueStart = pos + n + m - len(v)
			rec.valueEnd = pos + n + m
		}
	case protowire.StartGroupType:
		bodyEnd, m, err := r.group(num, b[n:])
		if err != nil {
			return rec, err
		}
		k = m
		rec.data = b[n : n+bodyEnd]
		rec.valueEnd = pos + n + bodyEnd
	default:
		k = protowire.ConsumeFieldValue(num, typ, b[n:])
		if k >= 0 {
			rec.data = b[n : n+k]
			rec.valueEnd = pos + n + k
		}
	}
	if k < 0 {
		return rec, r.failWire(protowire.ParseError(k))
	}
	rec.end = pos + n + k
	if err := r.charge(n + k); err != nil {
		return rec, err
	}
	return rec, nil
}

// group uses an explicit stack, including for unknown groups. No Go recursion
// is needed to skip an adversarial group nesting chain.
func (r *Reader) group(number protowire.Number, b []byte) (int, int, error) {
	stack := []protowire.Number{number}
	if r.depth+len(stack) > r.limits.MaxDepth {
		return 0, 0, r.failLimit("group depth")
	}
	for p := 0; p < len(b); {
		start := p
		num, typ, n := protowire.ConsumeTag(b[p:])
		if n < 0 {
			return 0, 0, r.failWire(protowire.ParseError(n))
		}
		if !num.IsValid() {
			return 0, 0, r.failWire(fmt.Errorf("invalid group field %d", num))
		}
		p += n
		if typ == protowire.EndGroupType {
			if num != stack[len(stack)-1] {
				return 0, 0, r.failWire(fmt.Errorf("mismatched group end"))
			}
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return start, p, nil
			}
			continue
		}
		if typ == protowire.StartGroupType {
			stack = append(stack, num)
			if r.depth+len(stack) > r.limits.MaxDepth {
				return 0, 0, r.failLimit("group depth")
			}
		} else {
			k := protowire.ConsumeFieldValue(num, typ, b[p:])
			if k < 0 {
				return 0, 0, r.failWire(protowire.ParseError(k))
			}
			p += k
		}
		if err := r.charge(p - start); err != nil {
			return 0, 0, err
		}
	}
	return 0, 0, r.failWire(fmt.Errorf("unterminated group"))
}

func (r *Reader) scan(target int, whole bool) error {
	for r.cursor < r.size {
		rec, err := r.readRecord(r.cursor)
		if err != nil {
			return err
		}
		r.cursor = rec.end
		slot := r.schema.slot(rec.number)
		if slot < 0 {
			continue
		}
		f := r.schema.fields[slot]
		if !f.accepts(rec.wire) {
			continue
		}
		if f.oneof != 0 {
			if r.active == nil {
				r.active = make([]int, r.schema.descriptor.Oneofs().Len())
			}
			prev := r.active[f.oneof-1] - 1
			if prev >= 0 && prev != slot {
				r.index[prev] = 0
				delete(r.cache, prev)
			}
			r.active[f.oneof-1] = slot + 1
		}
		if f.composite() {
			start := rec.start
			if r.index[slot] != 0 {
				start, _ = unpack(r.index[slot])
			}
			r.index[slot] = pack(start, rec.end)
		} else if !r.first || f.oneof != 0 || r.index[slot] == 0 {
			r.index[slot] = pack(rec.valueStart, rec.valueEnd)
		}
		if !whole && r.first && slot == target && !f.composite() && f.oneof == 0 {
			return nil
		}
	}
	r.done = true
	return nil
}

// Composite fields retain a span, not an allocation for every occurrence.
// Their records are collected only when the composite is actually requested.
func (r *Reader) records(slot int, visit func(record) error) error {
	if r.index[slot] == 0 {
		return nil
	}
	start, end := unpack(r.index[slot])
	f := r.schema.fields[slot]
	for pos := start; pos < end; {
		rec, err := r.readRecord(pos)
		if err != nil {
			return err
		}
		pos = rec.end
		if rec.number == f.descriptor.Number() && f.accepts(rec.wire) {
			if err = visit(rec); err != nil {
				return err
			}
		}
	}
	return nil
}
