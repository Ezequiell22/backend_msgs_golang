package main

import (
	"context"
	"crypto/tls"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	applog "backend_msgs_golang/internal/log"
	"backend_msgs_golang/internal/server"
	redisstore "backend_msgs_golang/internal/storage/redis"

	redis "github.com/redis/go-redis/v9"
)

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def
	}
	return d
}

func envInt64(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	i, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return i
}

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	if v == "1" || v == "true" || v == "TRUE" {
		return true
	}
	if v == "0" || v == "false" || v == "FALSE" {
		return false
	}
	return def
}

func envCSV(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	parts := []string{}
	for _, p := range strings.Split(v, ",") {
		s := strings.TrimSpace(p)
		if s != "" {
			parts = append(parts, s)
		}
	}
	return parts
}

func main() {
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	placeholderTTL := envDuration("PLACEHOLDER_TTL", 30*time.Minute)
	messageTTL := envDuration("MESSAGE_TTL", 24*time.Hour)
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	redisPassword := os.Getenv("REDIS_PASSWORD")
	redisDBStr := os.Getenv("REDIS_DB")
	redisDB := 0
	if redisDBStr != "" {
		if i, err := strconv.Atoi(redisDBStr); err == nil {
			redisDB = i
		}
	}
	useTLS := envBool("REDIS_TLS", false)

	ropts := &redis.Options{Addr: redisAddr, Password: redisPassword, DB: redisDB}
	if v := os.Getenv("REDIS_POOL_SIZE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			ropts.PoolSize = i
		}
	}
	if v := os.Getenv("REDIS_MIN_IDLE"); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			ropts.MinIdleConns = i
		}
	}
	if v := os.Getenv("REDIS_DIAL_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ropts.DialTimeout = d
		}
	}
	if v := os.Getenv("REDIS_READ_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ropts.ReadTimeout = d
		}
	}
	if v := os.Getenv("REDIS_WRITE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			ropts.WriteTimeout = d
		}
	}
	if useTLS {
		ropts.TLSConfig = &tls.Config{}
	}
	st := redisstore.NewWithOptions(ropts)
	lg := applog.New(os.Getenv("LOG_LEVEL"))
	cfg := server.Config{
		Addr:              addr,
		PlaceholderTTL:    placeholderTTL,
		MessageTTL:        messageTTL,
		ReadTimeout:       envDuration("READ_TIMEOUT", 5*time.Second),
		ReadHeaderTimeout: envDuration("READ_HEADER_TIMEOUT", 5*time.Second),
		WriteTimeout:      envDuration("WRITE_TIMEOUT", 10*time.Second),
		IdleTimeout:       envDuration("IDLE_TIMEOUT", 60*time.Second),
		MaxBodyBytes:      envInt64("MAX_BODY_BYTES", 1<<20),
		AllowedOrigins:    envCSV("CORS_ALLOW_ORIGINS"),
		RateLimitRPS: func() int {
			v := os.Getenv("RATE_LIMIT_RPS")
			if v == "" {
				return 0
			}
			i, _ := strconv.Atoi(v)
			return i
		}(),
		RateBurst: func() int {
			v := os.Getenv("RATE_BURST")
			if v == "" {
				return 0
			}
			i, _ := strconv.Atoi(v)
			return i
		}(),
	}
	srv := server.New(cfg, st, lg)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	srv.Start(ctx)
}
