// Package api implements the REST API (and the static web UI serving) of the
// NexVE daemon. Routing uses the standard library only (net/http ServeMux,
// Go 1.22 method+wildcard patterns) – no framework dependencies.
package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/nexve/nexve/internal/config"
	"github.com/nexve/nexve/internal/driver"
	"github.com/nexve/nexve/internal/node"
	"github.com/nexve/nexve/internal/web"
)

type Server struct {
	cfg   *config.Config
	drv   driver.Driver
	nodes *node.Collector
	log   *log.Logger
	mux   *http.ServeMux
}

func New(cfg *config.Config, drv driver.Driver, nodes *node.Collector, logger *log.Logger) *Server {
	s := &Server{cfg: cfg, drv: drv, nodes: nodes, log: logger, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	// --- API ---------------------------------------------------------------
	s.mux.HandleFunc("GET /api/v1/health", s.handleHealth) // no auth: load balancers / installers
	s.mux.HandleFunc("GET /api/v1/node/status", s.handleNodeStatus)

	s.mux.HandleFunc("GET /api/v1/vms", s.handleListVMs)
	s.mux.HandleFunc("POST /api/v1/vms", s.handleCreateVM)
	s.mux.HandleFunc("GET /api/v1/vms/{id}", s.handleGetVM)
	s.mux.HandleFunc("DELETE /api/v1/vms/{id}", s.handleDeleteVM)
	s.mux.HandleFunc("POST /api/v1/vms/{id}/start", s.handleStartVM)
	s.mux.HandleFunc("POST /api/v1/vms/{id}/stop", s.handleStopVM)
	s.mux.HandleFunc("GET /api/v1/vms/{id}/console", s.handleConsole)

	s.mux.HandleFunc("GET /api/v1/isos", s.handleListISOs)
	s.mux.HandleFunc("POST /api/v1/isos", s.handleUploadISO)

	// --- everything else: the embedded web UI ------------------------------
	s.mux.HandleFunc("/", s.handleStatic)
}

// Handler returns the full middleware chain: logging -> auth -> mux.
func (s *Server) Handler() http.Handler {
	return s.recoverer(s.logRequests(s.authenticate(s.mux)))
}

// ListenAndServe starts the API + web UI, with TLS when configured.
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:              s.cfg.Listen,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if !s.cfg.AuthEnabled() {
		s.log.Printf("WARNING: no NEXVE_AUTH_TOKEN / NEXVE_AUTH_USER+PASSWORD configured – the API is OPEN")
	}
	if s.cfg.TLSCert != "" {
		s.log.Printf("listening on https://%s (TLS on)", s.cfg.Listen)
		return srv.ListenAndServeTLS(s.cfg.TLSCert, s.cfg.TLSKey)
	}
	s.log.Printf("listening on http://%s (no TLS – set NEXVE_TLS_CERT/KEY)", s.cfg.Listen)
	return srv.ListenAndServe()
}

// ---------------------------------------------------------------------------
// Middleware
// ---------------------------------------------------------------------------

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		// Keep /health out of the noise.
		if !strings.HasSuffix(r.URL.Path, "/health") {
			s.log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}

