//go:build all || kernel

package lab

import (
	"fmt"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// This reader uses the same limited wire-type contract as the C/Rust kernels.
// It supports 0,1,2,5, aliases strings, and assumes UTF-8 validity.
func KernelField(raw []byte, field int, first bool) (string, error) {
	var value string
	for p := 0; p < len(raw); {
		tag, start, n, next := kernelRecord(raw, p)
		if next < 0 {
			return "", fmt.Errorf("invalid wire")
		}
		p = next
		if tag>>3 == uint64(field) && tag&7 == 2 {
			value = StringView(raw[start : start+n])
			if first {
				break
			}
		}
	}
	return value, nil
}

//go:generate python3 generate_kernel.py
func KernelProject(raw []byte, mask uint32, first bool) ([4]string, error) {
	var values [4]string
	var seen uint32
	for p := 0; p < len(raw); {
		tag, start, n, next := kernelRecord(raw, p)
		if next < 0 {
			return values, fmt.Errorf("invalid wire")
		}
		p = next
		idx := kernelIndex(tag >> 3)
		if idx < 0 || tag&7 != 2 || mask&(1<<idx) == 0 || (first && seen&(1<<idx) != 0) {
			continue
		}
		values[idx] = StringView(raw[start : start+n])
		seen |= 1 << idx
		if first && seen == mask {
			break
		}
	}
	return values, nil
}

type kernelSession struct {
	raw   []byte
	first bool
}

func (s *kernelSession) Close() {}
func (s *kernelSession) Get(fd protoreflect.FieldDescriptor) (string, error) {
	return KernelField(s.raw, int(fd.Number()), s.first)
}
func init() {
	for _, first := range []bool{false, true} {
		suffix := "last"
		if first {
			suffix = "first"
		}
		Register(Backend{Name: "go_kernel_field_" + suffix, FirstWins: first, ValidUTF8Assumed: true, Prepare: func(_ []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
			return func(raw []byte) (Session, error) { return &kernelSession{raw, first}, nil }, nil
		}})
		Register(Backend{Name: "go_kernel_project_" + suffix, FirstWins: first, ValidUTF8Assumed: true, Prepare: func(fields []protoreflect.FieldDescriptor) (func([]byte) (Session, error), error) {
			mask := uint32(1<<len(fields)) - 1
			return func(raw []byte) (Session, error) {
				values, e := KernelProject(raw, mask, first)
				return &planSession{raw, values, fields}, e
			}, nil
		}})
	}
}
