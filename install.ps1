[CmdletBinding()]
param(
    [string]$Version,
    [string]$InstallDir,
    [string]$Repo,
    [string]$BaseUrl,
    [switch]$InstallCodexSkill,
    [switch]$InstallClaudeSkill,
    [switch]$InstallGrokSkill
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

function Write-InstallerLog([string]$Message) {
    Write-Host "[planmaxx installer] $Message"
}

function Resolve-InstallerBoolean([string]$Name, [switch]$SwitchValue, [string]$EnvironmentValue) {
    if ($SwitchValue.IsPresent) {
        return $true
    }
    if ([string]::IsNullOrWhiteSpace($EnvironmentValue)) {
        return $false
    }
    switch ($EnvironmentValue.Trim().ToLowerInvariant()) {
        { $_ -in @('1', 'true', 'yes') } { return $true }
        { $_ -in @('0', 'false', 'no') } { return $false }
        default { throw "invalid $Name value: $EnvironmentValue (expected 1/0, true/false, yes/no)" }
    }
}

function Add-PlanMaxxUserPath([string]$Directory) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @()
    if (-not [string]::IsNullOrWhiteSpace($userPath)) {
        $entries = $userPath.Split(';', [StringSplitOptions]::RemoveEmptyEntries)
    }
    foreach ($entry in $entries) {
        if ([string]::Equals($entry.TrimEnd('\'), $Directory.TrimEnd('\'), [StringComparison]::OrdinalIgnoreCase)) {
            return $false
        }
    }
    $updated = if ([string]::IsNullOrWhiteSpace($userPath)) { $Directory } else { "$userPath;$Directory" }
    [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
    return $true
}

if ([string]::IsNullOrWhiteSpace($Repo)) {
    $Repo = if ([string]::IsNullOrWhiteSpace($env:PLANMAXX_REPO)) { 'AlhasanIQ/planmaxx' } else { $env:PLANMAXX_REPO }
}
if ([string]::IsNullOrWhiteSpace($Version)) {
    $Version = if ([string]::IsNullOrWhiteSpace($env:PLANMAXX_VERSION)) { 'latest' } else { $env:PLANMAXX_VERSION }
}
if ([string]::IsNullOrWhiteSpace($InstallDir)) {
    $InstallDir = if (-not [string]::IsNullOrWhiteSpace($env:PLANMAXX_INSTALL_DIR)) {
        $env:PLANMAXX_INSTALL_DIR
    } elseif (-not [string]::IsNullOrWhiteSpace($env:LOCALAPPDATA)) {
        Join-Path $env:LOCALAPPDATA 'PlanMaxx\bin'
    } else {
        throw 'LOCALAPPDATA is unavailable; pass -InstallDir or set PLANMAXX_INSTALL_DIR'
    }
}
if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
    $BaseUrl = $env:PLANMAXX_BASE_URL
}

$wantCodexSkill = Resolve-InstallerBoolean 'PLANMAXX_INSTALL_CODEX_SKILL' $InstallCodexSkill $env:PLANMAXX_INSTALL_CODEX_SKILL
$wantClaudeSkill = Resolve-InstallerBoolean 'PLANMAXX_INSTALL_CLAUDE_SKILL' $InstallClaudeSkill $env:PLANMAXX_INSTALL_CLAUDE_SKILL
$wantGrokSkill = Resolve-InstallerBoolean 'PLANMAXX_INSTALL_GROK_SKILL' $InstallGrokSkill $env:PLANMAXX_INSTALL_GROK_SKILL

$architectureName = [Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
$architecture = switch ($architectureName) {
    'x64' { 'amd64' }
    'arm64' { 'arm64' }
    default { throw "unsupported Windows architecture: $architectureName (supported: amd64, arm64)" }
}

if ($Version -eq 'latest') {
    $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest"
    $tag = [string]$release.tag_name
    if ([string]::IsNullOrWhiteSpace($tag)) {
        throw "could not resolve the latest release tag for $Repo"
    }
} else {
    $tag = $Version
}
$versionWithoutV = $tag.TrimStart('v')
$asset = "planmaxx_${versionWithoutV}_windows_${architecture}.tar.gz"
$releaseBase = if ([string]::IsNullOrWhiteSpace($BaseUrl)) {
    "https://github.com/$Repo/releases/download/$tag"
} else {
    $BaseUrl.TrimEnd('/')
}

$temporaryDirectory = Join-Path ([IO.Path]::GetTempPath()) ("planmaxx-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $temporaryDirectory | Out-Null
try {
    $archive = Join-Path $temporaryDirectory $asset
    $checksums = Join-Path $temporaryDirectory 'checksums.txt'
    Write-InstallerLog "Installing $Repo $tag for windows/$architecture"
    Invoke-WebRequest -UseBasicParsing -Uri "$releaseBase/$asset" -OutFile $archive
    Invoke-WebRequest -UseBasicParsing -Uri "$releaseBase/checksums.txt" -OutFile $checksums

    $escapedAsset = [Regex]::Escape($asset)
    $checksumLine = Get-Content -LiteralPath $checksums | Where-Object { $_ -match "^[0-9a-fA-F]{64}\s+\*?$escapedAsset$" } | Select-Object -Last 1
    if ([string]::IsNullOrWhiteSpace($checksumLine)) {
        throw "checksum for $asset was not found in checksums.txt"
    }
    $expectedHash = ($checksumLine -split '\s+')[0].ToLowerInvariant()
    $actualHash = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualHash -ne $expectedHash) {
        throw "checksum mismatch for $asset"
    }

    $extractDirectory = Join-Path $temporaryDirectory 'extract'
    New-Item -ItemType Directory -Path $extractDirectory | Out-Null
    $tar = Get-Command tar.exe -ErrorAction SilentlyContinue
    if ($null -eq $tar) {
        $tar = Get-Command tar -ErrorAction Stop
    }
    & $tar.Source -xzf $archive -C $extractDirectory
    if ($LASTEXITCODE -ne 0) {
        throw "tar failed with exit code $LASTEXITCODE"
    }
    $sourceBinary = Join-Path $extractDirectory 'planmaxx.exe'
    if (-not (Test-Path -LiteralPath $sourceBinary -PathType Leaf)) {
        throw 'release archive does not contain planmaxx.exe'
    }

    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    $binary = Join-Path $InstallDir 'planmaxx.exe'
    $temporaryBinary = Join-Path $InstallDir (".planmaxx.exe.tmp-$PID")
    Copy-Item -LiteralPath $sourceBinary -Destination $temporaryBinary -Force
    Move-Item -LiteralPath $temporaryBinary -Destination $binary -Force
    Write-InstallerLog "Installed $binary"

    $pathChanged = Add-PlanMaxxUserPath $InstallDir
    $currentEntries = $env:Path.Split(';', [StringSplitOptions]::RemoveEmptyEntries)
    if (-not ($currentEntries | Where-Object { [string]::Equals($_.TrimEnd('\'), $InstallDir.TrimEnd('\'), [StringComparison]::OrdinalIgnoreCase) })) {
        $env:Path = "$InstallDir;$env:Path"
    }
    if ($pathChanged) {
        Write-InstallerLog "Added $InstallDir to the user PATH. New processes will inherit it."
    }

    & $binary version
    if ($LASTEXITCODE -ne 0) {
        throw "installed planmaxx failed its version check with exit code $LASTEXITCODE"
    }

    if ($wantCodexSkill) {
        Write-InstallerLog 'Installing the optional user-level Codex skill...'
        & $binary skill install
        if ($LASTEXITCODE -ne 0) { throw 'PlanMaxx could not install the Codex skill' }
    } else {
        Write-InstallerLog "Optional Codex skill not installed. To enable it later, run: planmaxx skill install"
    }
    if ($wantClaudeSkill) {
        Write-InstallerLog 'Installing the optional user-level Claude Code skill...'
        & $binary skill install --target claude
        if ($LASTEXITCODE -ne 0) { throw 'PlanMaxx could not install the Claude Code skill' }
    }
    if ($wantGrokSkill) {
        Write-InstallerLog 'Installing the optional user-level Grok Build skill...'
        & $binary skill install --target grok
        if ($LASTEXITCODE -ne 0) { throw 'PlanMaxx could not install the Grok Build skill' }
    }

    if ($pathChanged -or $wantCodexSkill) {
        Write-InstallerLog 'Restart Codex Desktop so it inherits the updated PATH and reloads installed skills.'
    }
} finally {
    if (Test-Path -LiteralPath $temporaryDirectory) {
        Remove-Item -LiteralPath $temporaryDirectory -Recurse -Force
    }
}
