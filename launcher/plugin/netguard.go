package plugin

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var hashRe = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func ParsePinnedURL(raw string) (base, sha256hex string, err error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "https" {
		return "", "", fmt.Errorf("plugin URLs must use https (got %q)", u.Scheme)
	}
	if frag := u.Fragment; strings.HasPrefix(frag, "sha256:") {
		h := strings.TrimPrefix(frag, "sha256:")
		if !hashRe.MatchString(h) {
			return "", "", fmt.Errorf("invalid sha256 fragment %q (want 64 hex chars)", h)
		}
		sha256hex = strings.ToLower(h)
		u.Fragment, u.RawFragment = "", ""
	}
	return u.String(), sha256hex, nil
}

func ipBlocked(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()
}

func CheckPublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("plugins may only talk https (got %q)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url has no host")
	}
	if host == "localhost" {
		return fmt.Errorf("access to %q is not allowed for plugins", host)
	}
	if ip := net.ParseIP(host); ip != nil && ipBlocked(ip) {
		return fmt.Errorf("access to %s is not allowed for plugins", ip)
	}
	return nil
}

func SafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return fmt.Errorf("redirect to %q blocked: plugins may only talk https", req.URL.Scheme)
			}
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return nil
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, err
				}
				var dialIP net.IP
				for _, ip := range ips {
					if ipBlocked(ip.IP) {
						continue
					}
					if dialIP == nil {
						dialIP = ip.IP
					}
				}
				if dialIP == nil {
					return nil, fmt.Errorf("access to %s is not allowed for plugins", host)
				}
				return dialer.DialContext(ctx, network, net.JoinHostPort(dialIP.String(), port))
			},
		},
	}
}
