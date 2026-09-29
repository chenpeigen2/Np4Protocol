// Command np4bridge is the native library build target for GUI clients
// (currently the Flutter app). It is not a runnable binary: build it with
//
//	go build -buildmode=c-shared -o libnp4bridge.so ./cmd/np4bridge/
//
// (or c-archive / dylib per platform — see clients/flutter/tool/build_native.sh).
//
// The exported ABI is four symbols and will not grow:
//
//	char* np4_create(const char* config_json, int len)
//	char* np4_call(long long handle, const char* request_json, int len)
//	void  np4_stop(long long handle)
//	void  np4_free(char* p)
//
// Every returned char* is a NUL-terminated JSON envelope allocated by Go and
// owned by the caller: {"ok":true,"result":{...}} or {"ok":false,"error":"..."}.
// The caller must release it with np4_free. Content bytes travel base64-encoded
// inside the JSON.
package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	"Np4Protocol/go/pkg/bridge"
)

func respond(b []byte) *C.char {
	c := C.CString(string(b))
	return c
}

//export np4_create
func np4_create(cfg *C.char, n C.int) *C.char {
	result, err := bridge.Default.Create(C.GoBytes(unsafe.Pointer(cfg), n))
	return respond(bridge.Envelope(result, err))
}

//export np4_call
func np4_call(h C.longlong, req *C.char, n C.int) *C.char {
	result, err := bridge.Default.Call(int64(h), C.GoBytes(unsafe.Pointer(req), n))
	return respond(bridge.Envelope(result, err))
}

//export np4_stop
func np4_stop(h C.longlong) {
	_ = bridge.Default.Stop(int64(h))
}

//export np4_free
func np4_free(p *C.char) {
	C.free(unsafe.Pointer(p))
}

func main() {}
