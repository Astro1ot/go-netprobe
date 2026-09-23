package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/Astro1ot/go-netprobe/internal/probe"
)

func HTTP(engine *probe.Engine) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /targets", func(w http.ResponseWriter, r *http.Request) { write(w, 200, engine.Targets()) })
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) { write(w, 200, engine.Stats()) })
	mux.HandleFunc("POST /checks", func(w http.ResponseWriter, r *http.Request) {
		var input *struct {
			TargetIDs []string `json:"target_ids"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil || input == nil {
			write(w, 400, map[string]string{"error": "invalid JSON (maximum 16 KiB)"})
			return
		}
		if decoder.Decode(new(any)) != io.EOF {
			write(w, 400, map[string]string{"error": "exactly one JSON object required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		results, err := engine.Check(ctx, input.TargetIDs)
		if err != nil {
			code := 500
			switch {
			case errors.Is(err, probe.ErrInvalid):
				code = 400
			case errors.Is(err, probe.ErrBusy):
				code = 429
				w.Header().Set("Retry-After", "1")
			case errors.Is(err, context.DeadlineExceeded):
				code = 504
			case errors.Is(err, context.Canceled):
				code = 408
			}
			write(w, code, map[string]string{"error": err.Error()})
			return
		}
		write(w, 200, map[string]any{"results": results})
	})
	return mux
}
func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
