//go:build !windows

package config

import "os"

func atomicReplaceFile(srcPath, dstPath string) error {
	return os.Rename(srcPath, dstPath)
}
