package httpx

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

type clientIPKey struct{}

// ClientIP returns a validated proxy-derived IP, or the direct socket peer.
func ClientIP(req *http.Request) string {
	if ip, ok := req.Context().Value(clientIPKey{}).(string); ok {
		return ip
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		host = req.RemoteAddr
	}
	return host
}

// TrustedProxies accepts forwarded IPs only from explicitly configured peers.
func TrustedProxies(proxies []string) (func(http.Handler) http.Handler, error) {
	prefixes := make([]netip.Prefix, 0, len(proxies))
	for _, value := range proxies {
		value = strings.TrimSpace(value)
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			ip, ipErr := netip.ParseAddr(value)
			if ipErr != nil {
				return nil, fmt.Errorf("invalid trusted proxy %q", value)
			}
			prefix = netip.PrefixFrom(ip, ip.BitLen())
		}
		prefixes = append(prefixes, prefix)
	}
	trusted := func(ip netip.Addr) bool {
		for _, prefix := range prefixes {
			if prefix.Contains(ip) || prefix.Contains(ip.Unmap()) {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			peer := ClientIP(req)
			ip, err := netip.ParseAddr(peer)
			if err == nil && trusted(ip) {
				forwarded := strings.Split(req.Header.Get("X-Forwarded-For"), ",")
				valid := len(forwarded) <= 64
				addresses := make([]netip.Addr, 0, len(forwarded))
				for _, value := range forwarded {
					address, err := netip.ParseAddr(strings.TrimSpace(value))
					if err != nil {
						valid = false
						break
					}
					addresses = append(addresses, address)
				}
				if valid && len(addresses) > 0 {
					for i := len(addresses) - 1; i >= 0; i-- {
						peer = addresses[i].String()
						if !trusted(addresses[i]) {
							break
						}
					}
				} else if real, err := netip.ParseAddr(strings.TrimSpace(req.Header.Get("X-Real-IP"))); err == nil {
					peer = real.String()
				}
			}
			next.ServeHTTP(w, req.WithContext(context.WithValue(req.Context(), clientIPKey{}, peer)))
		})
	}, nil
}
