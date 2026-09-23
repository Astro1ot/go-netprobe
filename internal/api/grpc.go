package api

import (
	"context"
	"errors"
	"time"

	probev1 "github.com/Astro1ot/go-netprobe/gen/probe/v1"
	"github.com/Astro1ot/go-netprobe/internal/probe"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GRPC struct {
	probev1.UnimplementedProbeServiceServer
	Engine *probe.Engine
}

func (s *GRPC) Check(ctx context.Context, request *probev1.CheckRequest) (*probev1.CheckResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	results, err := s.Engine.Check(ctx, request.GetTargetIds())
	if err != nil {
		switch {
		case errors.Is(err, probe.ErrInvalid):
			return nil, status.Error(codes.InvalidArgument, err.Error())
		case errors.Is(err, probe.ErrBusy):
			return nil, status.Error(codes.ResourceExhausted, err.Error())
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			return nil, status.FromContextError(err).Err()
		default:
			return nil, status.Error(codes.Internal, "check failed")
		}
	}
	response := &probev1.CheckResponse{Results: make([]*probev1.Result, 0, len(results))}
	for _, result := range results {
		response.Results = append(response.Results, &probev1.Result{TargetId: result.TargetID, Protocol: result.Protocol, Up: result.Up, LatencyMs: result.LatencyMS, StatusCode: int32(result.StatusCode), Error: result.Error})
	}
	return response, nil
}
