package mobile

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T) (*Server, *Devices) {
	t.Helper()
	devices, _ := openTestDevices(t)
	return NewServer(devices, 9760), devices
}

func request(method, target, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:40000"
	r.Host = "127.0.0.1:9760"
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	return r
}

func serve(s *Server, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

// pair runs a pairing through the API and returns the cookie it set.
func pair(t *testing.T, s *Server, devices *Devices) *http.Cookie {
	t.Helper()
	code, _, _ := devices.NewPairing(time.Now())
	w := serve(s, request(http.MethodPost, "/api/pair", `{"code":"`+code+`","name":"Telefon"}`))
	if w.Code != http.StatusOK {
		t.Fatalf("pair = %d %s", w.Code, w.Body)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "asmgr_device" {
			return c
		}
	}
	t.Fatal("no cookie")
	return nil
}

func TestNonLoopbackPeerIsRefused(t *testing.T) {
	s, _ := newTestServer(t)
	r := request(http.MethodGet, "/", "")
	r.RemoteAddr = "192.168.1.5:1234"
	if w := serve(s, r); w.Code != http.StatusForbidden {
		t.Fatalf("code = %d", w.Code)
	}
}

func TestHostsAllowed(t *testing.T) {
	s, _ := newTestServer(t)
	for host, want := range map[string]int{
		"127.0.0.1:9760":          http.StatusOK,
		"localhost:9760":          http.StatusOK,
		"gep.tail1234.ts.net":     http.StatusOK,
		"evil.example.com":        http.StatusMisdirectedRequest,
		"127.0.0.1:9999":          http.StatusMisdirectedRequest,
		"ts.net.evil.example.com": http.StatusMisdirectedRequest,
		"evilts.net":              http.StatusMisdirectedRequest,
		"single.ts.net":           http.StatusMisdirectedRequest,
		"a.b?.ts.net":             http.StatusMisdirectedRequest,
		"Gep.Tail1.ts.net":        http.StatusOK,
	} {
		r := request(http.MethodGet, "/", "")
		r.Host = host
		if w := serve(s, r); w.Code != want {
			t.Errorf("%s: %d, want %d", host, w.Code, want)
		}
	}
}

// An origin must be this very server as the browser reached it: another machine of
// the same tailnet is the same site to a browser, and so gets the cookie sent along.
func TestCrossOriginWriteIsRefused(t *testing.T) {
	s, devices := newTestServer(t)
	for _, test := range []struct {
		host, origin string
		want         int
	}{
		{"127.0.0.1:9760", "https://evil.example.com", http.StatusForbidden},
		{"127.0.0.1:9760", "http://127.0.0.1:9999", http.StatusForbidden},
		{"127.0.0.1:9760", "https://gep.tail1234.ts.net", http.StatusForbidden},
		{"127.0.0.1:9760", "http://localhost:9760", http.StatusForbidden},
		{"127.0.0.1:9760", "http://127.0.0.1:9760", http.StatusOK},
		{"127.0.0.1:9760", "", http.StatusOK},
		{"gep.tail1234.ts.net", "https://gep.tail1234.ts.net", http.StatusOK},
		{"gep.tail1234.ts.net", "http://gep.tail1234.ts.net", http.StatusForbidden},
		{"gep.tail1234.ts.net", "https://masik.tail1234.ts.net", http.StatusForbidden},
		{"gep.tail1234.ts.net", "https://gep.other99.ts.net", http.StatusForbidden},
	} {
		code, _, _ := devices.NewPairing(time.Now())
		r := request(http.MethodPost, "/api/pair", `{"code":"`+code+`","name":"x"}`)
		r.Host = test.host
		if test.origin != "" {
			r.Header.Set("Origin", test.origin)
		}
		if w := serve(s, r); w.Code != test.want {
			t.Errorf("host %s origin %q: %d, want %d", test.host, test.origin, w.Code, test.want)
		}
	}
}

// A WebSocket handshake is a GET, and comes with the cookie from any page.
func TestAnUpgradeFromAnotherOriginIsRefused(t *testing.T) {
	s, _ := newTestServer(t)
	r := request(http.MethodGet, "/", "")
	r.Header.Set("Connection", "Upgrade")
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Origin", "https://evil.example.com")
	if w := serve(s, r); w.Code != http.StatusForbidden {
		t.Fatalf("code = %d", w.Code)
	}
}

func TestPairSetsStrictCookie(t *testing.T) {
	s, devices := newTestServer(t)
	cookie := pair(t, s, devices)
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/" || cookie.Secure {
		t.Fatalf("cookie = %+v", cookie)
	}
	code, _, _ := devices.NewPairing(time.Now())
	r := request(http.MethodPost, "/api/pair", `{"code":"`+code+`","name":"x"}`)
	r.Header.Set("X-Forwarded-Proto", "https")
	for _, c := range serve(s, r).Result().Cookies() {
		if c.Name == "asmgr_device" && !c.Secure {
			t.Fatal("not Secure behind HTTPS")
		}
	}
}

