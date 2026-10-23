package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/alswl/excalidraw-storage-backend-go/internal/api"
	"github.com/alswl/excalidraw-storage-backend-go/internal/config"
	"github.com/alswl/excalidraw-storage-backend-go/internal/store"
)

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		Port:         "0",
		GlobalPrefix: config.DefaultGlobalPrefix,
		SQLitePath:   filepath.Join(t.TempDir(), "test.db"),
		BodyLimit:    config.DefaultBodyLimit,
		LogLevel:     slog.LevelError,
	}
}

func newServer(t *testing.T, cfg config.Config, opts ...api.Option) *httptest.Server {
	t.Helper()

	st, err := store.Open(cfg.SQLitePath)
	if err != nil {
		t.Fatalf("store.Open(): %v", err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Errorf("store.Close(): %v", err)
		}
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	srv := httptest.NewServer(api.New(cfg, st, logger, opts...))
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url string, body []byte, headers map[string]string) (*http.Response, []byte) {
	t.Helper()

	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("http.NewRequest(%s %s): %v", method, url, err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	t.Cleanup(func() { res.Body.Close() })

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return res, resBody
}

func decodeJSON(t *testing.T, body []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatalf("decode json %q: %v", body, err)
	}
}

type idBody struct {
	ID string `json:"id"`
}

type errorBody struct {
	Message    string `json:"message"`
	StatusCode int    `json:"statusCode"`
}

func TestScenesRoundTrip(t *testing.T) {
	srv := newServer(t, testConfig(t))
	payload := []byte(`{"type":"excalidraw","version":2,"elements":[]}`)

	// Excalidraw does not send a Content-Type header.
	res, body := do(t, http.MethodPost, srv.URL+"/api/v2/scenes", payload, nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /scenes status = %d, want %d (body %s)", res.StatusCode, http.StatusCreated, body)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("POST /scenes Content-Type = %q, want application/json", ct)
	}

	var created idBody
	decodeJSON(t, body, &created)
	if !regexp.MustCompile(`^\d{16}$`).MatchString(created.ID) {
		t.Fatalf("POST /scenes id = %q, want 16 digits", created.ID)
	}

	res, body = do(t, http.MethodGet, srv.URL+"/api/v2/scenes/"+created.ID, nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /scenes/%s status = %d, want 200 (body %s)", created.ID, res.StatusCode, body)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("GET /scenes/%s Content-Type = %q, want application/octet-stream", created.ID, ct)
	}
	if !bytes.Equal(body, payload) {
		t.Errorf("GET /scenes/%s body = %q, want %q", created.ID, body, payload)
	}
	if got, want := res.Header.Get("Content-Length"), strconv.Itoa(len(payload)); got != want {
		t.Errorf("GET /scenes/%s Content-Length = %q, want %q", created.ID, got, want)
	}
}

func TestScenesCreateAcceptsTrailingSlash(t *testing.T) {
	srv := newServer(t, testConfig(t))

	res, body := do(t, http.MethodPost, srv.URL+"/api/v2/scenes/", []byte("scene"), nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /scenes/ status = %d, want %d (body %s)", res.StatusCode, http.StatusCreated, body)
	}
}

func TestScenesIdCollisionReturns500(t *testing.T) {
	const fixedID = "1234567890123456"

	srv := newServer(t, testConfig(t), api.WithSceneIDGenerator(func() (string, error) {
		return fixedID, nil
	}))

	res, body := do(t, http.MethodPost, srv.URL+"/api/v2/scenes", []byte("first"), nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("first POST /scenes status = %d, want 201 (body %s)", res.StatusCode, body)
	}

	res, body = do(t, http.MethodPost, srv.URL+"/api/v2/scenes", []byte("second"), nil)
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("colliding POST /scenes status = %d, want 500 (body %s)", res.StatusCode, body)
	}
	var errBody errorBody
	decodeJSON(t, body, &errBody)
	if errBody.Message != "Internal Server Error" || errBody.StatusCode != 500 {
		t.Errorf("colliding POST /scenes body = %+v, want Internal Server Error/500", errBody)
	}

	// The original value must be preserved.
	res, body = do(t, http.MethodGet, srv.URL+"/api/v2/scenes/"+fixedID, nil, nil)
	if res.StatusCode != http.StatusOK || string(body) != "first" {
		t.Errorf("GET after collision = %d %q, want 200 %q", res.StatusCode, body, "first")
	}
}

