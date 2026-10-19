param([Parameter(Mandatory=$true)][string]$Version)
$ErrorActionPreference = "Stop"
if ($Version -notmatch '^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-(alpha|beta|rc)(\.(0|[1-9]\d*))?)?$') { throw "Version must be MAJOR.MINOR.PATCH with an optional alpha, beta, or rc suffix: $Version" }
$ProductVersion = ($Version -split '-', 2)[0]
$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
Set-Location $root
$env:GOPATH = if ($env:GOPATH) { $env:GOPATH } else { Join-Path $root ".cache/go" }
$env:GOMODCACHE = if ($env:GOMODCACHE) { $env:GOMODCACHE } else { Join-Path $env:GOPATH "pkg/mod" }
$env:GOCACHE = if ($env:GOCACHE) { $env:GOCACHE } else { Join-Path $root ".cache/go-build" }
$env:GOBIN = if ($env:GOBIN) { $env:GOBIN } else { Join-Path $env:GOPATH "bin" }
$env:PATH = "$env:GOBIN;$env:PATH"
$env:CGO_ENABLED = "1"
$helper = Join-Path $root "third_party/kindling/windows-amd64/kindling-cli.exe"
if (!(Test-Path $helper)) { throw "Missing pinned helper: $helper" }
if (!(Test-Path (Join-Path $env:GOBIN "wails.exe"))) {
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.15.0
  if ($LASTEXITCODE -ne 0) { throw "Could not install Wails CLI: $LASTEXITCODE" }
}
$configPath = Join-Path $root "wails.json"
$backup = Get-Content -Raw $configPath
try {
  $config = $backup | ConvertFrom-Json
  $config.info.productVersion = $ProductVersion
  $config | ConvertTo-Json -Depth 32 | Set-Content -Encoding utf8 $configPath
  & (Join-Path $env:GOBIN "wails.exe") build -clean -platform windows/amd64 -ldflags "-X attune/internal/app.appVersion=$Version -X attune/internal/app.updateEnabled=true"
  if ($LASTEXITCODE -ne 0) { throw "Wails build failed with exit code $LASTEXITCODE" }
  $stage = Join-Path $root "build/release/Attune-windows-amd64"
  $dist = Join-Path $root "dist"
  New-Item -ItemType Directory -Force -Path "$stage/licenses", $dist | Out-Null
  Copy-Item (Join-Path $root "build/bin/Attune.exe") $stage
  Copy-Item $helper (Join-Path $stage "kindling-cli.exe")
  Copy-Item (Join-Path $root "LICENSE") (Join-Path $stage "licenses/Attune-LICENSE.txt")
  Copy-Item (Join-Path $root "THIRD_PARTY_NOTICES.md") (Join-Path $stage "licenses")
  Copy-Item (Join-Path $root "THIRD_PARTY_LICENSES.txt") (Join-Path $stage "licenses")
  Copy-Item (Join-Path $root "third_party/kindling/THIRD_PARTY_LICENSES.txt") (Join-Path $stage "licenses/Kindling-THIRD_PARTY_LICENSES.txt")
  Copy-Item (Join-Path $root "third_party/kindling/LICENSE") (Join-Path $stage "licenses/Kindling-LICENSE.txt")
  @"
Attune Windows amd64 (experimental)

Run Attune.exe. Microsoft Edge WebView2 Runtime is required.
USB Kindle discovery, transfer, and eject are experimental and use PowerShell Storage cmdlets. A removable volume with a drive letter and stable IDs is required.
Physical-device validation is limited; check that the Kindle appears as a mounted drive before transferring.
"@ | Set-Content -Encoding utf8 (Join-Path $stage "INSTALL.txt")
  Compress-Archive -Path "$stage/*" -DestinationPath (Join-Path $dist "Attune-windows-amd64.zip") -Force
} finally {
  Set-Content -NoNewline -Encoding utf8 $configPath $backup
}
Write-Output "Created dist package for windows/amd64 (version $Version)"
