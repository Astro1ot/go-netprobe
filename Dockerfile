FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/netprobe ./cmd/netprobe && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/probecli ./cmd/probecli && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/demo-target ./cmd/demo-target

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /
COPY configs/targets.docker.json /etc/netprobe/targets.json
EXPOSE 8080 9090
ENTRYPOINT ["/netprobe"]
CMD ["-config", "/etc/netprobe/targets.json"]
