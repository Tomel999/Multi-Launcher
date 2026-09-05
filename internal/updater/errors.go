package updater

import (
	"errors"
	"fmt"
	"net/http"
)

// Kind classifies an updater failure so callers (and the UI) can react
// differently to "GitHub is unreachable" versus "this release has no build
// for your platform".
type Kind string

const (
	// KindNetwork covers transport-level failures: DNS, TLS, connection
	// refused, timeouts, cancelled contexts.
	KindNetwork Kind = "network"
	// KindHTTP covers any non-2xx response that is not specifically a rate
	// limit or a missing resource.
	KindHTTP Kind = "http"
	// KindRateLimit covers HTTP 403/429 responses, which GitHub uses when the
	// unauthenticated request budget is exhausted.
	KindRateLimit Kind = "rate_limit"
	// KindNotFound covers HTTP 404 (no release published yet).
	KindNotFound Kind = "not_found"
	// KindDecode covers malformed JSON or an unexpected payload shape.
	KindDecode Kind = "decode"
	// KindNoAsset covers "the platform has no matching release asset".
	KindNoAsset Kind = "no_asset"
	// KindChecksum covers a missing or mismatching .sha256 sidecar.
	KindChecksum Kind = "checksum"
	// KindInstall covers failures while replacing or relaunching the binary.
	KindInstall Kind = "install"
	// KindManaged covers a refused self-install: the app is owned by a system
	// package manager (AUR, Flatpak, Snap, Homebrew...) and must be updated
	// through it instead.
	KindManaged Kind = "managed"
)

// Error is the typed error returned by every updater operation. It carries a
// machine-readable Kind so the frontend can pick a sensible message, plus an
// optional HTTP status and wrapped cause.
type Error struct {
	Kind    Kind   `json:"kind"`
	Status  int    `json:"status,omitempty"`
	Message string `json:"message"`
	Err     error  `json:"-"`
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	if e.Status != 0 {
		return fmt.Sprintf("%s: %s (HTTP %d)", e.Kind, e.Message, e.Status)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

// NewError builds a typed updater error.
func NewError(kind Kind, msg string) *Error {
	return &Error{Kind: kind, Message: msg}
}

// WrapError builds a typed updater error that retains its cause.
func WrapError(kind Kind, msg string, err error) *Error {
	return &Error{Kind: kind, Message: msg, Err: err}
}

// WithStatus attaches an HTTP status code to a typed error.
func (e *Error) WithStatus(code int) *Error {
	e.Status = code
	return e
}

// IsKind reports whether err is an updater *Error of the given kind. It
// returns false for nil and for non-updater errors.
func IsKind(err error, kind Kind) bool {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind == kind
	}
	return false
}

// IsNotFound reports whether err means "there is nothing to update to",
// which callers should treat as a silent no-op rather than a user-visible
// failure.
func IsNotFound(err error) bool {
	return IsKind(err, KindNotFound) || IsKind(err, KindNoAsset)
}

// httpKindFor maps an HTTP status code to the most appropriate error kind.
func httpKindFor(status int) Kind {
	switch status {
	case http.StatusNotFound:
		return KindNotFound
	case http.StatusForbidden, http.StatusTooManyRequests:
		return KindRateLimit
	default:
		return KindHTTP
	}
}
