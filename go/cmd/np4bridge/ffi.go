package main

// This file provides Go-callable wrappers over the exported C symbols. It
// exists for two reasons: the go tool does not allow `import "C"` in _test.go
// files, so tests exercise the real FFI path through these; and any Go-side
// embedder (e.g. a desktop host app) can use them instead of raw FFI.
//
// The wrappers deliberately round-trip through the C entry points (malloc'd
// envelopes, np4_free) rather than calling the Manager directly — they test
// the exact bytes a foreign caller would see.

import (
	/*
		#include <stdlib.h>
	*/
	"C"
	"unsafe"
)

func ffiCreate(configJSON []byte) []byte {
	c := C.CString(string(configJSON))
	defer C.free(unsafe.Pointer(c))
	p := np4_create(c, C.int(len(configJSON)))
	defer np4_free(p)
	return []byte(C.GoString(p))
}

func ffiCall(handle int64, requestJSON []byte) []byte {
	c := C.CString(string(requestJSON))
	defer C.free(unsafe.Pointer(c))
	p := np4_call(C.longlong(handle), c, C.int(len(requestJSON)))
	defer np4_free(p)
	return []byte(C.GoString(p))
}

func ffiStop(handle int64) {
	np4_stop(C.longlong(handle))
}
