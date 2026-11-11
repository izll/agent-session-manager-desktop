package mobile

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The server listens on this computer only. Reaching it from a phone is the VPN's
// job: `tailscale serve` takes the requests to this address, and so is the only
// other way in. Every layer below assumes that, and checks it again.

const (
	cookieName = "asmgr_device"
	// maxBody: nothing this API takes is larger than a short reply.
	maxBody = 64 << 10
	// cookieLifetime: a device stays let in until revoked; the cookie is only
	// the place the browser keeps its secret.
	cookieLifetime = 400 * 24 * time.Hour
)

// ErrPortInUse is a port something else already listens on.
var ErrPortInUse = errors.New("port already in use")

// Server is the phone's way into the app.
type Server struct {
	devices *Devices
	port    int

	mu       sync.Mutex
	http     *http.Server
	listener net.Listener
}

// NewServer prepares a server for 127.0.0.1:port.
func NewServer(devices *Devices, port int) *Server {
	return &Server{devices: devices, port: port}
}

// Start listens and serves in the background.
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port)))
	if err != nil {
		if addressInUse(err) {
			return fmt.Errorf("%w: %d", ErrPortInUse, s.port)
		}
		return err
	}
	server := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	s.mu.Lock()
	s.http, s.listener = server, listener
	s.mu.Unlock()
	go func() { _ = server.Serve(listener) }()
	return nil
}

// Stop ends serving and frees the port.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	server := s.http
	s.http, s.listener = nil, nil
	s.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Shutdown(ctx)
}

// Handler is the whole server, its checks in front of its routes.
func (s *Server) Handler() http.Handler {
	routes := http.NewServeMux()
	routes.HandleFunc("POST /api/pair", s.handlePair)
	routes.HandleFunc("GET /api/me", s.withDevice(s.handleMe))
	routes.HandleFunc("POST /api/logout", s.handleLogout)
	routes.HandleFunc("GET /{$}", s.handleIndex)
	routes.HandleFunc("GET /pair.js", s.handlePairScript)
	return s.guard(routes)
}

// guard turns away what did not come the one way in.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// From this computer: the VPN's proxy, or the user at it. Bound to the
		// loopback address, anything else would mean the binding changed.
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if ip := net.ParseIP(host); err != nil || ip == nil || !ip.IsLoopback() {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// Funnel puts a tailnet service on the public internet. This one lets a
		// device type into terminals, so it never answers there.
		if r.Header.Get("Tailscale-Funnel-Request") != "" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		// A name of this server's own. Another one is a page elsewhere that
		// had its name pointed here (DNS rebinding).
		if !s.hostAllowed(r.Host) {
			http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
			return
		}
		// A page of this server's own, for anything that changes something.
		// A browser sends Origin on those; its absence is a client that is
		// not a browser, which a page elsewhere cannot make one send.
		// A WebSocket handshake is a GET, and carries the cookie from any page.
		if (r.Method != http.MethodGet && r.Method != http.MethodHead) || r.Header.Get("Upgrade") != "" {
			if origin := r.Header.Get("Origin"); origin != "" && !originMatchesHost(origin, r.Host) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		next.ServeHTTP(w, r)
	})
}

func (s *Server) hostAllowed(host string) bool {
	port := strconv.Itoa(s.port)
	if host == "127.0.0.1:"+port || host == "localhost:"+port {
		return true
	}
	// tailscale serve: <machine>.<tailnet>.ts.net, without a port.
	return isTailnetName(host)
}

// originMatchesHost wants the origin to be this server as the browser reached it,
// nothing wider: every machine of one tailnet is the same site to a browser, so a page
// on another one gets the cookie sent along and would otherwise act with it.
func originMatchesHost(origin, host string) bool {
	scheme := "http://"
	if isTailnetName(host) {
		scheme = "https://"
	}
	return origin == scheme+host
}

// tailnetName: a machine and a tailnet label under .ts.net, letters, digits and
// hyphens only. Anything wider could carry a port, a path or a fragment, and take a
// pairing code elsewhere.
var tailnetName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+\.ts\.net$`)

// IsTailnetName says whether host is a tailnet machine name.
func IsTailnetName(host string) bool {
	return tailnetName.MatchString(strings.ToLower(host))
}

func isTailnetName(host string) bool { return IsTailnetName(host) }

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "request too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		name = "?"
	}
	if len(name) > 80 {
		name = name[:80]
	}
	device, secret, err := s.devices.Redeem(body.Code, name, time.Now())
	switch {
	case errors.Is(err, ErrPairingLocked):
		http.Error(w, "pairing locked", http.StatusTooManyRequests)
		return
	case errors.Is(err, ErrPairingInvalid):
		http.Error(w, "invalid code", http.StatusUnauthorized)
		return
	case err != nil:
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: secret, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: viaHTTPS(r),
		MaxAge: int(cookieLifetime / time.Second),
	})
	writeJSON(w, map[string]string{"deviceId": device.ID})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", HttpOnly: true,
		SameSite: http.SameSiteStrictMode, Secure: viaHTTPS(r), MaxAge: -1,
	})
	writeJSON(w, map[string]bool{"ok": true})
}

type deviceKey struct{}

// withDevice lets through only a device let in and not revoked.
func (s *Server) withDevice(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		device, ok := s.device(r)
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), deviceKey{}, device)))
	}
}

func (s *Server) device(r *http.Request) (Device, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return Device{}, false
	}
	return s.devices.Authenticate(cookie.Value, time.Now())
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	device := r.Context().Value(deviceKey{}).(Device)
	writeJSON(w, map[string]string{"id": device.ID, "name": device.Name})
}

// web is the phone's page. Scripts come from this server only, never inline: the page
// will hold a terminal that types into the user's shells.
//
//go:embed web
var web embed.FS

const pageSecurity = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	s.serveWebFile(w, "web/index.html", "text/html; charset=utf-8")
}

func (s *Server) handlePairScript(w http.ResponseWriter, r *http.Request) {
	s.serveWebFile(w, "web/pair.js", "text/javascript; charset=utf-8")
}

func (s *Server) serveWebFile(w http.ResponseWriter, name, contentType string) {
	data, err := web.ReadFile(name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Security-Policy", pageSecurity)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func viaHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}
