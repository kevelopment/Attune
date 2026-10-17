param(
  [string]$GOOS = "windows",
  [string]$GOARCH = "amd64"
)
$ErrorActionPreference = "Stop"
$version = "v0.45.6"
$platform = "$GOOS/$GOARCH"
if ($platform -ne "windows/amd64") { throw "Use scripts/fetch-kindling.sh for non-Windows helpers." }
$asset = "kindling-cli-windows.exe"
$expected = "1988a319d0d9af2e75cf776eb9e63a59412fcd8ebfc5d0cebb000519ccd99f54"
$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$targetDir = Join-Path $root "third_party/kindling/$GOOS-$GOARCH"
$target = Join-Path $targetDir "kindling-cli.exe"
$temp = Join-Path ([IO.Path]::GetTempPath()) ([guid]::NewGuid().ToString() + ".download")
try {
  New-Item -ItemType Directory -Force -Path $targetDir | Out-Null
  Invoke-WebRequest -Uri "https://github.com/ciscoriordan/kindling/releases/download/$version/$asset" -OutFile $temp
  $actual = (Get-FileHash -Algorithm SHA256 $temp).Hash.ToLowerInvariant()
  if ($actual -ne $expected) { throw "Kindling checksum mismatch: $actual" }
  Move-Item -Force $temp $target
  Invoke-WebRequest -Uri "https://raw.githubusercontent.com/ciscoriordan/kindling/$version/LICENSE" -OutFile (Join-Path $root "third_party/kindling/LICENSE")
  Write-Output "Fetched Kindling $version for $platform; sha256=$actual"
} finally {
  Remove-Item -Force -ErrorAction SilentlyContinue $temp
}
