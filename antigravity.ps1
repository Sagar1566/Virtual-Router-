<#
.SYNOPSIS
    Antigravity Runner Script for Ubuntu Router (Windows PowerShell)
.DESCRIPTION
    Automates building the React frontend, compiling the Go backend,
    and launching Ubuntu Router in dry-run mode for local development.
#>

param (
    [switch]$BuildWeb = $false,
    [switch]$LiveMode = $false,
    [int]$Port = 8080
)

$ErrorActionPreference = "Stop"

function Write-Info($msg) { Write-Host "[+] $msg" -ForegroundColor Green }
function Write-Warn($msg) { Write-Host "[!] $msg" -ForegroundColor Yellow }
function Write-Err($msg)  { Write-Host "[X] $msg" -ForegroundColor Red }

# 1. Ensure Go is available
$env:PATH += ";C:\Program Files\Go\bin"
if (-not (Get-Command "go" -ErrorAction SilentlyContinue)) {
    Write-Err "Go executable not found. Please install Go or add C:\Program Files\Go\bin to PATH."
    exit 1
}
Write-Info "Found Go: $(go version)"

# 2. Ensure Node/npm is available
if (-not (Get-Command "npm" -ErrorAction SilentlyContinue)) {
    Write-Err "npm executable not found. Please install Node.js."
    exit 1
}
Write-Info "Found Node/npm: $(node --version) / npm $(npm --version)"

$RootDir = $PSScriptRoot
Set-Location $RootDir

# 3. Build Web Frontend if missing or requested
$WebDist = Join-Path $RootDir "web\dist"
if (-not (Test-Path $WebDist) -or $BuildWeb) {
    Write-Info "Building Web Frontend..."
    Set-Location (Join-Path $RootDir "web")
    npm install
    npm run build
    Set-Location $RootDir
} else {
    Write-Info "Web Frontend dist folder found."
}

# 4. Build Go Backend
Write-Info "Building Go Backend (ubuntu-router.exe)..."
$env:CGO_ENABLED = "0"
go build -o ubuntu-router.exe ./cmd/ubuntu-router
if ($LASTEXITCODE -ne 0) {
    Write-Err "Go build failed!"
    exit 1
}
Write-Info "Backend binary built successfully!"

# 5. Launch Router
$RunArgs = @()
if (-not $LiveMode) {
    $RunArgs += "--dry-run"
}

Write-Info "Starting Ubuntu Router server..."
Write-Host "========================================" -ForegroundColor Cyan
Write-Host "  Ubuntu Router (Antigravity Runner)   " -ForegroundColor Cyan
Write-Host "  URL: http://localhost:$Port         " -ForegroundColor Cyan
Write-Host "  Mode: $(if ($LiveMode) { 'LIVE' } else { 'DRY-RUN' })" -ForegroundColor Cyan
Write-Host "========================================" -ForegroundColor Cyan

& ".\ubuntu-router.exe" @RunArgs
