param(
    [string]$BinaryPath,
    [string]$Arch,
    [string]$ArgFlag,
    [string]$ProjectRoot,
    [string]$BinDir,
    [string]$AppName,
    [string]$BundleOpenClaw,
    [string]$ExtraSkills
)

$makensisArgs = @(
    "-DARG_WAILS_${ArgFlag}_BINARY=$BinaryPath"
)

if ($BundleOpenClaw -eq "true") {
    $runtimeZip = Join-Path $ProjectRoot "build\openclaw-runtime\windows-${Arch}.zip"
    $makensisArgs += "-DARG_OPENCLAW_RUNTIME=$runtimeZip"
    $makensisArgs += "-DARG_OPENCLAW_RUNTIME_TARGET=windows-${Arch}"
    $makensisArgs += "-DBUNDLE_OPENCLAW=1"
}

if (-not [string]::IsNullOrEmpty($ExtraSkills)) {
    $makensisArgs += "-DARG_EXTRASKILLS=$ExtraSkills"
}

$nsisDir = Join-Path $ProjectRoot "build\windows\nsis"
Push-Location $nsisDir
try {
    & makensis @makensisArgs "project.nsi"
} finally {
    Pop-Location
}
