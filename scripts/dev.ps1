param([ValidateSet('migrate','test','test-docker','generate','check','certs','grpc-check','grpc-check-docker')][string]$Action = 'test')

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$toolRoot = Join-Path (Split-Path -Parent $repoRoot) '.tools'
$portableGo = Join-Path $toolRoot 'go1.27.4/go/bin'
if (Test-Path -LiteralPath (Join-Path $portableGo 'go.exe')) {
    $env:PATH = $portableGo + ';' + (Join-Path $toolRoot 'bin') + ';' + $env:PATH
    $env:GOCACHE = Join-Path $toolRoot 'cache/go-build'
    $env:GOMODCACHE = Join-Path $toolRoot 'cache/go-mod'
    $env:GOBIN = Join-Path $toolRoot 'bin'
}
if (-not (Get-Command go -ErrorAction SilentlyContinue)) { throw 'Go no disponible.' }

Push-Location -LiteralPath $repoRoot
try {
    $localEnv = Join-Path $repoRoot '.env'
    if (Test-Path -LiteralPath $localEnv) {
        foreach ($line in (Get-Content -LiteralPath $localEnv)) {
            if ($line -match '^(POSTGRES_HOST|POSTGRES_PORT|POSTGRES_DB|POSTGRES_USER|POSTGRES_PASSWORD|POSTGRES_SSLMODE|GRPC_PORT|KAFKA_BROKERS|GRPC_TLS_CERT_FILE|GRPC_TLS_KEY_FILE|GRPC_TLS_CA_FILE|GRPC_HEALTH_CERT_FILE|GRPC_HEALTH_KEY_FILE|SESSION_TTL|SESSION_IDLE_TIMEOUT)=(.*)$') {
                [Environment]::SetEnvironmentVariable($matches[1], $matches[2], 'Process')
            }
        }
    }
    switch ($Action) {
        'certs' { & go run ./cmd/devcert }
        'grpc-check' { & go run ./cmd/grpc-check }
        'grpc-check-docker' {
            & docker --context desktop-linux build --quiet --target test -t billetera-ms-usuarios:test .
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
            $certDirectory = Join-Path $repoRoot '.local-certs'
            & docker --context desktop-linux run --rm --network billetera-usuarios-dev_default --mount "type=bind,source=$certDirectory\bff.crt,target=/certs/bff.crt,readonly" --mount "type=bind,source=$certDirectory\bff.key,target=/certs/bff.key,readonly" --mount "type=bind,source=$certDirectory\ca.crt,target=/certs/ca.crt,readonly" billetera-ms-usuarios:test go run ./cmd/grpc-check --address ms-usuarios:50051 --certs /certs
        }
        'migrate' { & go run ./cmd/migrate }
        'check' { & go vet ./...; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; & go build ./... }
        'generate' {
            $compiler = Join-Path $toolRoot 'protoc/bin/protoc.exe'
            if (-not (Test-Path -LiteralPath $compiler)) { throw 'protoc portable no disponible.' }
            & $compiler -I proto -I (Join-Path $toolRoot 'protoc/include') --go_out=. --go_opt=module=billetera.local/ms-usuarios --go-grpc_out=. --go-grpc_opt=module=billetera.local/ms-usuarios proto/billetera/usuarios/v1/usuarios.proto
        }
        { $_ -in @('test','test-docker') } {
            foreach ($key in @('POSTGRES_USER','POSTGRES_PASSWORD','POSTGRES_HOST','POSTGRES_PORT')) {
                if (-not [Environment]::GetEnvironmentVariable($key,'Process')) { throw "Falta $key para pruebas de integración." }
            }
            $testUser = [Uri]::EscapeDataString($env:POSTGRES_USER)
            $testPassword = [Uri]::EscapeDataString($env:POSTGRES_PASSWORD)
            if ($Action -eq 'test-docker') {
                $env:USUARIOS_TEST_DATABASE_URL = 'postgresql://' + $testUser + ':' + $testPassword + '@postgres:5432/billetera_usuarios_test?sslmode=disable'
                & docker --context desktop-linux build --quiet --target test -t billetera-ms-usuarios:test .
                if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
                $reports = Join-Path $repoRoot '.test-results'
                New-Item -ItemType Directory -Force -Path $reports | Out-Null
                & docker --context desktop-linux run --rm --network billetera-usuarios-dev_default --env USUARIOS_TEST_DATABASE_URL --mount "type=bind,source=$reports,target=/reports" billetera-ms-usuarios:test
                if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
                Copy-Item -LiteralPath (Join-Path $reports 'coverage.out') -Destination (Join-Path $repoRoot 'coverage.out')
            } else {
                $env:USUARIOS_TEST_DATABASE_URL = 'postgresql://' + $testUser + ':' + $testPassword + '@' + $env:POSTGRES_HOST + ':' + $env:POSTGRES_PORT + '/billetera_usuarios_test?sslmode=' + $env:POSTGRES_SSLMODE
                & go test '-coverpkg=./internal/...,./migrations' '-coverprofile=coverage.out' ./...
            }
        }
    }
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Remove-Item Env:USUARIOS_TEST_DATABASE_URL -ErrorAction SilentlyContinue
    Pop-Location
}
