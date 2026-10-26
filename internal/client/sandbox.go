package client

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

// DevTransport guards every request, including redirects and signed uploads.
// Base defaults to http.DefaultTransport. Errors never expose signed URLs.
type DevTransport struct {
	Domain string
	Base   http.RoundTripper
}

func (g DevTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil || req.URL == nil || req.URL.Scheme != "https" || req.URL.User != nil {
		return nil, errors.New("sandbox transport refused request outside the configured HTTPS development domain")
	}
	host := strings.ToLower(req.URL.Hostname())
	if !insideDevDomain(host, g.Domain) || host == productionHost {
		return nil, errors.New("sandbox transport refused request outside the configured HTTPS development domain")
	}
	base := g.Base
	if base == nil {
		base = http.DefaultTransport
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		return resp, errors.New("development request failed")
	}
	return resp, nil
}

// insideDevDomain compares DNS labels, not arbitrary hostname substrings.
func insideDevDomain(host, domain string) bool {
	domain = strings.ToLower(domain)
	if domain == "" || len(domain) > 253 || net.ParseIP(domain) != nil {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return host == domain || strings.HasSuffix(host, "."+domain)
}
