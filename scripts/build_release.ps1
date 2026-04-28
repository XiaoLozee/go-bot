param(
    [string]$Version = "",
    [string[]]$Platforms = @(
        "windows/amd64",
        "windows/arm64",
        "linux/amd64",
        "linux/arm64",
        "darwin/amd64",
        "darwin/arm64"
    ),
    [string]$OutputRoot = "build/release",
    [string]$BinaryName = "go-bot",
    [switch]$BuildFrontend,
    [switch]$SkipArchive
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
$outputPath = Join-Path $projectRoot $OutputRoot
$distPath = Join-Path $outputPath "dist"
$packageOutputPath = Join-Path $outputPath "packages"
$checksumPath = Join-Path $packageOutputPath "SHA256SUMS.txt"

function Invoke-CheckedCommand {
    param(
        [Parameter(Mandatory = $true)][string]$FilePath,
        [Parameter(Mandatory = $true)][string[]]$Arguments,
        [Parameter(Mandatory = $true)][string]$WorkingDirectory
    )

    Push-Location $WorkingDirectory
    try {
        Write-Host "> $FilePath $($Arguments -join ' ')"
        & $FilePath @Arguments
        if ($LASTEXITCODE -ne 0) {
            throw "Command failed with exit code ${LASTEXITCODE}: $FilePath $($Arguments -join ' ')"
        }
    }
    finally {
        Pop-Location
    }
}

function Resolve-ReleaseVersion {
    param([string]$ExplicitVersion)

    $trimmed = $ExplicitVersion.Trim()
    if ($trimmed -ne "") {
        return $trimmed
    }

    $gitVersion = (& git -C $projectRoot describe --tags --always --dirty 2>$null)
    if ($LASTEXITCODE -eq 0 -and -not [string]::IsNullOrWhiteSpace($gitVersion)) {
        return $gitVersion.Trim()
    }

    return "dev-$(Get-Date -Format 'yyyyMMddHHmmss')"
}

function ConvertTo-SafeName {
    param([string]$Value)
    return ($Value -replace '[^A-Za-z0-9._-]', '-')
}

function Resolve-Platforms {
    param([string[]]$InputPlatforms)

    $items = @()
    foreach ($item in $InputPlatforms) {
        foreach ($part in ($item -split ',')) {
            $trimmed = $part.Trim()
            if ($trimmed -ne "") {
                $items += $trimmed
            }
        }
    }
    return $items
}

function Copy-IfExists {
    param(
        [Parameter(Mandatory = $true)][string]$Source,
        [Parameter(Mandatory = $true)][string]$Destination
    )

    if (-not (Test-Path -LiteralPath $Source)) {
        return
    }

    $parent = Split-Path -Parent $Destination
    if ($parent -ne "") {
        New-Item -ItemType Directory -Force -Path $parent | Out-Null
    }
    Copy-Item -LiteralPath $Source -Destination $Destination -Recurse -Force
}

function Copy-ReleaseAssets {
    param([string]$PackageDir)

    New-Item -ItemType Directory -Force -Path (Join-Path $PackageDir "configs") | Out-Null
    Copy-IfExists (Join-Path $projectRoot "README.md") (Join-Path $PackageDir "README.md")
    Copy-IfExists (Join-Path $projectRoot "LICENSE") (Join-Path $PackageDir "LICENSE")
    Copy-IfExists (Join-Path $projectRoot "ARCHITECTURE.md") (Join-Path $PackageDir "ARCHITECTURE.md")
    Copy-IfExists (Join-Path $projectRoot "SPEC.md") (Join-Path $PackageDir "SPEC.md")
    Copy-IfExists (Join-Path $projectRoot "contributing.md") (Join-Path $PackageDir "contributing.md")
    Copy-IfExists (Join-Path $projectRoot "go-bot.service") (Join-Path $PackageDir "go-bot.service")
    Copy-IfExists (Join-Path $projectRoot "configs/config.example.yml") (Join-Path $PackageDir "configs/config.example.yml")
    Copy-IfExists (Join-Path $projectRoot "configs/config.full.example.yml") (Join-Path $PackageDir "configs/config.full.example.yml")
    Copy-IfExists (Join-Path $projectRoot "docs") (Join-Path $PackageDir "docs")
}

function Invoke-GoBuildForPlatform {
    param(
        [Parameter(Mandatory = $true)][string]$GoOS,
        [Parameter(Mandatory = $true)][string]$GoArch,
        [Parameter(Mandatory = $true)][string]$OutputFile
    )

    $oldGoOS = $env:GOOS
    $oldGoArch = $env:GOARCH
    $oldCGOEnabled = $env:CGO_ENABLED

    try {
        $env:GOOS = $GoOS
        $env:GOARCH = $GoArch
        $env:CGO_ENABLED = "0"
        Invoke-CheckedCommand "go" @("build", "-trimpath", "-ldflags", "-s -w", "-o", $OutputFile, ".") $projectRoot
    }
    finally {
        if ($null -eq $oldGoOS) { Remove-Item Env:GOOS -ErrorAction SilentlyContinue } else { $env:GOOS = $oldGoOS }
        if ($null -eq $oldGoArch) { Remove-Item Env:GOARCH -ErrorAction SilentlyContinue } else { $env:GOARCH = $oldGoArch }
        if ($null -eq $oldCGOEnabled) { Remove-Item Env:CGO_ENABLED -ErrorAction SilentlyContinue } else { $env:CGO_ENABLED = $oldCGOEnabled }
    }
}

$releaseVersion = Resolve-ReleaseVersion $Version
$safeVersion = ConvertTo-SafeName $releaseVersion
$resolvedPlatforms = Resolve-Platforms $Platforms
$frontendDistIndex = Join-Path $projectRoot "internal/admin/webui/frontend/dist/index.html"

if ($BuildFrontend) {
    $frontendRoot = Join-Path $projectRoot "internal/admin/webui/frontend"
    Invoke-CheckedCommand "npm" @("ci") $frontendRoot
    Invoke-CheckedCommand "npm" @("run", "build") $frontendRoot
}

if (-not (Test-Path -LiteralPath $frontendDistIndex)) {
    throw "WebUI dist bundle not found. Run with -BuildFrontend or build internal/admin/webui/frontend first."
}

New-Item -ItemType Directory -Force -Path $distPath | Out-Null
New-Item -ItemType Directory -Force -Path $packageOutputPath | Out-Null
if (Test-Path -LiteralPath $checksumPath) {
    Remove-Item -LiteralPath $checksumPath -Force
}

$results = @()
foreach ($platform in $resolvedPlatforms) {
    $platformParts = $platform -split '/', 2
    if ($platformParts.Count -ne 2) {
        throw "Invalid platform: $platform. Expected format: goos/goarch"
    }

    $goos = $platformParts[0].Trim()
    $goarch = $platformParts[1].Trim()
    if ($goos -eq "" -or $goarch -eq "") {
        throw "Invalid platform: $platform. Expected format: goos/goarch"
    }

    $packageName = "$BinaryName-$safeVersion-$goos-$goarch"
    $packageDir = Join-Path $distPath $packageName
    if (Test-Path -LiteralPath $packageDir) {
        Remove-Item -LiteralPath $packageDir -Recurse -Force
    }
    New-Item -ItemType Directory -Force -Path $packageDir | Out-Null

    $binaryFileName = $BinaryName
    if ($goos -eq "windows") {
        $binaryFileName = "$BinaryName.exe"
    }
    $binaryPath = Join-Path $packageDir $binaryFileName

    Write-Host "Building $goos/$goarch -> $binaryPath"
    Invoke-GoBuildForPlatform $goos $goarch $binaryPath
    Copy-ReleaseAssets $packageDir

    $archivePath = ""
    $archiveSizeMB = 0
    if (-not $SkipArchive) {
        $archivePath = Join-Path $packageOutputPath "$packageName.zip"
        if (Test-Path -LiteralPath $archivePath) {
            Remove-Item -LiteralPath $archivePath -Force
        }
        $archiveItems = Get-ChildItem -LiteralPath $packageDir -Force
        Compress-Archive -LiteralPath $archiveItems.FullName -DestinationPath $archivePath
        $hash = Get-FileHash -LiteralPath $archivePath -Algorithm SHA256
        Add-Content -LiteralPath $checksumPath -Value "$($hash.Hash.ToLowerInvariant())  $(Split-Path -Leaf $archivePath)"
        $archiveSizeMB = [math]::Round(((Get-Item -LiteralPath $archivePath).Length / 1MB), 2)
    }

    $results += [pscustomobject]@{
        Platform = "$goos/$goarch"
        Binary   = $binaryPath
        Archive  = $archivePath
        SizeMB   = $archiveSizeMB
    }
}

$results | Format-Table -AutoSize
if (-not $SkipArchive) {
    Write-Host "Checksums: $checksumPath"
}
