// Package api exposes the HTTP API of the storage backend.
//
// Routes (relative to the configured global prefix:
//
//	POST /scenes        store a scene, returns {"id":"<16 digits>"}
//	GET  /scenes/{id}   raw scene bytes
//	PUT  /rooms/{id}    store a room, returns {"id":"<id>"}
//	GET  /rooms/{id}    raw room bytes
//	PUT  /files/{id}    store a file, returns {"id":"<id>"}
//	GET  /files/{id}    raw file bytes
//
// Request bodies are always consumed raw,
// regardless of the Content-Type header.
package api

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/alexberdnik/excalidraw-storage-backend-go/internal/config"
	"github.com/alexberdnik/excalidraw-storage-backend-go/internal/store"
)

const SceneIDLength = 16

type API struct {
	store      store.Store
	logger     *slog.Logger
	bodyLimit  int64
	newSceneID func() (string, error)
	prefix     string
}

type Option func(*API)

func WithSceneIDGenerator(fn func() (string, error)) Option {
	return func(a *API) {
		if fn != nil {
			a.newSceneID = fn
		}
	}
}

func New(cfg config.Config, st store.Store, logger *slog.Logger, opts ...Option) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}

	a := &API{
		store:      st,
		logger:     logger,
		bodyLimit:  cfg.BodyLimit,
		newSceneID: NewSceneID,
		prefix:     config.NormalizePrefix(cfg.GlobalPrefix),
	}
	for _, opt := range opts {
		opt(a)
	}

	mux := http.NewServeMux()

	// POST /scenes — both without and with a trailing slash, because the
	// Excalidraw frontend is commonly configured with a trailing slash and
	// browsers turn 301 redirects of POST requests into GET requests.
	mux.HandleFunc(pattern(http.MethodPost, a.path("/scenes")), a.handleCreateScene)
	mux.HandleFunc(pattern(http.MethodPost, a.path("/scenes/{$}")), a.handleCreateScene)

	// PUT /rooms/{id}, PUT /files/{id}
	mux.HandleFunc(pattern(http.MethodPut, a.path("/rooms/{id}")), a.handlePut(store.Rooms, "room"))
	mux.HandleFunc(pattern(http.MethodPut, a.path("/files/{id}")), a.handlePut(store.Files, "file"))

	// GET /scenes/{id}, GET /rooms/{id}, GET /files/{id}
	mux.HandleFunc(pattern(http.MethodGet, a.path("/scenes/{id}")), a.handleGet(store.Scenes, "scene"))
	mux.HandleFunc(pattern(http.MethodGet, a.path("/rooms/{id}")), a.handleGet(store.Rooms, "room"))
	mux.HandleFunc(pattern(http.MethodGet, a.path("/files/{id}")), a.handleGet(store.Files, "file"))

	// Everything else gets the 404 response.
	mux.HandleFunc("/", a.handleNotFound)

	return recoverer(logger, cors(mux))
}

func pattern(method, path string) string {
	return method + " " + path
}

func (a *API) path(route string) string {
	return a.prefix + route
}

// handleGet returns the raw stored bytes for the given namespace.
func (a *API) handleGet(namespace store.Namespace, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		value, err := a.store.Get(r.Context(), namespace, id)
		if errors.Is(err, store.ErrNotFound) {
			a.writeError(w, http.StatusNotFound, "Not Found")
			return
		}
		if err != nil {
			a.logger.Error("get "+kind, "id", id, "error", err)
			a.writeError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}

		a.logger.Debug("get "+kind, "id", id, "bytes", len(value))

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(value)))
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodHead {
			return
		}
		if _, err := w.Write(value); err != nil {
			a.logger.Debug("write response", "id", id, "error", err)
		}
	}
}

// handlePut stores the raw request body under the id from the URL.
func (a *API) handlePut(namespace store.Namespace, kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")

		payload, ok := a.readBody(w, r)
		if !ok {
			return
		}

		if err := a.store.Set(r.Context(), namespace, id, payload); err != nil {
			a.logger.Error("create "+kind, "id", id, "error", err)
			a.writeError(w, http.StatusInternalServerError, "Internal Server Error")
			return
		}

		a.logger.Debug("created "+kind, "id", id, "bytes", len(payload))
		a.writeJSON(w, http.StatusOK, idResponse{ID: id})
	}
}

