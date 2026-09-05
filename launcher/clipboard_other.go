//go:build !windows

package launcher

import "fmt"

func CopyPNGToClipboard(path string) error {
	return fmt.Errorf("clipboard copy not supported on this platform")
}
