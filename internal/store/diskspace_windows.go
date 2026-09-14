//go:build windows

package store

import (
	"syscall"
	"unsafe"
)

var getDiskFreeSpaceEx = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")

// availableBytes reports how many bytes a non-privileged process can still
// write to the volume holding path. GetDiskFreeSpaceExW answers per user rather
// than per volume, which is the number a quota-bound account can actually use.
func availableBytes(path string) (uint64, error) {
	utf16Path, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeForCaller, totalBytes, totalFree uint64
	ret, _, callErr := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(utf16Path)),
		uintptr(unsafe.Pointer(&freeForCaller)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if ret == 0 {
		return 0, callErr
	}
	return freeForCaller, nil
}
