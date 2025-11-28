package server

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type mockStore struct {
	reserveOK bool
	attachOK  bool
	attachErr error
	getVal    string
	getOK     bool
	getErr    error
	pingErr   error
}

func (m *mockStore) ReserveCode(_ context.Context, code string, ttl time.Duration) (bool, error) {
	return m.reserveOK, nil
}
func (m *mockStore) AttachCipher(_ context.Context, code string, ciphertext string, ttl time.Duration) (bool, error) {
	return m.attachOK, m.attachErr
}
func (m *mockStore) GetAndDelete(_ context.Context, code string) (string, bool, error) {
	return m.getVal, m.getOK, m.getErr
}
func (m *mockStore) Ping(_ context.Context) error { return m.pingErr }

func newTestServer(ms *mockStore) *Server {
	cfg := Config{Addr: ":0", PlaceholderTTL: time.Minute, MessageTTL: time.Hour}
	lg := &nopLogger{}
	s := New(cfg, ms, lg)
	return s
}

type nopLogger struct{}

func (*nopLogger) Debug(string, map[string]any) {}
func (*nopLogger) Info(string, map[string]any)  {}
func (*nopLogger) Warn(string, map[string]any)  {}
func (*nopLogger) Error(string, map[string]any) {}

func TestPostCode201(t *testing.T) {
	ms := &mockStore{reserveOK: true}
	s := newTestServer(ms)
	req := httptest.NewRequest(http.MethodPost, "/code", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", w.Code)
	}
	if w.Header().Get("Location") == "" {
		t.Fatalf("missing Location header")
	}
}

func TestPutMessageValid(t *testing.T) {
	ms := &mockStore{attachOK: true}
	s := newTestServer(ms)
	iv := make([]byte, 12)
	payload := []byte("abc")
	body := base64.StdEncoding.EncodeToString(append(iv, payload...))
	req := httptest.NewRequest(http.MethodPut, "/message/xyz", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}

func TestPutMessageBadBase64(t *testing.T) {
	s := newTestServer(&mockStore{})
	req := httptest.NewRequest(http.MethodPut, "/message/xyz", strings.NewReader("%%%"))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPutMessageInvalidIV(t *testing.T) {
	s := newTestServer(&mockStore{})
	body := base64.StdEncoding.EncodeToString([]byte("short"))
	req := httptest.NewRequest(http.MethodPut, "/message/xyz", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestPutMessageConflict(t *testing.T) {
	s := newTestServer(&mockStore{attachOK: false})
	iv := make([]byte, 12)
	body := base64.StdEncoding.EncodeToString(append(iv, []byte("x")...))
	req := httptest.NewRequest(http.MethodPut, "/message/xyz", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}
}

func TestPutMessageError(t *testing.T) {
	s := newTestServer(&mockStore{attachOK: false, attachErr: assertErr{}})
	iv := make([]byte, 12)
	body := base64.StdEncoding.EncodeToString(append(iv, []byte("x")...))
	req := httptest.NewRequest(http.MethodPut, "/message/xyz", strings.NewReader(body))
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

type assertErr struct{}

func (assertErr) Error() string { return "err" }

func TestGetMessageOK(t *testing.T) {
	s := newTestServer(&mockStore{getVal: "abc", getOK: true})
	req := httptest.NewRequest(http.MethodGet, "/message/xyz", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != "abc" {
		t.Fatalf("unexpected body")
	}
}

func TestGetMessageNotFound(t *testing.T) {
	s := newTestServer(&mockStore{getOK: false})
	req := httptest.NewRequest(http.MethodGet, "/message/xyz", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestGetMessageError(t *testing.T) {
	s := newTestServer(&mockStore{getErr: assertErr{}})
	req := httptest.NewRequest(http.MethodGet, "/message/xyz", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", w.Code)
	}
}

func TestHealth(t *testing.T) {
	s := newTestServer(&mockStore{})
	// replace health handler route present via New
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestCORSPreflight(t *testing.T) {
	ms := &mockStore{}
	cfg := Config{AllowedOrigins: []string{"*"}}
	lg := &nopLogger{}
	s := New(cfg, ms, lg)
	req := httptest.NewRequest(http.MethodOptions, "/code", nil)
	req.Header.Set("Origin", "http://example.com")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if w.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Fatalf("missing CORS header")
	}
}

func TestRateLimit(t *testing.T) {
	ms := &mockStore{reserveOK: true}
	cfg := Config{RateLimitRPS: 1, RateBurst: 1}
	lg := &nopLogger{}
	s := New(cfg, ms, lg)
	s.tokens = make(chan struct{}, 1)
	s.tokens <- struct{}{}
	// first allowed
	r1 := httptest.NewRequest(http.MethodPost, "/code", nil)
	w1 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w1, r1)
	if w1.Code == http.StatusTooManyRequests {
		t.Fatalf("rate limited unexpectedly")
	}
	// second denied
	r2 := httptest.NewRequest(http.MethodPost, "/code", nil)
	w2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(w2, r2)
	if w2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w2.Code)
	}
}
