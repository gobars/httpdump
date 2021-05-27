package replay

import (
	"unsafe"
)

// SliceToString preferred for large body payload (zero allocation and faster)
func SliceToString(buf []byte) string { return *(*string)(unsafe.Pointer(&buf)) }