// authenticate guards every /api/ route except /api/v1/health.
// Accepted credentials (both optional, both constant-time compared):
//   - Authorization: Bearer <NEXVE_AUTH_TOKEN>
//   - Authorization: Basic  base64(NEXVE_AUTH_USER:NEXVE_AUTH_PASSWORD)
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}
		if !s.cfg.AuthEnabled() { // development mode
			next.ServeHTTP(w, r)
			return
		}
		auth := r.Header.Get("Authorization")
		if s.cfg.AuthToken != "" && strings.HasPrefix(auth, "Bearer ") {
			token := strings.TrimPrefix(auth, "Bearer ")
			if subtle.ConstantTimeCompare([]byte(token), []byte(s.cfg.AuthToken)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}
		if s.cfg.AuthUser != "" && strings.HasPrefix(auth, "Basic ") {
			user, pass, ok := r.BasicAuth()
			if ok &&
				subtle.ConstantTimeCompare([]byte(user), []byte(s.cfg.AuthUser)) == 1 &&
				subtle.ConstantTimeCompare([]byte(pass), []byte(s.cfg.AuthPassword)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="nexve", Basic realm="nexve"`)
		writeErr(w, http.StatusUnauthorized, "unauthorized: provide a valid token or basic-auth credentials")
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Printf("panic serving %s %s: %v", r.Method, r.URL.Path, rec)
				writeErr(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	// Dashboard data is per-user/tenant state: never cache it.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// writeDriverErr maps driver sentinel errors onto HTTP status codes.
func writeDriverErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, driver.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, driver.ErrExists):
		writeErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, driver.ErrInvalid):
		writeErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, driver.ErrUnavailable):
		writeErr(w, http.StatusServiceUnavailable, err.Error())
	case errors.Is(err, driver.ErrUnsupported):
		writeErr(w, http.StatusNotImplemented, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

// ---------------------------------------------------------------------------
// Handlers – node
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": s.cfg.Version,
		"driver":  s.drv.Name(),
		"time":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleNodeStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.nodes.Status())
}

// ---------------------------------------------------------------------------
// Handlers – VMs / containers
// ---------------------------------------------------------------------------

func (s *Server) handleListVMs(w http.ResponseWriter, r *http.Request) {
	insts, err := s.drv.List(r.Context())
	if err != nil {
		writeDriverErr(w, err)
		return
	}
	if insts == nil {
		insts = []driver.Instance{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"instances": insts})
}

func (s *Server) handleGetVM(w http.ResponseWriter, r *http.Request) {
	inst, err := s.drv.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDriverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) handleCreateVM(w http.ResponseWriter, r *http.Request) {
	var spec driver.CreateSpec
	body := io.LimitReader(r.Body, 1<<20) // 1 MiB is plenty for a spec
	if err := json.NewDecoder(body).Decode(&spec); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	inst, err := s.drv.Create(r.Context(), spec)
	if err != nil {
		writeDriverErr(w, err)
		return
	}
	s.log.Printf("created %s (%s)", inst.ID, inst.State)
	writeJSON(w, http.StatusCreated, inst)
}

func (s *Server) handleStartVM(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.drv.Start(r.Context(), id); err != nil {
		writeDriverErr(w, err)
		return
	}
	inst, _ := s.drv.Get(r.Context(), id)
	writeJSON(w, http.StatusOK, firstOrEcho(inst, id, "started"))
}

func (s *Server) handleStopVM(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	force := r.URL.Query().Get("force") == "1" || r.URL.Query().Get("force") == "true"
	if err := s.drv.Stop(r.Context(), id, force); err != nil {
		writeDriverErr(w, err)
		return
	}
	inst, _ := s.drv.Get(r.Context(), id)
	writeJSON(w, http.StatusOK, firstOrEcho(inst, id, "stopped"))
}

func (s *Server) handleDeleteVM(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	purge := r.URL.Query().Get("purge") == "1" || r.URL.Query().Get("purge") == "true"
	if err := s.drv.Delete(r.Context(), id, purge); err != nil {
		writeDriverErr(w, err)
		return
	}
	s.log.Printf("deleted %s (purge=%v)", id, purge)
	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func (s *Server) handleConsole(w http.ResponseWriter, r *http.Request) {
	info, err := s.drv.Console(r.Context(), r.PathValue("id"))
	if err != nil {
		writeDriverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func firstOrEcho(inst *driver.Instance, id, msg string) any {
	if inst != nil {
		return inst
	}
	return map[string]string{"id": id, "result": msg}
}

// ---------------------------------------------------------------------------
// Handlers – ISO library
// ---------------------------------------------------------------------------

func (s *Server) handleListISOs(w http.ResponseWriter, r *http.Request) {
	isos, err := s.drv.ListISOs(r.Context())
	if err != nil {
		writeDriverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"isos": isos})
}

// handleUploadISO accepts multipart/form-data with an "iso" file part.
// Uploads are streamed straight to the ISO library (max 16 GiB).
func (s *Server) handleUploadISO(w http.ResponseWriter, r *http.Request) {
	const maxUpload = 16 << 30
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeErr(w, http.StatusBadRequest, "expected multipart/form-data: "+err.Error())
		return
	}
	file, header, err := r.FormFile("iso")
	if err != nil {
		writeErr(w, http.StatusBadRequest, "missing form file field 'iso': "+err.Error())
		return
	}
	defer file.Close()
	iso, err := s.drv.SaveISO(r.Context(), header.Filename, file)
	if err != nil {
		writeDriverErr(w, err)
		return
	}
	s.log.Printf("ISO uploaded: %s (%d bytes)", iso.Name, iso.SizeB)
	writeJSON(w, http.StatusCreated, iso)
}

// ---------------------------------------------------------------------------
// Static web UI (embedded SPA)
// ---------------------------------------------------------------------------

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeErr(w, http.StatusNotFound, "unknown API route")
		return
	}

	staticFS := web.FS()
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}

	data, err := fs.ReadFile(staticFS, name)
	if err != nil {
		// SPA fallback: unknown paths render the app shell (client-side routes).
		name = "index.html"
		data, err = fs.ReadFile(staticFS, name)
		if err != nil {
			s.serveNotBuilt(w)
			return
		}
	}

	ctype := mime.TypeByExtension("." + fileExt(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	if name == "index.html" {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // Vite content-hashes assets
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(data)
}

func fileExt(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return ""
}

// serveNotBuilt is shown when the binary runs without an embedded frontend
// (e.g. plain `go run ./cmd/nexved` during backend development).
func (s *Server) serveNotBuilt(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8"><title>NexVE</title>
<body style="font-family:system-ui;background:#09090b;color:#fafafa;display:grid;place-items:center;height:100vh;margin:0">
<div style="text-align:center;max-width:34rem">
<h1 style="font-size:1.4rem">NexVE API is running 🎉</h1>
<p style="color:#a1a1aa">The web UI was not embedded in this binary. Build it with:</p>
<pre style="background:#18181b;padding:1rem;border-radius:.5rem;text-align:left">make frontend && make backend</pre>
<p style="color:#a1a1aa">…or develop with hot reload: <code>make dev</code> (vite on :5173 → API on :8006)</p>
<p style="color:#71717a">API health: <a style="color:#60a5fa" href="/api/v1/health">/api/v1/health</a></p>
</div></body>`)
}
