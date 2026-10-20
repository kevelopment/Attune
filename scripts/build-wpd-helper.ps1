$ErrorActionPreference = "Stop"
$root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path
$source = Join-Path $root "internal/device/wpd/main.cpp"
$outputDir = Join-Path $root "build/bin"
$output = Join-Path $outputDir "wpd-helper.exe"
if (!(Get-Command cl.exe -ErrorAction SilentlyContinue)) {
  $vswhere = Join-Path ${env:ProgramFiles(x86)} "Microsoft Visual Studio/Installer/vswhere.exe"
  if (Test-Path $vswhere) {
    $vsInstall = & $vswhere -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
    $vsDevCmd = if ($vsInstall) { Join-Path $vsInstall "Common7/Tools/VsDevCmd.bat" }
    if ($vsDevCmd -and (Test-Path $vsDevCmd)) {
      $environment = & $env:ComSpec /d /s /c ('call "' + $vsDevCmd + '" -arch=x64 -host_arch=x64 >nul && set')
      foreach ($line in $environment) {
        $separator = $line.IndexOf('=')
        if ($separator -gt 0) {
          [Environment]::SetEnvironmentVariable($line.Substring(0, $separator), $line.Substring($separator + 1), 'Process')
        }
      }
    }
  }
}
if (!(Get-Command cl.exe -ErrorAction SilentlyContinue)) {
  throw "Build wpd-helper.exe with the MSVC C++ workload and Windows SDK installed (PortableDeviceApi.h, PortableDevice.h, and PortableDeviceGUIDs.lib are required). The script could not locate an x64 Visual Studio toolchain."
}
if ($env:VSCMD_ARG_TGT_ARCH -and $env:VSCMD_ARG_TGT_ARCH -notmatch '^(x64|amd64)$') { throw "The WPD helper must target x64; open an x64 Visual Studio Developer PowerShell." }
New-Item -ItemType Directory -Force -Path $outputDir | Out-Null
& cl.exe /nologo /std:c++17 /EHsc /W4 /DUNICODE /D_UNICODE $source "/Fo:$outputDir\wpd-helper.obj" "/Fe:$output" /link PortableDeviceGUIDs.lib Ole32.lib OleAut32.lib Propsys.lib
if ($LASTEXITCODE -ne 0) { throw "WPD helper compilation failed with exit code $LASTEXITCODE" }
Write-Output "Built $output"
