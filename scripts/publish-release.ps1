param(
    [Parameter(Mandatory = $true)]
    [string]$Tag,

    [Parameter(Mandatory = $true)]
    [int]$Build,

    [string]$Repository = "autunn/ugreen-nas-wechat-notifier",
    [string]$UgcliPath = "",
    [string]$Version = "",
    [string]$Proxy = ""
)

$ErrorActionPreference = "Stop"

$repoRoot = Split-Path -Parent $PSScriptRoot
$packageRoot = Join-Path $repoRoot "packaging\ugreen-native-app"
$buildDir = Join-Path $packageRoot "build_dir"
$upkDir = Join-Path $packageRoot "build_dir\pkgs\upk"
$startLocation = (Get-Location).Path

if (-not $Version) {
    $Version = $Tag
}

if (-not $UgcliPath) {
    $UgcliPath = Join-Path $repoRoot "tools\ugcli\ugcli-v1.1.0.12-windows-amd64.exe"
}

if (-not (Test-Path -LiteralPath $UgcliPath)) {
    throw "ugcli not found: $UgcliPath"
}

if (-not (Get-Command gh -ErrorAction SilentlyContinue) -and -not (Test-Path -LiteralPath "C:\Program Files\GitHub CLI\gh.exe")) {
    throw "GitHub CLI not found. Install gh first."
}

$gh = "gh"
if (-not (Get-Command gh -ErrorAction SilentlyContinue)) {
    $gh = "C:\Program Files\GitHub CLI\gh.exe"
}

$oldHttpProxy = $env:HTTP_PROXY
$oldHttpsProxy = $env:HTTPS_PROXY

try {
    if ($Proxy) {
        $env:HTTP_PROXY = $Proxy
        $env:HTTPS_PROXY = $Proxy
    }

    Push-Location $repoRoot
    powershell -ExecutionPolicy Bypass -File (Join-Path $repoRoot "scripts\build-ugreen-native-app.ps1") -Version $Version
    Pop-Location

    if (Test-Path -LiteralPath $buildDir) {
        Remove-Item -LiteralPath $buildDir -Recurse -Force
    }

    Push-Location $packageRoot
    & $UgcliPath pack --build $Build
    if ($LASTEXITCODE -ne 0) {
        throw "ugcli pack failed"
    }
    Pop-Location

    $assets = Get-ChildItem -LiteralPath $upkDir -Filter "*.upk" | Sort-Object Name
    if (-not $assets) {
        throw "No UPK files found in $upkDir"
    }

    $checksumPath = Join-Path $upkDir "SHA256SUMS.txt"
    $lines = foreach ($asset in $assets) {
        $hash = Get-FileHash -LiteralPath $asset.FullName -Algorithm SHA256
        "$($hash.Hash.ToLowerInvariant())  $($asset.Name)"
    }
    Set-Content -LiteralPath $checksumPath -Value $lines -Encoding UTF8

    $notes = @"
UGREEN NAS WeChat Notifier $Tag

Assets:
$($assets.Name -join "`n")

Checksums are provided in SHA256SUMS.txt.
"@

    $releaseExists = $false
    try {
        & $gh release view $Tag --repo $Repository | Out-Null
        $releaseExists = $true
    } catch {
        $releaseExists = $false
    }

    $assetPaths = @($assets.FullName) + @($checksumPath)

    if ($releaseExists) {
        & $gh release upload $Tag @assetPaths --repo $Repository --clobber
    } else {
        & $gh release create $Tag @assetPaths --repo $Repository --target main --title $Tag --notes $notes
    }

    Write-Host "Release published: https://github.com/$Repository/releases/tag/$Tag"
}
finally {
    if ($oldHttpProxy) {
        $env:HTTP_PROXY = $oldHttpProxy
    } else {
        Remove-Item Env:HTTP_PROXY -ErrorAction SilentlyContinue
    }

    if ($oldHttpsProxy) {
        $env:HTTPS_PROXY = $oldHttpsProxy
    } else {
        Remove-Item Env:HTTPS_PROXY -ErrorAction SilentlyContinue
    }

    Set-Location -LiteralPath $startLocation
}