func TestScenesNotFound(t *testing.T) {
	srv := newServer(t, testConfig(t))

	res, body := do(t, http.MethodGet, srv.URL+"/api/v2/scenes/0000000000000000", nil, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("GET missing scene status = %d, want 404", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("GET missing scene Content-Type = %q, want application/json", ct)
	}
	var errBody errorBody
	decodeJSON(t, body, &errBody)
	if errBody.Message != "Not Found" || errBody.StatusCode != 404 {
		t.Errorf("GET missing scene body = %+v, want Not Found/404", errBody)
	}
}

func TestRoomsRoundTrip(t *testing.T) {
	srv := newServer(t, testConfig(t))
	payload := []byte(`{"type":"excalidraw","elements":[]}`)

	res, body := do(t, http.MethodPut, srv.URL+"/api/v2/rooms/abc123", payload, map[string]string{
		"Content-Type": "text/plain", // must be ignored: bodies are always raw
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT /rooms/abc123 status = %d, want 200 (body %s)", res.StatusCode, body)
	}
	var stored idBody
	decodeJSON(t, body, &stored)
	if stored.ID != "abc123" {
		t.Errorf("PUT /rooms/abc123 id = %q, want abc123", stored.ID)
	}

	res, body = do(t, http.MethodGet, srv.URL+"/api/v2/rooms/abc123", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /rooms/abc123 status = %d, want 200", res.StatusCode)
	}
	if !bytes.Equal(body, payload) {
		t.Errorf("GET /rooms/abc123 body = %q, want %q", body, payload)
	}
}

func TestFilesRoundTripBinary(t *testing.T) {
	srv := newServer(t, testConfig(t))

	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}

	res, body := do(t, http.MethodPut, srv.URL+"/api/v2/files/image-1", payload, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT /files/image-1 status = %d, want 200 (body %s)", res.StatusCode, body)
	}

	res, body = do(t, http.MethodGet, srv.URL+"/api/v2/files/image-1", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET /files/image-1 status = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("GET /files/image-1 Content-Type = %q, want application/octet-stream", ct)
	}
	if !bytes.Equal(body, payload) {
		t.Error("GET /files/image-1 body does not match the uploaded binary")
	}
}

func TestPutOverwrites(t *testing.T) {
	srv := newServer(t, testConfig(t))

	if res, _ := do(t, http.MethodPut, srv.URL+"/api/v2/files/x", []byte("old"), nil); res.StatusCode != http.StatusOK {
		t.Fatalf("first PUT status = %d", res.StatusCode)
	}
	if res, _ := do(t, http.MethodPut, srv.URL+"/api/v2/files/x", []byte("new"), nil); res.StatusCode != http.StatusOK {
		t.Fatalf("second PUT status = %d", res.StatusCode)
	}

	_, body := do(t, http.MethodGet, srv.URL+"/api/v2/files/x", nil, nil)
	if string(body) != "new" {
		t.Errorf("GET after overwrite = %q, want %q", body, "new")
	}
}

func TestEmptyBodyIsStored(t *testing.T) {
	srv := newServer(t, testConfig(t))

	res, body := do(t, http.MethodPost, srv.URL+"/api/v2/scenes", nil, nil)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("POST /scenes with empty body status = %d, want 201 (body %s)", res.StatusCode, body)
	}
	var created idBody
	decodeJSON(t, body, &created)

	res, body = do(t, http.MethodGet, srv.URL+"/api/v2/scenes/"+created.ID, nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET empty scene status = %d, want 200", res.StatusCode)
	}
	if len(body) != 0 {
		t.Errorf("GET empty scene body = %q, want empty", body)
	}
}

func TestBodyLimit(t *testing.T) {
	cfg := testConfig(t)
	cfg.BodyLimit = 8
	srv := newServer(t, cfg)

	res, body := do(t, http.MethodPost, srv.URL+"/api/v2/scenes", bytes.Repeat([]byte("a"), 100), nil)
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("POST oversized scene status = %d, want 413 (body %s)", res.StatusCode, body)
	}

	// Chunked request (unknown content length) must be limited as well.
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v2/scenes", io.NopCloser(strings.NewReader(strings.Repeat("b", 100))))
	if err != nil {
		t.Fatalf("http.NewRequest(): %v", err)
	}
	req.ContentLength = -1
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST chunked scene: %v", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("chunked POST oversized scene status = %d, want 413", res.StatusCode)
	}
}

func TestGlobalPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		path   string
	}{
		{name: "default", prefix: "/api/v2", path: "/api/v2/scenes"},
		{name: "custom", prefix: "/custom/v1", path: "/custom/v1/scenes"},
		{name: "empty", prefix: "", path: "/scenes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig(t)
			cfg.GlobalPrefix = tt.prefix
			srv := newServer(t, cfg)

			res, body := do(t, http.MethodPost, srv.URL+tt.path, []byte("scene"), nil)
			if res.StatusCode != http.StatusCreated {
				t.Fatalf("POST %s status = %d, want 201 (body %s)", tt.path, res.StatusCode, body)
			}

			// The default prefix must not be reachable when another one
			// (or none) is configured.
			if tt.prefix != config.DefaultGlobalPrefix {
				res, body = do(t, http.MethodPost, srv.URL+"/api/v2/scenes", []byte("scene"), nil)
				if res.StatusCode != http.StatusNotFound {
					t.Errorf("POST /api/v2/scenes status = %d, want 404 (body %s)", res.StatusCode, body)
				}
			}
		})
	}
}

func TestCORS(t *testing.T) {
	srv := newServer(t, testConfig(t))

	res, _ := do(t, http.MethodGet, srv.URL+"/api/v2/scenes/does-not-exist", nil, map[string]string{
		"Origin": "http://localhost:3000",
	})
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q, want *", got)
	}

	req, err := http.NewRequest(http.MethodOptions, srv.URL+"/api/v2/scenes", nil)
	if err != nil {
		t.Fatalf("http.NewRequest(): %v", err)
	}
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("OPTIONS: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusNoContent {
		t.Errorf("preflight status = %d, want 204", res.StatusCode)
	}
	if got := res.Header.Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("preflight Allow-Origin = %q, want *", got)
	}
	if got := res.Header.Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Errorf("preflight Allow-Methods = %q, want it to contain POST", got)
	}
	if got := res.Header.Get("Access-Control-Allow-Headers"); got != "content-type" {
		t.Errorf("preflight Allow-Headers = %q, want content-type", got)
	}
}

func TestUnknownRouteReturnsJSON404(t *testing.T) {
	srv := newServer(t, testConfig(t))

	for _, path := range []string{"/api/v2/unknown", "/"} {
		res, body := do(t, http.MethodGet, srv.URL+path, nil, nil)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want 404", path, res.StatusCode)
		}
		var errBody errorBody
		decodeJSON(t, body, &errBody)
		wantMessage := "Cannot GET " + path
		if errBody.Message != wantMessage || errBody.StatusCode != 404 {
			t.Errorf("GET %s body = %+v, want %q/404", path, errBody, wantMessage)
		}
	}
}

func TestHeadRequest(t *testing.T) {
	srv := newServer(t, testConfig(t))

	if res, _ := do(t, http.MethodPut, srv.URL+"/api/v2/files/head-test", []byte("12345"), nil); res.StatusCode != http.StatusOK {
		t.Fatalf("PUT status = %d", res.StatusCode)
	}

	res, body := do(t, http.MethodHead, srv.URL+"/api/v2/files/head-test", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", res.StatusCode)
	}
	if len(body) != 0 {
		t.Errorf("HEAD body = %q, want empty", body)
	}
	if got := res.Header.Get("Content-Length"); got != "5" {
		t.Errorf("HEAD Content-Length = %q, want 5", got)
	}
}

func TestNamespacesAreIsolatedOverHTTP(t *testing.T) {
	srv := newServer(t, testConfig(t))

	if res, _ := do(t, http.MethodPut, srv.URL+"/api/v2/rooms/shared", []byte("room"), nil); res.StatusCode != http.StatusOK {
		t.Fatalf("PUT /rooms/shared status = %d", res.StatusCode)
	}

	res, _ := do(t, http.MethodGet, srv.URL+"/api/v2/files/shared", nil, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("GET /files/shared status = %d, want 404", res.StatusCode)
	}
}

func TestUnsupportedMethodReturns404(t *testing.T) {
	srv := newServer(t, testConfig(t))

	// Express has no per-method 405: an unmatched method falls through to
	// the router's 404 handler, so the Go version does the same.
	res, body := do(t, http.MethodDelete, srv.URL+"/api/v2/scenes/123", nil, nil)
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("DELETE /scenes/123 status = %d, want 404", res.StatusCode)
	}
	var errBody errorBody
	decodeJSON(t, body, &errBody)
	if errBody.Message != "Cannot DELETE /api/v2/scenes/123" {
		t.Errorf("DELETE /scenes/123 message = %q", errBody.Message)
	}
}

func TestNewSceneID(t *testing.T) {
	pattern := regexp.MustCompile(`^\d{16}$`)
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id, err := api.NewSceneID()
		if err != nil {
			t.Fatalf("NewSceneID(): %v", err)
		}
		if !pattern.MatchString(id) {
			t.Fatalf("NewSceneID() = %q, want 16 digits", id)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("NewSceneID() returned duplicate %q", id)
		}
		seen[id] = struct{}{}
	}
}
