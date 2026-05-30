param(
    [string]$Version = ""
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$packageRoot = Join-Path $repoRoot "packaging\\ugreen-native-app"
$frontendProject = Join-Path $repoRoot "frontend\\ugreen-app"
$targets = @(
    @{ Arch = "amd64"; Output = Join-Path $packageRoot "rootfs_amd64\\bin\\nasnotify" },
    @{ Arch = "arm64"; Output = Join-Path $packageRoot "rootfs_arm64\\bin\\nasnotify" }
)

$originalEnv = @{
    GOOS = $env:GOOS
    GOARCH = $env:GOARCH
    CGO_ENABLED = $env:CGO_ENABLED
}

try {
    Push-Location $repoRoot

    if (Test-Path $frontendProject) {
        npm.cmd --prefix $frontendProject run build
        if ($LASTEXITCODE -ne 0) {
            throw "frontend build failed"
        }
    }

    foreach ($target in $targets) {
        New-Item -ItemType Directory -Force -Path (Split-Path -Parent $target.Output) | Out-Null

        $env:GOOS = "linux"
        $env:GOARCH = $target.Arch
        $env:CGO_ENABLED = "0"

        $ldflags = "-s -w"
        if ($Version) {
            $ldflags = "$ldflags -X main.Version=$Version"
        }

        go build -buildvcs=false -trimpath -ldflags $ldflags -o $target.Output ./cmd/nasnotify
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed for $($target.Arch)"
        }
    }
}
finally {
    Pop-Location

    foreach ($name in $originalEnv.Keys) {
        if ($null -eq $originalEnv[$name]) {
            Remove-Item "Env:$name" -ErrorAction SilentlyContinue
        } else {
            Set-Item "Env:$name" $originalEnv[$name]
        }
    }
}

Write-Host "UGREEN packaging binaries updated in $packageRoot"
