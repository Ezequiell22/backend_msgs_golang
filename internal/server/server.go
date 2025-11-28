package server

import (
    "context"
    "crypto/rand"
    "encoding/json"
    "encoding/base64"
    "io"
    "math/big"
    "net/http"
    "strings"
    "time"

	applog "backend_msgs_golang/internal/log"
	"backend_msgs_golang/internal/storage"
)

type Config struct {
    Addr              string
    PlaceholderTTL    time.Duration
    MessageTTL        time.Duration
    ReadTimeout       time.Duration
    ReadHeaderTimeout time.Duration
    WriteTimeout      time.Duration
    IdleTimeout       time.Duration
    MaxBodyBytes      int64
    AllowedOrigins    []string
    RateLimitRPS      int
    RateBurst         int
}

type Server struct {
    cfg    Config
    store  storage.Storage
    router http.Handler
    log    applog.Logger
    tokens chan struct{}
}

func New(cfg Config, st storage.Storage, lg applog.Logger) *Server {
    s := &Server{cfg: cfg, store: st, log: lg}
    if cfg.RateLimitRPS > 0 {
        burst := cfg.RateBurst
        if burst <= 0 { burst = cfg.RateLimitRPS }
        s.tokens = make(chan struct{}, burst)
        t := time.NewTicker(time.Second / time.Duration(cfg.RateLimitRPS))
        go func() {
            for range t.C {
                select { case s.tokens <- struct{}{}: default: }
            }
        }()
    }
    mux := http.NewServeMux()
    mux.HandleFunc("/code", s.postCode)
    mux.HandleFunc("/message/", s.message)
    mux.HandleFunc("/health", s.health)
    s.router = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        s.secHeaders(w)
        s.corsHeaders(w, r)
        if r.Method == http.MethodOptions { w.WriteHeader(http.StatusNoContent); return }
        rid := s.requestID()
        w.Header().Set("X-Request-Id", rid)
        if !s.allow() { w.WriteHeader(http.StatusTooManyRequests); return }
        mux.ServeHTTP(w, r)
    })
    return s
}

func (s *Server) Handler() http.Handler { return s.router }

func (s *Server) secHeaders(w http.ResponseWriter) {
    w.Header().Set("Referrer-Policy", "no-referrer")
    w.Header().Set("Cache-Control", "no-store")
    w.Header().Set("X-Content-Type-Options", "nosniff")
    w.Header().Set("Pragma", "no-cache")
}

func (s *Server) corsHeaders(w http.ResponseWriter, r *http.Request) {
    if len(s.cfg.AllowedOrigins) == 0 { return }
    o := r.Header.Get("Origin")
    if o == "" { return }
    for _, a := range s.cfg.AllowedOrigins {
        if a == "*" || a == o {
            w.Header().Set("Access-Control-Allow-Origin", o)
            w.Header().Set("Vary", "Origin")
            w.Header().Set("Access-Control-Allow-Methods", "GET,PUT,POST,OPTIONS")
            w.Header().Set("Access-Control-Allow-Headers", "Content-Type,X-Request-Id")
            break
        }
    }
}

func (s *Server) requestID() string {
    var b [16]byte
    rand.Read(b[:])
    const hex = "0123456789abcdef"
    out := make([]byte, 32)
    for i := 0; i < 16; i++ { out[i*2] = hex[b[i]>>4]; out[i*2+1] = hex[b[i]&0x0f] }
    return string(out)
}

func (s *Server) allow() bool {
    if s.tokens == nil { return true }
    select { case <-s.tokens: return true; default: return false }
}

func (s *Server) generateCode(n int) string {
	const letters = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	var b strings.Builder
	for i := 0; i < n; i++ {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		b.WriteByte(letters[idx.Int64()])
	}
	return b.String()
}

func (s *Server) postCode(w http.ResponseWriter, r *http.Request) {
    if r.Method != http.MethodPost {
        w.WriteHeader(http.StatusMethodNotAllowed)
        return
    }
    ctx := r.Context()
	var code string
	for {
		code = s.generateCode(8)
		ok, err := s.store.ReserveCode(ctx, code, s.cfg.PlaceholderTTL)
		if err != nil {
			if s.log != nil {
				s.log.Error("reserve_code_error", map[string]any{"endpoint": "code"})
			}
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if ok {
			break
		}
	}
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("Location", "/message/"+code)
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(map[string]string{"code": code})
}

func (s *Server) message(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/message/") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if r.Method == http.MethodPut {
		s.putMessage(w, r)
		return
	}
	if r.Method == http.MethodGet {
		s.getMessage(w, r)
		return
	}
	w.WriteHeader(http.StatusMethodNotAllowed)
}

func (s *Server) putMessage(w http.ResponseWriter, r *http.Request) {
    s.secHeaders(w)
    code := strings.TrimPrefix(r.URL.Path, "/message/")
    max := s.cfg.MaxBodyBytes
    if max <= 0 {
        max = 1 << 20
    }
    r.Body = http.MaxBytesReader(w, r.Body, max)
    body, err := io.ReadAll(r.Body)
    if err != nil {
        if s.log != nil {
            s.log.Warn("body_read_error", map[string]any{"endpoint": "message_put"})
        }
        w.WriteHeader(http.StatusBadRequest)
        return
    }
    ct := strings.TrimSpace(string(body))
    if ct == "" {
        if s.log != nil {
            s.log.Warn("empty_body", map[string]any{"endpoint": "message_put"})
        }
        w.WriteHeader(http.StatusBadRequest)
        return
    }
    buf, err := base64.StdEncoding.DecodeString(ct)
    if err != nil || len(buf) < 13 {
        if s.log != nil {
            s.log.Warn("invalid_base64", map[string]any{"endpoint": "message_put"})
        }
        w.WriteHeader(http.StatusBadRequest)
        return
    }
    iv := buf[:12]
    if len(iv) != 12 || len(buf[12:]) == 0 {
        if s.log != nil {
            s.log.Warn("invalid_iv", map[string]any{"endpoint": "message_put"})
        }
        w.WriteHeader(http.StatusBadRequest)
        return
    }

    ok, err := s.store.AttachCipher(r.Context(), code, ct, s.cfg.MessageTTL)
    if err != nil {
        if s.log != nil {
            s.log.Error("attach_cipher_error", map[string]any{"endpoint": "message_put"})
		}
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if !ok {
		if s.log != nil {
			s.log.Warn("attach_conflict", map[string]any{"endpoint": "message_put"})
		}
		w.WriteHeader(http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) {
    code := strings.TrimPrefix(r.URL.Path, "/message/")
    ct, ok, err := s.store.GetAndDelete(r.Context(), code)
    if err != nil {
        if s.log != nil {
            s.log.Error("get_delete_error", map[string]any{"endpoint": "message_get"})
        }
        w.WriteHeader(http.StatusInternalServerError)
        return
    }
    if !ok {
        w.WriteHeader(http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "text/plain")
    w.Write([]byte(ct))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
    st := "ok"
    if err := s.store.Ping(r.Context()); err != nil { st = "error" }
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]string{"status": "ok", "redis": st})
}

func validBase64(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			continue
		}
		return false
	}
	return true
}

func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.router,
		ReadTimeout:       s.cfg.ReadTimeout,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		WriteTimeout:      s.cfg.WriteTimeout,
		IdleTimeout:       s.cfg.IdleTimeout,
	}
	go func() {
		<-ctx.Done()
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}()
	return srv.ListenAndServe()
}
