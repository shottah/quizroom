package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Guard passes a request on only when it's addressed to this computer and no
// other website sent it. Quizroom has no login, so these are what keep a page
// in your browser from using your copy:
//   - The Host check stops DNS rebinding, where a page points its own domain
//     at 127.0.0.1 so the browser treats Quizroom as part of that site.
//   - The Sec-Fetch-Site and Origin checks stop cross-site requests, such as a
//     form on another site starting a Claude job. Clients that send neither
//     header, like curl or an AI agent, pass.
//
// extra lists host names to accept besides localhost and 127.0.0.1.
func Guard(next http.Handler, extra []string) http.Handler {
	allowed := map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
	for _, h := range extra {
		allowed[h] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h := hostname(r.Host); !allowed[h] {
			refuse(w, fmt.Sprintf("Quizroom only answers requests addressed to localhost or 127.0.0.1, not %q. To use another name, add it to ALLOWED_HOSTS.", h))
			return
		}
		switch r.Header.Get("Sec-Fetch-Site") {
		case "", "same-origin", "none": // no browser marking, the app itself, or the address bar
		default:
			refuse(w, "Quizroom refuses requests sent by other websites.")
			return
		}
		// Browsers too old for Sec-Fetch-Site still send Origin.
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || !allowed[hostname(u.Host)] {
				refuse(w, "Quizroom refuses requests sent by other websites.")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Hosts parses ALLOWED_HOSTS: comma-separated host names, without ports.
func Hosts(list string) []string {
	var hs []string
	for _, h := range strings.Split(list, ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			hs = append(hs, h)
		}
	}
	return hs
}

// hostname is a Host header or URL host without its port, lowercased.
func hostname(hostport string) string {
	h := hostport
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		h = host
	}
	return strings.ToLower(strings.Trim(h, "[]"))
}

// refuse answers 403 in the API's usual problem format.
func refuse(w http.ResponseWriter, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{"title": "Forbidden", "status": http.StatusForbidden, "detail": detail})
}
