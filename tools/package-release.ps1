[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet("server", "daemon")]
    [string]$Component,

    [Parameter(Mandatory = $true)]
    [ValidateSet("linux", "windows")]
    [string]$TargetOS,

    [Parameter(Mandatory = $true)]
    [ValidateSet("amd64", "arm64")]
    [string]$TargetArch,

    [Parameter(Mandatory = $true)]
    [string]$Version,

    [string]$RepositoryRoot,
    [string]$PwaDirectory,
    [string]$OutputDirectory = ".release"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

if ([string]::IsNullOrWhiteSpace($RepositoryRoot)) {
    $RepositoryRoot = Split-Path -Parent $PSScriptRoot
}
$RepositoryRoot = [System.IO.Path]::GetFullPath($RepositoryRoot)

$normalizedVersion = $Version.Trim()
if ($normalizedVersion.StartsWith("v", [System.StringComparison]::OrdinalIgnoreCase)) {
    $normalizedVersion = $normalizedVersion.Substring(1)
}
if ($normalizedVersion -notmatch '^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$') {
    throw "Version must be a semantic version such as 0.2.4 or v0.2.4."
}

if ($Component -eq "server" -and $TargetOS -ne "linux") {
    throw "The supported Server release target is Linux."
}
if ($TargetOS -eq "windows" -and $TargetArch -ne "amd64") {
    throw "The supported Windows release target is amd64."
}

if (-not [System.IO.Path]::IsPathRooted($OutputDirectory)) {
    $OutputDirectory = Join-Path $RepositoryRoot $OutputDirectory
}
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null

if ($Component -eq "server") {
    if ([string]::IsNullOrWhiteSpace($PwaDirectory)) {
        throw "Server packages require -PwaDirectory."
    }
    if (-not [System.IO.Path]::IsPathRooted($PwaDirectory)) {
        $PwaDirectory = Join-Path $RepositoryRoot $PwaDirectory
    }
    $PwaDirectory = [System.IO.Path]::GetFullPath($PwaDirectory)
    if (-not (Test-Path -LiteralPath (Join-Path $PwaDirectory "index.html") -PathType Leaf)) {
        throw "PWA build output is missing index.html: $PwaDirectory"
    }
}

$moduleDirectory = Join-Path $RepositoryRoot $Component
$commandPath = if ($Component -eq "server") { "./cmd/server" } else { "./cmd/daemon" }
$versionSymbol = "github.com/nekonest/$Component/internal/buildinfo.Version"
$binaryName = "nekonest-$Component"
if ($TargetOS -eq "windows") {
    $binaryName += ".exe"
}

$assetBaseName = "nekonest-$Component-$TargetOS-$TargetArch"
$assetExtension = if ($TargetOS -eq "windows") { ".zip" } else { ".tar.gz" }
$assetPath = Join-Path $OutputDirectory ($assetBaseName + $assetExtension)
$stagingDirectory = Join-Path $OutputDirectory (".staging-" + [Guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Path $stagingDirectory | Out-Null

$previousGoos = $env:GOOS
$previousGoarch = $env:GOARCH
$previousCgo = $env:CGO_ENABLED

try {
    $env:GOOS = $TargetOS
    $env:GOARCH = $TargetArch
    $env:CGO_ENABLED = "0"

    $binaryPath = Join-Path $stagingDirectory $binaryName
    $ldflags = "-s -w -X $versionSymbol=$normalizedVersion"

    Push-Location $moduleDirectory
    try {
        & go build -trimpath -buildvcs=true "-ldflags=$ldflags" -o $binaryPath $commandPath
        if ($LASTEXITCODE -ne 0) {
            throw "go build failed for $Component $TargetOS/$TargetArch."
        }
    }
    finally {
        Pop-Location
    }

    Copy-Item -LiteralPath (Join-Path $RepositoryRoot "LICENSE") -Destination $stagingDirectory
    Copy-Item -LiteralPath (Join-Path $RepositoryRoot "LICENSE_zh") -Destination $stagingDirectory
    Copy-Item -LiteralPath (Join-Path $RepositoryRoot "README.md") -Destination $stagingDirectory
    Copy-Item -LiteralPath (Join-Path $RepositoryRoot "README.zh-CN.md") -Destination $stagingDirectory
    Set-Content -LiteralPath (Join-Path $stagingDirectory "VERSION") -Value $normalizedVersion -NoNewline

    if ($Component -eq "server") {
        Copy-Item -LiteralPath $PwaDirectory -Destination (Join-Path $stagingDirectory "pwa-dist") -Recurse
    }

    $currentOS = if ($IsWindows) { "windows" } elseif ($IsLinux) { "linux" } else { "other" }
    $currentArch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()) {
        "x64" { "amd64" }
        "arm64" { "arm64" }
        default { "other" }
    }
    if ($currentOS -eq $TargetOS -and $currentArch -eq $TargetArch) {
        $reportedVersion = (& $binaryPath -version | Out-String).Trim()
        if ($LASTEXITCODE -ne 0 -or $reportedVersion -ne $normalizedVersion) {
            throw "Packaged binary reported '$reportedVersion', expected '$normalizedVersion'."
        }
    }

    if (Test-Path -LiteralPath $assetPath) {
        Remove-Item -LiteralPath $assetPath -Force
    }
    if ($TargetOS -eq "windows") {
        Compress-Archive -Path (Join-Path $stagingDirectory "*") -DestinationPath $assetPath -CompressionLevel Optimal
    }
    else {
        & tar -czf $assetPath -C $stagingDirectory .
        if ($LASTEXITCODE -ne 0) {
            throw "tar failed while creating $assetPath."
        }
    }

    Write-Output $assetPath
}
finally {
    if ($null -eq $previousGoos) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $previousGoos }
    if ($null -eq $previousGoarch) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $previousGoarch }
    if ($null -eq $previousCgo) { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $previousCgo }
    if (Test-Path -LiteralPath $stagingDirectory) {
        Remove-Item -LiteralPath $stagingDirectory -Recurse -Force
    }
}
