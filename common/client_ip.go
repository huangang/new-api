package common

import (
	"net"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

const realClientIPKey = "real_client_ip"

var (
	trustedProxyCIDRs []*net.IPNet
	trustedProxyOnce  sync.Once
	trustedProxyList  []string
)

// RealClientIP returns the best-effort public client IP and caches it on the request context.
func RealClientIP(c *gin.Context) string {
	if cached, ok := c.Get(realClientIPKey); ok {
		if ip, ok := cached.(string); ok && ip != "" {
			return ip
		}
	}
	ip := RealClientIPFromRequest(c.Request)
	if ip != "" {
		c.Set(realClientIPKey, ip)
		return ip
	}
	return c.ClientIP()
}

// RealClientIPFromRequest extracts the client IP from common proxy headers,
// falling back to the remote address.
func RealClientIPFromRequest(req *http.Request) string {
	if req == nil {
		return ""
	}
	remoteIP := parseRemoteIP(req.RemoteAddr)
	if remoteIP != nil && isTrustedProxyIP(remoteIP) {
		for _, header := range []string{"X-Forwarded-For", "X-Real-IP", "CF-Connecting-IP"} {
			if ip := pickForwardedIP(req.Header.Get(header)); ip != "" {
				return ip
			}
		}
	}
	if remoteIP != nil {
		return remoteIP.String()
	}
	return ""
}

// TrustedProxies returns the configured proxy CIDRs for gin.SetTrustedProxies.
func TrustedProxies() []string {
	loadTrustedProxies()
	return append([]string(nil), trustedProxyList...)
}

func parseRemoteIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	if host == "" {
		return nil
	}
	return net.ParseIP(host)
}

func pickForwardedIP(headerVal string) string {
	if headerVal == "" {
		return ""
	}
	var firstValid string
	for _, part := range strings.Split(headerVal, ",") {
		ipStr := strings.TrimSpace(part)
		if ipStr == "" {
			continue
		}
		ip := net.ParseIP(ipStr)
		if ip == nil {
			continue
		}
		if firstValid == "" {
			firstValid = ipStr
		}
		if !IsPrivateIP(ip) {
			return ipStr
		}
	}
	return firstValid
}

func isTrustedProxyIP(ip net.IP) bool {
	loadTrustedProxies()
	for _, cidr := range trustedProxyCIDRs {
		if cidr.Contains(ip) {
			return true
		}
	}
	return false
}

func loadTrustedProxies() {
	trustedProxyOnce.Do(func() {
		proxiesEnv := os.Getenv("TRUSTED_PROXIES")
		trustedProxyList = splitProxyList(proxiesEnv)
		if len(trustedProxyList) == 0 {
			trustedProxyList = []string{"0.0.0.0/0", "::/0"}
		}
		trustedProxyCIDRs = parseProxyCIDRs(trustedProxyList)
		if len(trustedProxyCIDRs) == 0 {
			trustedProxyList = []string{"0.0.0.0/0", "::/0"}
			trustedProxyCIDRs = parseProxyCIDRs(trustedProxyList)
		}
	})
}

func splitProxyList(raw string) []string {
	splitFunc := func(r rune) bool {
		return r == ',' || r == ';'
	}
	parts := strings.FieldsFunc(raw, splitFunc)
	list := make([]string, 0, len(parts))
	for _, part := range parts {
		if v := strings.TrimSpace(part); v != "" {
			list = append(list, v)
		}
	}
	return list
}

func parseProxyCIDRs(list []string) []*net.IPNet {
	cidrs := make([]*net.IPNet, 0, len(list))
	for _, proxy := range list {
		p := proxy
		if !strings.Contains(p, "/") {
			ip := net.ParseIP(p)
			if ip == nil {
				SysLog("invalid trusted proxy ip: " + proxy)
				continue
			}
			if ip.To4() != nil {
				p += "/32"
			} else {
				p += "/128"
			}
		}
		_, cidr, err := net.ParseCIDR(p)
		if err != nil {
			SysLog("invalid trusted proxy cidr: " + proxy + ", err: " + err.Error())
			continue
		}
		cidrs = append(cidrs, cidr)
	}
	return cidrs
}
