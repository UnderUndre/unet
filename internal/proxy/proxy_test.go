package proxy

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDialer dials a stub listener; records the last requested address and
// can be forced to fail with a given error.
type fakeDialer struct {
	stubAddr string
	failErr  error
	lastAddr string
}

func (f *fakeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	f.lastAddr = address
	if f.failErr != nil {
		return nil, f.failErr
	}
	var d net.Dialer
	return d.DialContext(ctx, network, f.stubAddr)
}

// stubUpstream echoes bytes and records the first raw request line + whether
// a given header survived.
type stubUpstream struct {
	ln      net.Listener
	mu      sync.Mutex
	raw     []byte
	hasAuth bool
}

func (s *stubUpstream) record(b []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.raw = append(s.raw, b...)
	s.hasAuth = strings.Contains(strings.ToLower(string(s.raw)), "proxy-authorization:")
}

func (s *stubUpstream) snapshot() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.raw)
}

func newStubUpstream(t *testing.T, echo bool) *stubUpstream {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &stubUpstream{ln: ln}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(conn, echo)
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *stubUpstream) serve(conn net.Conn, echo bool) {
	defer conn.Close()
	buf := make([]byte, 4096)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			s.record(buf[:n])
			if echo {
				conn.Write(buf[:n])
			} else {
				// Answer once so HTTP transports complete their round trip.
				conn.Write([]byte("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n"))
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func testServer(t *testing.T, dial Dialer, cfg Config) (addr string, srv *Server) {
	t.Helper()
	cfg.Listen = "127.0.0.1:0"
	cfg.DialTimeout = 2 * time.Second
	lg := &testLogger{}
	srv = New(cfg, dial, lg)
	ln, err := Listen(cfg.Listen)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.handleListener(ln) }()
	return ln.Addr().String(), srv
}

func dialTCP(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

type testLogger struct {
	msgs []string
}

func (l *testLogger) Infof(f string, a ...interface{}) {
	l.msgs = append(l.msgs, "I "+fmt.Sprintf(f, a...))
}
func (l *testLogger) Warnf(f string, a ...interface{}) {
	l.msgs = append(l.msgs, "W "+fmt.Sprintf(f, a...))
}
func (l *testLogger) Errorf(f string, a ...interface{}) {
	l.msgs = append(l.msgs, "E "+fmt.Sprintf(f, a...))
}

func TestSOCKS5ConnectNoAuthEcho(t *testing.T) {
	up := newStubUpstream(t, true)
	fd := &fakeDialer{stubAddr: up.ln.Addr().String()}
	addr, _ := testServer(t, fd, Config{})

	conn := dialTCP(t, addr)
	// greeting: no-auth offer
	fmt.Fprintf(conn, "\x05\x01\x00")
	buf := make([]byte, 2)
	if _, err := ioReadFull(conn, buf); err != nil {
		t.Fatal(err)
	}
	if buf[0] != 0x05 || buf[1] != 0x00 {
		t.Fatalf("method reply = %v", buf)
	}
	// CONNECT 127.0.0.1:port (IPv4 atyp)
	tport := portOf(t, up.ln.Addr().String())
	req := []byte{0x05, 0x01, 0x00, 0x01, 127, 0, 0, 1, byte(tport >> 8), byte(tport)}
	conn.Write(req)
	rep := make([]byte, 10)
	if _, err := ioReadFull(conn, rep); err != nil {
		t.Fatal(err)
	}
	if rep[1] != repSucceeded {
		t.Fatalf("connect reply = %d", rep[1])
	}
	// echo roundtrip
	conn.Write([]byte("hello-unet"))
	echo := make([]byte, 10)
	if _, err := ioReadFull(conn, echo); err != nil {
		t.Fatal(err)
	}
	if string(echo) != "hello-unet" {
		t.Fatalf("echo = %q", echo)
	}
	if fd.lastAddr == "" {
		t.Fatal("dialer never called")
	}
}

func TestSOCKS5AuthRequiredAndEnforced(t *testing.T) {
	up := newStubUpstream(t, true)
	fd := &fakeDialer{stubAddr: up.ln.Addr().String()}
	addr, _ := testServer(t, fd, Config{AuthEnabled: true, Username: "local", Password: "s3cret"})

	conn := dialTCP(t, addr)
	fmt.Fprintf(conn, "\x05\x02\x00\x02") // offer none+userpass
	rep := make([]byte, 2)
	_, _ = ioReadFull(conn, rep)
	if rep[1] != authUserPass {
		t.Fatalf("server chose method 0x%02x, want 0x02", rep[1])
	}
	// wrong password
	fmt.Fprintf(conn, "\x01\x05local\x05wrong")
	st := make([]byte, 2)
	_, _ = ioReadFull(conn, st)
	if st[1] != 0x01 {
		t.Fatalf("auth status = %d, want failure 1", st[1])
	}
	// correct password on a fresh conn
	conn2 := dialTCP(t, addr)
	fmt.Fprintf(conn2, "\x05\x02\x00\x02")
	buf := make([]byte, 2)
	_, _ = ioReadFull(conn2, buf)
	fmt.Fprintf(conn2, "\x01\x05local\x06s3cret")
	st2 := make([]byte, 2)
	_, _ = ioReadFull(conn2, st2)
	if st2[1] != 0x00 {
		t.Fatalf("auth status = %d, want success", st2[1])
	}
}

func TestSOCKS5DNSFailClosed(t *testing.T) {
	up := newStubUpstream(t, true)
	fd := &fakeDialer{stubAddr: up.ln.Addr().String(), failErr: fmt.Errorf("lookup example.invalid: no such host")}
	addr, _ := testServer(t, fd, Config{})
	conn := dialTCP(t, addr)
	fmt.Fprintf(conn, "\x05\x01\x00")
	buf := make([]byte, 2)
	_, _ = ioReadFull(conn, buf)
	domain := "example.invalid"
	req := []byte{0x05, 0x01, 0x00, atypDomain, byte(len(domain))}
	req = append(req, domain...)
	req = append(req, 0x01, 0xBB) // port 443
	conn.Write(req)
	rep := make([]byte, 10)
	if _, err := ioReadFull(conn, rep); err != nil {
		t.Fatal(err)
	}
	if rep[1] != repHostUnreach {
		t.Fatalf("reply = %d, want %d (fail closed, zcode F2/FR-009)", rep[1], repHostUnreach)
	}
}

func TestHTTPForwardStripsProxyAuthorization(t *testing.T) {
	up := newStubUpstream(t, false)
	fd := &fakeDialer{stubAddr: up.ln.Addr().String()}
	addr, srv := testServer(t, fd, Config{})
	lg2 := srv.log.(*testLogger)
	conn := dialTCP(t, addr)

	req, _ := http.NewRequest("GET", "http://example.invalid/path", nil)
	req.Header.Set("Proxy-Authorization", "Basic dXNlcjpwd2Q=")
	req.Header.Set("Connection", "keep-alive")
	req.Write(conn)

	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	respBytes := make([]byte, 0, 1024)
	tmp := make([]byte, 256)
	for {
		n, err := conn.Read(tmp)
		respBytes = append(respBytes, tmp[:n]...)
		if err != nil || len(respBytes) > 2048 {
			break
		}
	}

	raw := up.snapshot()
	lower := strings.ToLower(raw)
	if strings.Contains(lower, "proxy-authorization:") {
		t.Fatalf("Proxy-Authorization LEAKED to upstream: %q", raw)
	}
	if !strings.Contains(lower, "get /path") {
		t.Fatalf("request not forwarded; stub got %q; client got %q; logs %v", raw, respBytes, lg2.msgs)
	}
	if !strings.Contains(string(respBytes), "204") {
		t.Fatalf("client response = %q, want 204 from stub", respBytes)
	}
	if fd.lastAddr != "example.invalid:80" {
		t.Fatalf("dial target = %q", fd.lastAddr)
	}
}

func TestDemuxHTTPAndSOCKSOnOneListener(t *testing.T) {
	up := newStubUpstream(t, true)
	fd := &fakeDialer{stubAddr: up.ln.Addr().String()}
	addr, _ := testServer(t, fd, Config{})

	// HTTP client on the same listener a SOCKS5 client would use
	conn := dialTCP(t, addr)
	fmt.Fprintf(conn, "GET http://example.invalid/x HTTP/1.1\r\nHost: example.invalid\r\n\r\n")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(up.raw) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if len(up.snapshot()) == 0 {
		t.Fatal("HTTP branch never reached the stub — demux broken")
	}
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	fmt.Sscanf(p, "%d", &port)
	return port
}

func ioReadFull(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
