// demo-target provides deterministic healthy, failing and slow HTTP endpoints.
package main

import (
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ok", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); _, _ = w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /fail", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "intentional demo failure", 503) })
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			w.WriteHeader(200)
		}
	})
	address := os.Getenv("DEMO_ADDR")
	if address == "" {
		address = "127.0.0.1:8085"
	}
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 2 * time.Second, WriteTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}
