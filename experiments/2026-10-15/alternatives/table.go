//go:build all || table

package lab

import (
	"fmt"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// The dispatch table is compiled from existing field descriptors once.
// This path needs no resource-specific generated code.
func prepareTable(fields []protoreflect.FieldDescriptor, first, kernel bool) func([]byte) (Session, error) {
	max := 0
	for _, f := range fields {
		if int(f.Number()) > max {
			max = int(f.Number())
		}
	}
	lut := make([]int8, max+1)
	for i, f := range fields {
		lut[int(f.Number())] = int8(i + 1)
	}
	all := uint32(1<<len(fields)) - 1
	return func(raw []byte) (Session, error) {
		s := &planSession{raw: raw, fields: fields}
		var seen uint32
		if kernel {
			for p := 0; p < len(raw); {
				tag, start, n, next := kernelRecord(raw, p)
				if next < 0 {
					return nil, fmt.Errorf("invalid wire")
				}
				p = next
				idx := -1
				if tag>>3 < uint64(len(lut)) {
					idx = int(lut[tag>>3]) - 1
				}
				if idx < 0 || tag&7 != 2 || (first && seen&(1<<idx) != 0) {
					continue
				}
				s.values[idx] = StringView(raw[start : start+n])
				seen |= 1 << idx
				if first && seen == all {
					break
				}
			}
			return s, nil
		}
		for d := raw; len(d) > 0; {
			num, typ, n := protowire.ConsumeTag(d)
			if n < 0 {
				return nil, protowire.ParseError(n)
			}
			d = d[n:]
			idx := -1
			if int(num) < len(lut) {
				idx = int(lut[num]) - 1
			}
			if idx >= 0 && typ == protowire.BytesType && (!first || seen&(1<<idx) == 0) {
				x, n := protowire.ConsumeBytes(d)
				if n < 0 {
					return nil, protowire.ParseError(n)
				}
				s.values[idx] = StringView(x)
				seen |= 1 << idx
				d = d[n:]
				if first && seen == all {
					break
				}
			} else {
				n := protowire.ConsumeFieldValue(num, typ, d)
				if n < 0 {
					return nil, protowire.ParseError(n)
				}
				d = d[n:]
			}
		}
		return s, nil
	}
}
func init() {
	for _, kernel := range []bool{false, true} {
		for _, first := range []bool{false, true} {
			name := "descriptor_table_last"
			if first {
				name = "descriptor_table_first"
			}
			if kernel {
				name += "_kernel"
			}
			Register(Backend{Name: name, FirstWins: first, ValidUTF8Assumed: true, Prepare: func(f []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
				for _, fd := range f {
					if fd.Number() > 4096 {
						return nil, fmt.Errorf("dense dispatch table limited to field 4096")
					}
				}
				return prepareTable(f, first, kernel), nil
			}, Note: "runtime descriptor to number-index lookup table, no resource codegen"})
		}
	}
}
