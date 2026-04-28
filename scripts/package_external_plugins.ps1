param(
    [string]$SourceRoot = "plugins",
    [string]$OutputRoot = "build/plugin-packages",
    [string]$StageRoot = "build/plugin-packages/_stage",
    [string]$TargetOS = "",
    [string]$TargetArch = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$projectRoot = Split-Path -Parent $PSScriptRoot
$sourcePath = Join-Path $projectRoot $SourceRoot
$outputPath = Join-Path $projectRoot $OutputRoot
$stagePath = Join-Path $projectRoot $StageRoot
$commonSourcePath = Join-Path $sourcePath "_common"

if (-not (Test-Path -LiteralPath $sourcePath)) {
    throw "Source directory not found: $sourcePath"
}
if (-not (Test-Path -LiteralPath $commonSourcePath)) {
    throw "Common runtime directory not found: $commonSourcePath"
}

if (-not [string]::IsNullOrWhiteSpace($TargetOS) -or -not [string]::IsNullOrWhiteSpace($TargetArch)) {
    Write-Warning "TargetOS/TargetArch are ignored in Python-first packaging mode."
}

New-Item -ItemType Directory -Force -Path $outputPath | Out-Null
if (Test-Path -LiteralPath $stagePath) {
    Remove-Item -LiteralPath $stagePath -Recurse -Force
}
New-Item -ItemType Directory -Force -Path $stagePath | Out-Null

$pluginDirs = Get-ChildItem -LiteralPath $sourcePath -Directory |
    Where-Object {
        $_.Name -notin @('_common', '.bak') -and (
            (Test-Path -LiteralPath (Join-Path $_.FullName 'plugin.yaml')) -or
            (Test-Path -LiteralPath (Join-Path $_.FullName 'plugin.yml'))
        )
    } |
    Sort-Object Name

if (-not $pluginDirs) {
    throw "No plugin directories found under: $sourcePath"
}

$results = @()
foreach ($pluginDir in $pluginDirs) {
    $stagePluginPath = Join-Path $stagePath $pluginDir.Name
    if (Test-Path -LiteralPath $stagePluginPath) {
        Remove-Item -LiteralPath $stagePluginPath -Recurse -Force
    }

    Copy-Item -LiteralPath $pluginDir.FullName -Destination $stagePluginPath -Recurse
    Get-ChildItem -LiteralPath $stagePluginPath -Recurse -Directory |
        Where-Object { $_.Name -in @('.venv', 'venv') } |
        Remove-Item -Recurse -Force
    Get-ChildItem -LiteralPath $stagePluginPath -Recurse -Directory -Filter '__pycache__' |
        Remove-Item -Recurse -Force
    Get-ChildItem -LiteralPath $stagePluginPath -Recurse -File |
        Where-Object { $_.Extension -in @('.pyc', '.pyo') } |
        Remove-Item -Force

    $embeddedCommonPath = Join-Path $stagePluginPath '_common'
    if (Test-Path -LiteralPath $embeddedCommonPath) {
        Remove-Item -LiteralPath $embeddedCommonPath -Recurse -Force
    }
    Copy-Item -LiteralPath $commonSourcePath -Destination $embeddedCommonPath -Recurse
    Get-ChildItem -LiteralPath $embeddedCommonPath -Recurse -Directory -Filter '__pycache__' |
        Remove-Item -Recurse -Force
    Get-ChildItem -LiteralPath $embeddedCommonPath -Recurse -File |
        Where-Object { $_.Extension -in @('.pyc', '.pyo') } |
        Remove-Item -Force

    $zipPath = Join-Path $outputPath ($pluginDir.Name + '.zip')
    if (Test-Path -LiteralPath $zipPath) {
        Remove-Item -LiteralPath $zipPath -Force
    }
    Compress-Archive -LiteralPath $stagePluginPath -DestinationPath $zipPath

    $results += [pscustomobject]@{
        Plugin = $pluginDir.Name
        Runtime = 'python'
        Zip    = $zipPath
        SizeKB = [math]::Round(((Get-Item -LiteralPath $zipPath).Length / 1KB), 2)
    }
}

$results | Format-Table -AutoSize
