package updater

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// progressBytes is the minimum number of bytes between progress callbacks.
	progressBytes = 256 << 10 // 256 KiB
	// progressInterval is the maximum wall-clock gap between progress
	// callbacks, so a slow connection still updates the UI.
	progressInterval = 200 * time.Millisecond
	// copyBufferSize is the per-read chunk size for the download loop.
	copyBufferSize = 32 << 10 // 32 KiB
	// checksumMaxBytes bounds a .sha256 sidecar file. A real one is ~100 bytes;
	// the cap stops a mislabelled binary from being pulled into memory.
	checksumMaxBytes = 4 << 10 // 4 KiB
)

// Download streams a release asset to dest, verifying its SHA-256 against the
// matching "<asset>.sha256" sidecar before the file is put in place.
//
// The file is written to a temporary sibling and renamed on success, so a
// failed or aborted download never leaves a partial file at dest and never
// damages an existing installation.
//
// onProgress is called periodically with bytes downloaded and the total size
// (0 when the server does not report one). It may be called from a goroutine
// other than the caller's, so it must be safe for concurrent use and must not
// block; pass nil to opt out.
func (u *Updater) Download(ctx context.Context, asset Asset, dest string, onProgress func(downloaded, total int64)) error {
	if asset.URL == "" {
		return NewError(KindNetwork, fmt.Sprintf("asset %q has no download URL", asset.Name))
	}
	if u.client == nil {
		return NewError(KindNetwork, "updater has no GitHub client configured")
	}
	dest = filepath.Clean(dest)
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return WrapError(KindInstall, "cannot create update directory", err)
	}

	// Stage next to the destination so the final rename stays on one
	// filesystem (atomic, and required for replacing a running binary on
	// Linux).
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(dest)+".*.part")
	if err != nil {
		return WrapError(KindInstall, "cannot create temporary update file", err)
	}
	tmpName := tmp.Name()

	// cleanup removes the staging file; it is a no-op once the rename
	// succeeded because we blank the name out.
	cleanup := func() {
		if tmpName != "" {
			_ = os.Remove(tmpName)
		}
	}

	// ---- stream the asset -------------------------------------------------
	body, err := u.client.FetchAsset(ctx, asset)
	if err != nil {
		tmp.Close()
		cleanup()
		return err
	}

	downloaded, werr := copyWithProgress(ctx, tmp, body, asset.Size, onProgress)
	body.Close()
	if cerr := tmp.Close(); cerr != nil && werr == nil {
		werr = cerr
	}
	if werr != nil {
		cleanup()
		if ctx.Err() != nil {
			return WrapError(KindNetwork, "download interrupted", ctx.Err())
		}
		return WrapError(KindNetwork, fmt.Sprintf("download of %s failed", asset.Name), werr)
	}

	// Trust the advertised size when GitHub gave us one: a truncated response
	// that still terminated cleanly must not be installed.
	if asset.Size > 0 && downloaded != asset.Size {
		cleanup()
		return NewError(KindNetwork, fmt.Sprintf(
			"%s is truncated: got %d bytes, expected %d", asset.Name, downloaded, asset.Size))
	}

	// ---- verify -----------------------------------------------------------
	if err := u.verify(ctx, asset, tmpName, downloaded); err != nil {
		cleanup()
		return err
	}

	// ---- publish ----------------------------------------------------------
	if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		cleanup()
		return WrapError(KindInstall, "cannot replace previous update file", err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		cleanup()
		return WrapError(KindInstall, "cannot move update into place", err)
	}
	tmpName = "" // ownership transferred to dest
	if err := os.Chmod(dest, 0o755); err != nil {
		// Non-fatal: the file is already in place, and Windows ignores this.
		_ = err
	}
	return nil
}

// copyWithProgress copies src to dst, invoking onProgress periodically. It
// returns the number of bytes written.
func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, total int64, onProgress func(int64, int64)) (int64, error) {
	buf := make([]byte, copyBufferSize)
	var written int64
	var lastBytes int64
	lastEmit := time.Now()

	emit := func(force bool) {
		if onProgress == nil {
			return
		}
		now := time.Now()
		if force || written-lastBytes >= progressBytes || now.Sub(lastEmit) >= progressInterval {
			lastBytes = written
			lastEmit = now
			onProgress(written, total)
		}
	}
	defer func() { emit(true) }()

	for {
		// Honour cancellation between chunks as well as during reads.
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			w, werr := dst.Write(buf[:n])
			written += int64(w)
			if werr != nil {
				return written, werr
			}
			if w != n {
				return written, io.ErrShortWrite
			}
			emit(false)
		}
		if rerr == io.ErrUnexpectedEOF || rerr == io.EOF {
			return written, nil
		}
		if rerr != nil {
			return written, rerr
		}
	}
}

// verify checks the staged file against the release's .sha256 sidecar.
// Verification fails closed: a missing or unreadable sidecar is an error, not
// a pass, because an unverified binary must never replace the installation.
func (u *Updater) verify(ctx context.Context, asset Asset, path string, _ int64) error {
	if u.ChecksumDisabled {
		return nil
	}
	sumAsset, err := u.checksumAsset(asset)
	if err != nil {
		return err
	}
	raw, err := u.client.FetchAssetBytes(ctx, sumAsset, checksumMaxBytes)
	if err != nil {
		// A 404 (or any transport error) on the sidecar is still a checksum
		// failure: we cannot verify, so we refuse to install.
		return WrapError(KindChecksum,
			fmt.Sprintf("cannot download the checksum file %s", sumAsset.Name), err)
	}
	want, err := ParseChecksum(raw, asset.Name)
	if err != nil {
		return err
	}
	got, err := FileSHA256(path)
	if err != nil {
		return WrapError(KindChecksum, "cannot hash the downloaded update", err)
	}
	if !strings.EqualFold(got, want) {
		return NewError(KindChecksum, fmt.Sprintf(
			"checksum mismatch for %s: release says %s but the download hashed to %s — the update was discarded",
			asset.Name, want, got))
	}
	return nil
}

// ParseChecksum extracts a SHA-256 hex digest from a checksum file. It accepts
// the common shapes of `sha256sum` output:
//
//	<hex>  filename
//	<hex> *filename
//	<hex>
//
// Anything else, or a non-hex digest, is rejected.
func ParseChecksum(data []byte, assetName string) (string, error) {
	field := strings.TrimSpace(string(data))
	if field == "" {
		return "", NewError(KindChecksum, fmt.Sprintf(
			"checksum file for %s is empty", assetName))
	}
	// Split on any whitespace and keep the first token.
	if i := strings.IndexAny(field, " \t\r\n"); i >= 0 {
		field = field[:i]
	}
	field = strings.TrimPrefix(field, "*") // binary-mode marker
	field = strings.ToLower(strings.TrimSpace(field))

	if len(field) != sha256.Size*2 {
		return "", NewError(KindChecksum, fmt.Sprintf(
			"checksum file for %s does not contain a SHA-256 digest (got %d characters, want %d)",
			assetName, len(field), sha256.Size*2))
	}
	if _, err := hex.DecodeString(field); err != nil {
		return "", NewError(KindChecksum, fmt.Sprintf(
			"checksum file for %s contains invalid hex: %v", assetName, err))
	}
	return field, nil
}

// FileSHA256 returns the lowercase hex SHA-256 digest of a file.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
