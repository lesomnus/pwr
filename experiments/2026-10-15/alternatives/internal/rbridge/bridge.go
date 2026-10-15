package rbridge

/*
#cgo CFLAGS: -O3
#cgo noescape rust_scan
#cgo nocallback rust_scan
#cgo noescape rust_project
#cgo nocallback rust_project
#cgo noescape rust_batch
#cgo nocallback rust_batch
#cgo noescape rust_noop
#cgo nocallback rust_noop
#cgo LDFLAGS: ${SRCDIR}/libreader.a -ldl -lm
#include "bridge.h"
*/
import "C"
import (
	"fmt"
	"runtime"
	"unsafe"
)

func Noop() { C.rust_noop() }
func Single(raw []byte, field uint32, first bool) (int, int, error) {
	if len(raw) == 0 {
		return 0, 0, nil
	}
	var out C.rust_span
	mode := C.int(0)
	if first {
		mode = 1
	}
	if C.rust_scan((*C.uchar)(unsafe.Pointer(unsafe.SliceData(raw))), C.size_t(len(raw)), C.uint32_t(field), mode, &out) != 0 {
		return 0, 0, fmt.Errorf("invalid wire")
	}
	runtime.KeepAlive(raw)
	return int(out.offset), int(out.length), nil
}
func Project(raw []byte, mask uint32, first bool) ([4][2]int, error) {
	if len(raw) == 0 {
		return [4][2]int{}, nil
	}
	var out [4]C.rust_span
	mode := C.int(0)
	if first {
		mode = 1
	}
	if C.rust_project((*C.uchar)(unsafe.Pointer(unsafe.SliceData(raw))), C.size_t(len(raw)), C.uint32_t(mask), mode, &out[0]) != 0 {
		return [4][2]int{}, fmt.Errorf("invalid wire")
	}
	runtime.KeepAlive(raw)
	var res [4][2]int
	for i, v := range out {
		res[i] = [2]int{int(v.offset), int(v.length)}
	}
	return res, nil
}
func Batch(raw []byte, offsets []uintptr, mask uint32, first bool, out []uintptr) error {
	mode := C.int(0)
	if first {
		mode = 1
	}
	if len(raw) == 0 {
		for _, v := range offsets {
			if v != 0 {
				return fmt.Errorf("invalid empty batch")
			}
		}
		clear(out)
		return nil
	}
	if len(offsets) < 2 || len(out) < (len(offsets)-1)*8 {
		return fmt.Errorf("invalid batch")
	}
	if C.rust_batch((*C.uchar)(unsafe.Pointer(unsafe.SliceData(raw))), (*C.size_t)(unsafe.Pointer(unsafe.SliceData(offsets))), C.size_t(len(offsets)-1), C.uint32_t(mask), mode, (*C.rust_span)(unsafe.Pointer(unsafe.SliceData(out)))) != 0 {
		return fmt.Errorf("invalid wire")
	}
	runtime.KeepAlive(raw)
	runtime.KeepAlive(offsets)
	runtime.KeepAlive(out)
	return nil
}
