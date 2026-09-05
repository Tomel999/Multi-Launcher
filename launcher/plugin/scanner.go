package plugin

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
)

const maxScanBytes = 8 << 20

// maxWasmBytes is the per-file scan limit for compiled .wasm plugins.
// WASM binaries (even TinyGo builds with a heavier stdlib) can legitimately
// exceed the 8 MiB limit designed for JavaScript text, but compiled code
// should not be enormous either.
const maxWasmBytes = 32 << 20

var wasmMagic = []byte{0x00, 0x61, 0x73, 0x6D}

const maxArchiveEntries = 2000

const (
	entropyThreshold = 6.0
	minEntropyBytes  = 1024
)

var obfuscationMarkers = []struct {
	name string
	re   *regexp.Regexp
}{
	{"an eval() call", regexp.MustCompile(`\beval\s*\(`)},
	{"a new Function() constructor", regexp.MustCompile(`\bnew\s+Function\s*\(`)},
	{"an atob() decoder", regexp.MustCompile(`\batob\s*\(`)},
	{"_0x-style obfuscated identifiers", regexp.MustCompile(`_0x[0-9a-fA-F]{4,}\b`)},
	{"a long base64 literal", regexp.MustCompile(`["'][A-Za-z0-9+/=]{256,}["']`)},
	{"a long hex literal", regexp.MustCompile(`["'][0-9a-fA-F]{256,}["']`)},
}

func isScriptName(name string) bool {
	l := strings.ToLower(name)
	return strings.HasSuffix(l, ".js") || strings.HasSuffix(l, ".mjs") || strings.HasSuffix(l, ".cjs")
}

func shannonEntropy(data []byte) float64 {
	var freq [256]int
	for _, b := range data {
		freq[b]++
	}
	n := float64(len(data))
	var e float64
	for _, c := range freq {
		if c == 0 {
			continue
		}
		p := float64(c) / n
		e -= p * math.Log2(p)
	}
	return e
}

// MaxFileScanLimit returns the per-file scan limit for the given archive entry name.
func MaxFileScanLimit(name string) int64 {
	if isWasmName(name) {
		return maxWasmBytes
	}
	return maxScanBytes
}

func isWasmName(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".wasm")
}

// ScanWasm validates a .wasm plugin binary: size limit and magic bytes.
// Entropy/obfuscation heuristics don't apply to compiled WASM.
func ScanWasm(name string, content []byte) error {
	if len(content) >= maxWasmBytes {
		return fmt.Errorf("%s exceeds the %d MiB wasm size limit", name, maxWasmBytes>>20)
	}
	if len(content) < 8 || !bytes.Equal(content[:4], wasmMagic) {
		return fmt.Errorf("%s is not a valid WebAssembly binary (bad magic bytes)", name)
	}
	return nil
}

func ScanScript(name string, content []byte) error {
	if isWasmName(name) {
		return ScanWasm(name, content)
	}
	if !isScriptName(name) || len(content) == 0 {
		return nil
	}
	if len(content) >= minEntropyBytes && shannonEntropy(content) > entropyThreshold {
		return fmt.Errorf("%s looks packed or encrypted (entropy %.2f bits per byte)", name, shannonEntropy(content))
	}
	for _, m := range obfuscationMarkers {
		if m.re.Match(content) {
			return fmt.Errorf("%s contains %s", name, m.name)
		}
	}
	return nil
}

func ScanArchive(zipPath string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open plugin archive: %w", err)
	}
	defer zr.Close()
	if len(zr.File) > maxArchiveEntries {
		return fmt.Errorf("archive has too many entries (%d, max %d)", len(zr.File), maxArchiveEntries)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		content, err := io.ReadAll(io.LimitReader(rc, maxScanBytes))
		rc.Close()
		if err != nil {
			return err
		}
		if err := ScanScript(f.Name, content); err != nil {
			return err
		}
	}
	return nil
}
