$ErrorActionPreference = 'Stop'
# protoc 36.2 must be installed on PATH. Runtime builds do not require protoc:
# generated .pb.go files are committed under gen/.
go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12
if ($LASTEXITCODE -ne 0) { throw 'protoc-gen-go installation failed' }
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
if ($LASTEXITCODE -ne 0) { throw 'protoc-gen-go-grpc installation failed' }
$taskProtoBin = go env GOBIN
if (-not $taskProtoBin) { $taskProtoBin = Join-Path (go env GOPATH) 'bin' }
$env:PATH = "$taskProtoBin;$env:PATH"
protoc -I api --go_out=. --go_opt=module=github.com/Astro1ot/go-netprobe --go-grpc_out=. --go-grpc_opt=module=github.com/Astro1ot/go-netprobe probe/v1/probe.proto
if ($LASTEXITCODE -ne 0) { throw 'Protocol Buffers generation failed' }
