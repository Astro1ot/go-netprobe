package probe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNetworkHTTPStatusesRedirectAndDeadline(t *testing.T) {
	var redirected atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("/fail", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) })
	mux.HandleFunc("/redirect", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/destination", 302) })
	mux.HandleFunc("/destination", func(w http.ResponseWriter, r *http.Request) { redirected.Add(1) })
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	server := httptest.NewServer(mux)
	defer server.Close()
	checker := NetworkChecker()
	for _, tc := range []struct {
		path string
		code int
		up   bool
	}{{"/ok", 204, true}, {"/fail", 503, false}, {"/redirect", 302, true}} {
		result := checker(context.Background(), Target{ID: "http", Protocol: "http", Address: server.URL + tc.path})
		if result.StatusCode != tc.code || result.Up != tc.up {
			t.Fatalf("%s: %+v", tc.path, result)
		}
	}
	if redirected.Load() != 0 {
		t.Fatal("checker followed a redirect")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	result := checker(ctx, Target{ID: "slow", Protocol: "http", Address: server.URL + "/slow"})
	if result.Up || result.Error == "" {
		t.Fatalf("deadline not applied: %+v", result)
	}
}
func TestNetworkTCPReachableAndClosedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			conn.Close()
		}
	}()
	checker := NetworkChecker()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if result := checker(ctx, Target{"tcp", "tcp", address}); !result.Up {
		t.Fatalf("TCP connect failed: %+v", result)
	}
	<-done
	listener.Close()
	if result := checker(ctx, Target{"tcp", "tcp", address}); result.Up || strings.TrimSpace(result.Error) == "" {
		t.Fatalf("closed port reported healthy: %+v", result)
	}
}
