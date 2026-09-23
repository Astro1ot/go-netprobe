param([string]$BaseUrl = 'http://localhost:8082')
$ErrorActionPreference = 'Stop'
Invoke-RestMethod "$BaseUrl/targets" | Format-Table
$taskBody = @{ target_ids = @('demo-http','demo-tcp','demo-failure','demo-timeout') } | ConvertTo-Json
Invoke-RestMethod "$BaseUrl/checks" -Method Post -ContentType 'application/json' -Body $taskBody | ConvertTo-Json -Depth 5
Invoke-RestMethod "$BaseUrl/stats" | ConvertTo-Json -Depth 5
Write-Host 'gRPC: go run ./cmd/probecli -targets demo-http,demo-tcp'