// handleCreateScene stores the raw request body under a newly generated
// numeric id. A collision is reported as 500.
func (a *API) handleCreateScene(w http.ResponseWriter, r *http.Request) {
	payload, ok := a.readBody(w, r)
	if !ok {
		return
	}

	id, err := a.newSceneID()
	if err != nil {
		a.logger.Error("generate scene id", "error", err)
		a.writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	created, err := a.store.Create(r.Context(), store.Scenes, id, payload)
	if err != nil {
		a.logger.Error("create scene", "id", id, "error", err)
		a.writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}
	if !created {
		a.logger.Error("create scene: id collision", "id", id)
		a.writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return
	}

	a.logger.Debug("created scene", "id", id, "bytes", len(payload))
	a.writeJSON(w, http.StatusCreated, idResponse{ID: id})
}

func (a *API) handleNotFound(w http.ResponseWriter, r *http.Request) {
	a.writeError(w, http.StatusNotFound, "Cannot "+r.Method+" "+r.URL.Path)
}

// readBody reads the whole request body up to the configured limit.
func (a *API) readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Body == nil {
		return []byte{}, true
	}

	if r.ContentLength > a.bodyLimit {
		a.writeError(w, http.StatusRequestEntityTooLarge, "request entity too large")
		return nil, false
	}

	body := http.MaxBytesReader(w, r.Body, a.bodyLimit)
	payload, err := io.ReadAll(body)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			a.writeError(w, http.StatusRequestEntityTooLarge, "request entity too large")
			return nil, false
		}
		a.logger.Error("read request body", "error", err)
		a.writeError(w, http.StatusInternalServerError, "Internal Server Error")
		return nil, false
	}

	if payload == nil {
		payload = []byte{}
	}
	return payload, true
}

func (a *API) writeJSON(w http.ResponseWriter, status int, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		a.logger.Error("encode json response", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		a.logger.Debug("write json response", "error", err)
	}
}

func (a *API) writeError(w http.ResponseWriter, status int, message string) {
	a.writeJSON(w, status, errorResponse{
		Message:    message,
		StatusCode: status,
	})
}

type idResponse struct {
	ID string `json:"id"`
}

type errorResponse struct {
	Message    string `json:"message"`
	StatusCode int    `json:"statusCode"`
}

// NewSceneID generates a random numeric id of SceneIDLength digits.
func NewSceneID() (string, error) {
	const digits = "0123456789"

	out := make([]byte, 0, SceneIDLength)
	var buf [1]byte
	for len(out) < SceneIDLength {
		if _, err := rand.Read(buf[:]); err != nil {
			return "", err
		}
		// Reject the upper part of the byte range so the modulo does not
		// introduce a bias (256 is not a multiple of 10).
		if buf[0] >= 250 {
			continue
		}
		out = append(out, digits[int(buf[0])%len(digits)])
	}
	return string(out), nil
}

// every origin is allowed and preflight requests are answered directly.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Access-Control-Allow-Origin", "*")

		if r.Method == http.MethodOptions {
			header.Set("Access-Control-Allow-Methods", "GET,HEAD,PUT,PATCH,POST,DELETE")
			if requested := r.Header.Get("Access-Control-Request-Headers"); requested != "" {
				header.Set("Access-Control-Allow-Headers", requested)
				header.Add("Vary", "Access-Control-Request-Headers")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// recoverer converts panics into a JSON 500 response instead of dropping
// the connection.
func recoverer(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("panic while handling request",
					"method", r.Method, "path", r.URL.Path, "panic", recovered)
				writeInternalError(w)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func writeInternalError(w http.ResponseWriter) {
	body := []byte(`{"message":"Internal Server Error","statusCode":500}`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusInternalServerError)
	w.Write(body)
}
