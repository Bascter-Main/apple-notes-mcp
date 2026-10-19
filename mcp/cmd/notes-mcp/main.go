package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"example.com/icloud-notes-mcp/internal/icloudadapter"
	"example.com/icloud-notes-mcp/internal/mcp"
	"example.com/icloud-notes-mcp/internal/security"
)

func env(k, def string) string {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	return v
}
func main() {
	log.SetFlags(log.LstdFlags)
	addr := env("MCP_BIND", "127.0.0.1:8765")
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		log.Fatal("invalid MCP_BIND")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		log.Fatal("refusing non-loopback MCP_BIND without OAuth; keep private behind tunnel")
	}
	stateDir := env("ICLOUD_STATE_DIR", "./.private")
	var store security.Store = security.FileStore{Dir: stateDir}
	if env("ICLOUD_SESSION_MODE", "file") == "static-env" {
		store = security.StaticEnvStore{KeyB64: os.Getenv("ICLOUD_SESSION_KEY_B64"), CiphertextB64: os.Getenv("ICLOUD_SESSION_ENCRYPTED_B64")}
	}
	maxNotes, _ := strconv.Atoi(env("MAX_SCAN_NOTES", "1000"))
	roots := strings.Split(os.Getenv("ALLOWED_FOLDER_IDS"), ",")
	source, err := icloudadapter.NewSource(store, roots, maxNotes)
	if err != nil {
		log.Fatal(err)
	}
	timeout, _ := time.ParseDuration(env("ICLOUD_READ_TIMEOUT", "90s"))
	cacheSecs, _ := strconv.Atoi(env("MCP_CACHE_SECONDS", "120"))
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.NewServer(source, time.Duration(cacheSecs)*time.Second))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("alive\n"))
	})
	srv := &http.Server{Addr: addr, Handler: requestDeadline(mux, timeout), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("private MCP listening on %s (loopback only)", addr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("MCP listener failed: %v", err)
	}
}
func requestDeadline(next http.Handler, d time.Duration) http.Handler {
	if d <= 0 {
		d = 90 * time.Second
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
