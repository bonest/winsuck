param(
  [string]$Version = "latest"
)

$Repository = "bonest/winsuck"
$InstallDirectory = Join-Path $env:LOCALAPPDATA "winsuck\bin"
$ApiUrl = "https://api.github.com/repos/$Repository/releases"

if ($Version -eq "latest") {
  $Release = Invoke-RestMethod "$ApiUrl/latest"
} else {
  $Release = Invoke-RestMethod "$ApiUrl/tags/v$Version"
}

$Archive = $Release.assets | Where-Object {
  $_.name -match "^winsuck_.*_windows_amd64\.zip$"
} | Select-Object -First 1
$Checksums = $Release.assets | Where-Object { $_.name -eq "checksums.txt" } |
  Select-Object -First 1

if (-not $Archive -or -not $Checksums) {
  throw "Windows archive or checksums.txt is missing from the release."
}

$TemporaryDirectory = Join-Path ([System.IO.Path]::GetTempPath()) "winsuck-install"
New-Item -ItemType Directory -Force $TemporaryDirectory, $InstallDirectory | Out-Null
$ArchivePath = Join-Path $TemporaryDirectory $Archive.name
$ChecksumsPath = Join-Path $TemporaryDirectory "checksums.txt"

Invoke-WebRequest $Archive.browser_download_url -OutFile $ArchivePath
Invoke-WebRequest $Checksums.browser_download_url -OutFile $ChecksumsPath

$Expected = ((Select-String -Path $ChecksumsPath -Pattern ([regex]::Escape($Archive.name))).Line -split '\s+')[0].ToLower()
$Actual = (Get-FileHash -Algorithm SHA256 $ArchivePath).Hash.ToLower()
if ($Expected -ne $Actual) {
  throw "Checksum verification failed for $($Archive.name)."
}

Expand-Archive -Path $ArchivePath -DestinationPath $TemporaryDirectory -Force
Copy-Item (Join-Path $TemporaryDirectory "winsuck.exe") $InstallDirectory -Force

$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (($UserPath -split ";") -notcontains $InstallDirectory) {
  [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDirectory", "User")
}
$env:Path = "$env:Path;$InstallDirectory"

Write-Host "Installed winsuck.exe to $InstallDirectory"
Write-Host "Open a new PowerShell or WSL session before relying on the persisted PATH."
