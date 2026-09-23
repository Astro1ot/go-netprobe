package probe

import (
	"context"
	"net"
	"net/http"
	"time"
)

func NetworkChecker() Checker {
	transport := &http.Transport{Proxy: nil, MaxIdleConns: 32, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 3 * time.Second}
	client := &http.Client{Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	dialer := net.Dialer{}
	return func(ctx context.Context, target Target) Result {
		if target.Protocol == "tcp" {
			conn, err := dialer.DialContext(ctx, "tcp", target.Address)
			if err != nil {
				return Result{Error: err.Error()}
			}
			conn.Close()
			return Result{Up: true}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.Address, nil)
		if err != nil {
			return Result{Error: err.Error()}
		}
		response, err := client.Do(request)
		if err != nil {
			return Result{Error: err.Error()}
		}
		// Health is measured at response headers; bodies are never buffered in memory.
		response.Body.Close()
		result := Result{StatusCode: response.StatusCode, Up: response.StatusCode >= 200 && response.StatusCode < 400}
		if !result.Up {
			result.Error = http.StatusText(response.StatusCode)
		}
		return result
	}
}
