// An OAuth resource-server gateway for an existing localhost-only MCP server.
// Authorization is enforced BEFORE any request can reach the upstream notes MCP.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/authplane/go-sdk/core/authplane"
	"github.com/authplane/go-sdk/core/resource"
	"github.com/authplane/go-sdk/http/pkg/authplanehttp"
)

func mustEnv(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		log.Fatalf("missing required environment variable %s", name)
	}
	return value
}

func validateEndpoints(issuer, audience string) error {
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return errors.New("AUTHPLANE_ISSUER must be an HTTPS origin without path, credentials, query, or fragment")
	}
	if audience != issuer+"/mcp" {
		return errors.New("AUTHPLANE_RESOURCE must equal AUTHPLANE_ISSUER plus /mcp")
	}
	return nil
}

func main() {
	issuer := mustEnv("AUTHPLANE_ISSUER")
	audience := mustEnv("AUTHPLANE_RESOURCE")
	subject := mustEnv("AUTH_ALLOWED_SUBJECT")
	address := mustEnv("GATEWAY_LISTEN")
	upstreamAddress := mustEnv("MCP_UPSTREAM")
	if address != "127.0.0.1:8766" || upstreamAddress != "http://127.0.0.1:8765" {
		log.Fatal("security boundary: gateway must listen on 127.0.0.1:8766 and proxy only to 127.0.0.1:8765")
	}
	if err := validateEndpoints(issuer, audience); err != nil {
		log.Fatalf("security boundary: %v", err)
	}

	upstream, err := url.Parse(upstreamAddress)
	if err != nil {
		log.Fatalf("bad upstream URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := authplane.NewClient(ctx, issuer)
	if err != nil {
		log.Fatalf("failed to discover Authplane and JWKS: %v", err)
	}
	defer client.Close()
	res, err := client.Resource(audience, resource.WithScopes("notes:read"))
	if err != nil {
		log.Fatalf("failed to create protected resource: %v", err)
	}
	adapter := authplanehttp.New(res)

	proxy := httputil.NewSingleHostReverseProxy(upstream)
	// Rewrite is used instead of Director; it discards inbound spoofed
	// X-Forwarded-* headers and allows us to strip the bearer from the upstream.
	proxy.Director = nil
	proxy.Rewrite = func(req *httputil.ProxyRequest) {
		req.SetURL(upstream)
		req.Out.Header.Del("Authorization")
		req.Out.Header.Del("Cookie")
		req.Out.Header.Del("Proxy-Authorization")
		req.Out.Header.Del("X-Forwarded-For")
		req.Out.Header.Del("X-Forwarded-Host")
		req.Out.Header.Del("X-Forwarded-Proto")
		req.Out.Host = upstream.Host
	}
	proxy.FlushInterval = -1 // Do not delay SSE messages.
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("upstream MCP connection failed (%s)", r.Method)
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}
	proxy.Transport = &http.Transport{
		Proxy:                 nil,
		MaxIdleConns:          8,
		MaxIdleConnsPerHost:   8,
		ResponseHeaderTimeout: 150 * time.Second,
	}

	mux := http.NewServeMux()
	mux.Handle(adapter.WellKnownPRMPath(), adapter.PRMHandler())
	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		claims := authplanehttp.ClaimsFromContext(r.Context())
		if claims == nil || claims.Sub() != subject {
			http.Error(w, "access denied", http.StatusForbidden)
			return
		}
		if r.URL.Path != "/mcp" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		proxy.ServeHTTP(w, r)
	})
	mux.Handle("/mcp", adapter.RequireScopes("notes:read")(protected))
	srv := &http.Server{
		Addr:              address,
		Handler:           adapter.Middleware()(mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	log.Printf("OAuth MCP gateway bound on %s; resource audience %s", address, audience)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
