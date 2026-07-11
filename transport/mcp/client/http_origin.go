package mcpclient

import (
	"net/url"
	"strings"
)

type httpOrigin struct {
	scheme string
	host   string
	port   string
	valid  bool
}

func parseHTTPOrigin(rawURL string) httpOrigin {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return httpOrigin{}
	}
	return originFromURL(parsed)
}

func originFromURL(parsed *url.URL) httpOrigin {
	if parsed == nil {
		return httpOrigin{}
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if host == "" || (scheme != "http" && scheme != "https") {
		return httpOrigin{}
	}
	port := parsed.Port()
	if port == "" {
		if scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	return httpOrigin{scheme: scheme, host: host, port: port, valid: true}
}

func (o httpOrigin) matches(candidate *url.URL) bool {
	return o.valid && o == originFromURL(candidate)
}
