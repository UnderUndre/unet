package proxy

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"time"
)

// hopByHopHeaders are stripped from every forwarded HTTP message
// (interfaces §1.4, zcode F6 — RFC 7230 §6.1 plus proxy credentials).
var hopByHopHeaders = []string{
	"Proxy-Authorization",
	"Proxy-Connection",
	"Proxy-Authenticate",
	"Connection",
	"Te",
	"Trailer",
	"Upgrade",
	"Keep-Alive",
}

// connectionTokens returns the canonicalized names named by Connection headers.
func connectionTokens(h http.Header) []string {
	var out []string
	for _, v := range h.Values("Connection") {
		for _, tok := range strings.Split(v, ",") {
			tok = strings.TrimSpace(tok)
			if tok != "" {
				out = append(out, textproto.CanonicalMIMEHeaderKey(tok))
			}
		}
	}
	return out
}

// stripHopByHop removes hop-by-hop headers, including any header named by
// Connection. Proxy-Authorization is ALWAYS stripped so local proxy
// credentials never reach upstream origins (FR-008.004).
func stripHopByHop(h http.Header) {
	tokens := connectionTokens(h)
	for _, name := range hopByHopHeaders {
		h.Del(name)
	}
	for _, tok := range tokens {
		h.Del(tok)
	}
}

// serveHTTP handles the non-SOCKS branch of the first-byte demux (§1.0):
// keep-alive loop of HTTP requests, CONNECT tunneling, absolute-form
// forwarding. Auth is enforced on EVERY request when enabled (zcode F6).
func (s *Server) serveHTTP(br *peekReader, conn net.Conn) {
	for {
		req, err := http.ReadRequest(br.br)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				s.log.Warnf("http: read request: %v", err)
			}
			return
		}

		if s.cfg.AuthEnabled {
			user, pass, ok := parseBasicAuth(req.Header.Get("Proxy-Authorization"))
			expectedPass := s.resolvePassword()
			if !ok || user != s.cfg.Username || subtle.ConstantTimeCompare([]byte(pass), []byte(expectedPass)) != 1 {
				writeHTTPError(conn, http.StatusProxyAuthRequired,
					"Proxy-Authenticate: Basic realm=\"unet\"\r\n\r\nproxy authentication required")
				return
			}
		}

		if req.Method == http.MethodConnect {
			s.handleConnect(conn, req)
			return // connection is now a raw byte tunnel
		}
		if !s.handleForward(conn, req) {
			return
		}
		if wantsClose(req) {
			return
		}
	}
}

// parseBasicAuth decodes a Proxy-Authorization: Basic header value.
func parseBasicAuth(v string) (user, pass string, ok bool) {
	const prefix = "Basic "
	if len(v) <= len(prefix) || !strings.EqualFold(v[:len(prefix)], prefix) {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v[len(prefix):]))
	if err != nil {
		return "", "", false
	}
	i := strings.IndexByte(string(raw), ':')
	if i < 0 {
		return "", "", false
	}
	return string(raw[:i]), string(raw[i+1:]), true
}

func wantsClose(req *http.Request) bool {
	for _, tok := range connectionTokens(req.Header) {
		if strings.EqualFold(tok, "close") {
			return true
		}
	}
	return req.Close
}

// dialTimeoutCtx wraps the request context with the configured dial timeout.
func dialTimeoutCtx(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, d)
}

// handleConnect bridges a CONNECT tunnel through the netstack.
func (s *Server) handleConnect(conn net.Conn, req *http.Request) {
	host := req.Host
	if host == "" {
		writeHTTPError(conn, http.StatusBadRequest, "missing CONNECT target")
		return
	}
	ctx, cancel := dialTimeoutCtx(req.Context(), s.cfg.DialTimeout)
	defer cancel()
	up, err := s.dial.DialContext(ctx, "tcp", host)
	if err != nil {
		s.log.Warnf("http CONNECT %s: %v", redactTarget(host), err)
		writeHTTPError(conn, http.StatusBadGateway, "dial through tunnel failed")
		return
	}
	defer up.Close()

	if _, err := conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		return
	}
	bridge(conn, up)
}

// handleForward forwards an absolute-form (or origin-form with Host) request
// through the netstack. Upstream keep-alives are disabled for v1 (correctness
// first; T6 revisits) — each request opens a fresh tunnel stream.
func (s *Server) handleForward(conn net.Conn, req *http.Request) bool {
	target := req.URL.Host
	if target == "" {
		target = req.Host
	}
	if target == "" {
		writeHTTPError(conn, http.StatusBadRequest, "no target host")
		return false
	}

	// Origin-form requests (§1.4: "use the Host header as the target host")
	// arrive with a relative URL — rebuild an absolute http URL for RoundTrip.
	if req.URL.Scheme == "" {
		u := *req.URL
		u.Scheme = "http"
		u.Host = target
		u.Opaque = ""
		req.URL = &u
	}

	stripHopByHop(req.Header)
	req.RequestURI = "" // required by RoundTrip for client requests

	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			ctx, cancel := dialTimeoutCtx(ctx, s.cfg.DialTimeout)
			defer cancel()
			return s.dial.DialContext(ctx, network, addr)
		},
		DisableKeepAlives: true,
	}
	resp, err := transport.RoundTrip(req)
	if err != nil {
		s.log.Warnf("http forward %s: %v", redactTarget(target), err)
		writeHTTPError(conn, http.StatusBadGateway, "forward through tunnel failed")
		return false
	}
	defer resp.Body.Close()
	stripHopByHop(resp.Header)

	if err := resp.Write(conn); err != nil {
		return false
	}
	// Drain up to 1MB of the body so responses don't stall the keep-alive loop.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	return true
}

// writeHTTPError writes a minimal error response; extraHeaders may carry
// e.g. the Proxy-Authenticate challenge.
func writeHTTPError(conn net.Conn, code int, msgWithHeaders string) {
	head := fmt.Sprintf("HTTP/1.1 %d %s\r\n", code, http.StatusText(code))
	body := "unet proxy error\n"
	if i := strings.Index(msgWithHeaders, "\r\n\r\n"); i >= 0 {
		head += msgWithHeaders[:i+4]
		body = msgWithHeaders[i+4:]
	} else {
		head += "Content-Type: text/plain\r\n"
	}
	head += fmt.Sprintf("Content-Length: %d\r\nConnection: close\r\n\r\n", len(body))
	_, _ = conn.Write([]byte(head + body))
}
