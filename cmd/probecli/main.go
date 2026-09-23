package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	probev1 "github.com/Astro1ot/go-netprobe/gen/probe/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protojson"
)

func main() {
	address := flag.String("address", "127.0.0.1:9092", "gRPC server address")
	ids := flag.String("targets", "", "comma-separated target IDs; empty means all")
	timeout := flag.Duration("timeout", 10*time.Second, "request deadline")
	flag.Parse()
	if err := run(*address, *ids, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(address, ids string, timeout time.Duration) error {
	// Local learning deployment; production requires TLS and authentication.
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()
	request := &probev1.CheckRequest{}
	if ids != "" {
		for _, id := range strings.Split(ids, ",") {
			request.TargetIds = append(request.TargetIds, strings.TrimSpace(id))
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := probev1.NewProbeServiceClient(conn).Check(ctx, request)
	if err != nil {
		return err
	}
	data, err := (protojson.MarshalOptions{Multiline: true, Indent: "  ", EmitUnpopulated: true, UseProtoNames: true}).Marshal(result)
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
