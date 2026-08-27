param (
    [switch]$BuildWeb = $false,
    [switch]$LiveMode = $false,
    [int]$Port = 8080
)

$RootDir = Resolve-Path "$PSScriptRoot\.."
Set-Location $RootDir
& "$RootDir\antigravity.ps1" -BuildWeb:$BuildWeb -LiveMode:$LiveMode -Port:$Port