func TestWrongCodeIsRefused(t *testing.T) {
	s, _ := newTestServer(t)
	w := serve(s, request(http.MethodPost, "/api/pair", `{"code":"nope","name":"x"}`))
	if w.Code != http.StatusUnauthorized || len(w.Result().Cookies()) != 0 {
		t.Fatalf("code = %d, cookies %v", w.Code, w.Result().Cookies())
	}
}

func TestMeNeedsDevice(t *testing.T) {
	s, devices := newTestServer(t)
	if w := serve(s, request(http.MethodGet, "/api/me", "")); w.Code != http.StatusUnauthorized {
		t.Fatalf("without cookie: %d", w.Code)
	}
	cookie := pair(t, s, devices)
	r := request(http.MethodGet, "/api/me", "")
	r.AddCookie(cookie)
	w := serve(s, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"Telefon"`) {
		t.Fatalf("with cookie: %d %s", w.Code, w.Body)
	}
}

func TestRevokedDeviceIsRejected(t *testing.T) {
	s, devices := newTestServer(t)
	cookie := pair(t, s, devices)
	if err := devices.Revoke(devices.List()[0].ID); err != nil {
		t.Fatal(err)
	}
	r := request(http.MethodGet, "/api/me", "")
	r.AddCookie(cookie)
	if w := serve(s, r); w.Code != http.StatusUnauthorized {
		t.Fatalf("code = %d", w.Code)
	}
}

func TestLogoutForgetsTheCookie(t *testing.T) {
	s, devices := newTestServer(t)
	cookie := pair(t, s, devices)
	r := request(http.MethodPost, "/api/logout", "")
	r.AddCookie(cookie)
	w := serve(s, r)
	cleared := false
	for _, c := range w.Result().Cookies() {
		cleared = cleared || (c.Name == "asmgr_device" && c.MaxAge < 0)
	}
	if w.Code != http.StatusOK || !cleared {
		t.Fatalf("code = %d, cookies %v", w.Code, w.Result().Cookies())
	}
}

func TestStartOnBusyPortFails(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port, _ := strconv.Atoi(strings.TrimPrefix(listener.Addr().String(), "127.0.0.1:"))
	devices, _ := openTestDevices(t)
	if err := NewServer(devices, port).Start(); !errors.Is(err, ErrPortInUse) {
		t.Fatalf("err = %v", err)
	}
}

func TestStartServesOnLoopbackAndStops(t *testing.T) {
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	port, _ := strconv.Atoi(strings.TrimPrefix(listener.Addr().String(), "127.0.0.1:"))
	listener.Close()
	devices, _ := openTestDevices(t)
	s := NewServer(devices, port)
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/")
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %v %v", resp, err)
	}
	resp.Body.Close()
	if err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if again, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port)); err != nil {
		t.Fatalf("port still held: %v", err)
	} else {
		again.Close()
	}
}

func TestBodyLimit(t *testing.T) {
	s, _ := newTestServer(t)
	big := `{"code":"` + strings.Repeat("a", 70*1024) + `","name":"x"}`
	if w := serve(s, request(http.MethodPost, "/api/pair", big)); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code = %d", w.Code)
	}
}

// The page a phone opens from the QR code, and the script that pairs it. Scripts come
// from this server only: the page handles a pairing code and later a terminal.
func TestIndexServesThePairingPage(t *testing.T) {
	s, _ := newTestServer(t)
	w := serve(s, request(http.MethodGet, "/", ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `src="/pair.js"`) {
		t.Fatalf("index = %d %s", w.Code, w.Body)
	}
	csp := w.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "script-src 'self'", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("csp = %q, missing %q", csp, want)
		}
	}
	scripts := strings.Split(strings.Split(csp, "script-src")[1], ";")[0]
	if strings.TrimSpace(scripts) != "'self'" {
		t.Fatalf("scripts not limited to this server: %q", scripts)
	}
	js := serve(s, request(http.MethodGet, "/pair.js", ""))
	if js.Code != http.StatusOK || !strings.HasPrefix(js.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("pair.js = %d %q", js.Code, js.Header().Get("Content-Type"))
	}
}

// Funnel puts a tailnet service on the public internet. This page lets a device type
// into terminals, so it is never served that way, whatever serve is set to.
func TestAFunnelRequestIsRefused(t *testing.T) {
	s, _ := newTestServer(t)
	r := request(http.MethodGet, "/", "")
	r.Host = "gep.tail1234.ts.net"
	r.Header.Set("Tailscale-Funnel-Request", "?1")
	if w := serve(s, r); w.Code != http.StatusForbidden {
		t.Fatalf("code = %d", w.Code)
	}
}
