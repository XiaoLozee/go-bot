//go:build windows

package config

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	moveFileReplaceExisting = 0x1
	moveFileWriteThrough    = 0x8
)

func atomicReplaceFile(srcPath, dstPath string) error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	moveFileExW := kernel32.NewProc("MoveFileExW")

	srcPtr, err := syscall.UTF16PtrFromString(srcPath)
	if err != nil {
		return fmt.Errorf("转换源路径失败: %w", err)
	}
	dstPtr, err := syscall.UTF16PtrFromString(dstPath)
	if err != nil {
		return fmt.Errorf("转换目标路径失败: %w", err)
	}

	ret, _, callErr := moveFileExW.Call(
		uintptr(unsafe.Pointer(srcPtr)),
		uintptr(unsafe.Pointer(dstPtr)),
		uintptr(moveFileReplaceExisting|moveFileWriteThrough),
	)
	if ret == 0 {
		if callErr != syscall.Errno(0) {
			return callErr
		}
		return fmt.Errorf("MoveFileExW 返回失败")
	}
	return nil
}
