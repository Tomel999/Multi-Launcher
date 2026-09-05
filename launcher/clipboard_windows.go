//go:build windows

package launcher

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procSetClipboardData = user32.NewProc("SetClipboardData")
	procCloseClipboard   = user32.NewProc("CloseClipboard")

	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
)

const (
	cfDIB        = 8
	gmemMoveable = 0x0002
)

func CopyPNGToClipboard(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return fmt.Errorf("decode png: %w", err)
	}
	dib := pngToDIB(img)

	r, _, err := procOpenClipboard.Call(0)
	if r == 0 {
		return fmt.Errorf("OpenClipboard: %v", err)
	}
	defer procCloseClipboard.Call()

	r, _, err = procEmptyClipboard.Call()
	if r == 0 {
		return fmt.Errorf("EmptyClipboard: %v", err)
	}

	h, _, err := procGlobalAlloc.Call(gmemMoveable, uintptr(len(dib)))
	if h == 0 {
		return fmt.Errorf("GlobalAlloc: %v", err)
	}
	ptr, _, lerr := procGlobalLock.Call(h)
	if ptr == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("GlobalLock: %v", lerr)
	}
	dst := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(dib))
	copy(dst, dib)
	procGlobalUnlock.Call(h)

	r, _, serr := procSetClipboardData.Call(cfDIB, h)
	if r == 0 {
		procGlobalFree.Call(h)
		return fmt.Errorf("SetClipboardData: %v", serr)
	}
	return nil
}

func pngToDIB(img image.Image) []byte {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	headerSize := 40
	rowSize := w * 4
	padding := (4 - rowSize%4) % 4
	buf := make([]byte, headerSize+(rowSize+padding)*h)

	binary.LittleEndian.PutUint32(buf[0:], uint32(headerSize))
	binary.LittleEndian.PutUint32(buf[4:], uint32(w))
	binary.LittleEndian.PutUint32(buf[8:], uint32(h))
	binary.LittleEndian.PutUint16(buf[12:], 1)
	binary.LittleEndian.PutUint16(buf[14:], 32)

	off := headerSize
	for y := h - 1; y >= 0; y-- {
		row := off
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			buf[row+x*4+0] = byte(bl >> 8)
			buf[row+x*4+1] = byte(g >> 8)
			buf[row+x*4+2] = byte(r >> 8)
			buf[row+x*4+3] = 0xFF
		}
		off += rowSize + padding
	}
	return buf
}
