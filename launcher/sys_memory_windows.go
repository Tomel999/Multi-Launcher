//go:build windows

package launcher

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var kernel32Mem = windows.NewLazySystemDLL("kernel32.dll")
var procGlobalMemoryStatusEx = kernel32Mem.NewProc("GlobalMemoryStatusEx")

type memStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// SystemMemoryGB returns the total physical RAM in whole GB.
func SystemMemoryGB() int {
	ms := new(memStatusEx)
	ms.Length = uint32(unsafe.Sizeof(*ms))
	r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(ms)))
	if r == 0 || ms.TotalPhys <= 0 {
		return 16
	}
	gb := int(ms.TotalPhys / (1024 * 1024 * 1024))
	if gb < 1 {
		gb = 1
	}
	return gb
}