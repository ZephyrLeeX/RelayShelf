package httpx

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

type RequestInfo struct {
	ClientIP         netip.Addr
	Scheme, Host     string
	FromTrustedProxy bool
}

type Resolver struct{ trusted []netip.Prefix }

func NewResolver(trusted []netip.Prefix) *Resolver {
	return &Resolver{trusted: append([]netip.Prefix(nil), trusted...)}
}
func (r *Resolver) trustedAddr(value netip.Addr) bool {
	for _, p := range r.trusted {
		if p.Contains(value) {
			return true
		}
	}
	return false
}
func remoteIP(remote string) (netip.Addr, error) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	return netip.ParseAddr(strings.Trim(host, "[]"))
}
func (r *Resolver) Resolve(req *http.Request) (RequestInfo, error) {
	remote, err := remoteIP(req.RemoteAddr)
	if err != nil {
		return RequestInfo{}, errors.New("invalid remote address")
	}
	info := RequestInfo{ClientIP: remote, Host: req.Host}
	if req.TLS != nil {
		info.Scheme = "https"
	} else {
		info.Scheme = "http"
	}
	if !r.trustedAddr(remote) {
		return info, nil
	}
	info.FromTrustedProxy = true
	if raw := req.Header.Get("X-Forwarded-For"); raw != "" {
		parts := strings.Split(raw, ",")
		chain := make([]netip.Addr, 0, len(parts))
		for _, part := range parts {
			ip, parseErr := netip.ParseAddr(strings.TrimSpace(part))
			if parseErr != nil {
				return RequestInfo{}, errors.New("invalid forwarded client address")
			}
			chain = append(chain, ip)
		}
		// Walk from the peer-facing end. Trusted hops are discarded and the
		// first untrusted hop is the client. This prevents a caller-supplied
		// leftmost value from overriding the address appended by our proxy.
		info.ClientIP = chain[len(chain)-1]
		for index := len(chain) - 1; index >= 0; index-- {
			info.ClientIP = chain[index]
			if !r.trustedAddr(chain[index]) {
				break
			}
		}
	}
	if raw := req.Header.Get("X-Forwarded-Proto"); raw != "" {
		if raw != "http" && raw != "https" {
			return RequestInfo{}, errors.New("invalid forwarded proto")
		}
		info.Scheme = raw
	}
	if raw := req.Header.Get("X-Forwarded-Host"); raw != "" {
		if strings.Contains(raw, ",") {
			return RequestInfo{}, errors.New("invalid forwarded host")
		}
		parsed, err := url.Parse("//" + raw)
		if err != nil || parsed.Host != raw || parsed.User != nil || parsed.Path != "" {
			return RequestInfo{}, errors.New("invalid forwarded host")
		}
		info.Host = raw
	}
	return info, nil
}
