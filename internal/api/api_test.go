package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	probev1 "github.com/Astro1ot/go-netprobe/gen/probe/v1"
	"github.com/Astro1ot/go-netprobe/internal/probe"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func testEngine(t *testing.T) *probe.Engine {
	t.Helper()
	e, err := probe.New([]probe.Target{{ID: "demo", Protocol: "tcp", Address: "localhost:80"}}, probe.Options{Concurrency: 1, MaxBatches: 2, Timeout: time.Second}, func(context.Context, probe.Target) probe.Result { return probe.Result{Up: true} })
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestHTTPValidationAndResults(t *testing.T) {
	handler := HTTP(testEngine(t))
	for _, tc := range []struct {
		body string
		code int
	}{{`{"target_ids":["demo"]}`, 200}, {`{}`, 200}, {`{"target_ids":["missing"]}`, 400}, {`{"target_ids":["demo","demo"]}`, 400}, {`{"address":"http://arbitrary-host"}`, 400}, {`{} {}`, 400}, {`{`, 400}, {strings.Repeat("x", 16385), 400}} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("POST", "/checks", strings.NewReader(tc.body)))
		if response.Code != tc.code {
			t.Fatalf("%.80s => %d %s", tc.body, response.Code, response.Body.String())
		}
		if tc.code == 200 {
			var body struct {
				Results []probe.Result `json:"results"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || len(body.Results) != 1 || !body.Results[0].Up {
				t.Fatalf("invalid response: %s", response.Body.String())
			}
		}
	}
}
func TestGRPCWireRoundTripAndErrors(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	probev1.RegisterProbeServiceServer(server, &GRPC{Engine: testEngine(t)})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet", grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	client := probev1.NewProbeServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	response, err := client.Check(ctx, &probev1.CheckRequest{TargetIds: []string{"demo"}})
	if err != nil || len(response.GetResults()) != 1 || !response.Results[0].Up {
		t.Fatalf("RPC result=%v error=%v", response, err)
	}
	_, err = client.Check(ctx, &probev1.CheckRequest{TargetIds: []string{"missing"}})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("got %v", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Check(cancelled, &probev1.CheckRequest{})
	if status.Code(err) != codes.Canceled {
		t.Fatalf("got %v", err)
	}
}
