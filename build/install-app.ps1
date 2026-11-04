<#
.SYNOPSIS
  make install-app for Windows: build the desktop app, install it where the
  release installer puts one, and open it.

.DESCRIPTION
  The mirror of build/install-app.sh's macOS and Linux branches. Windows has
  no installer: the app installs itself, and this does what it does. The exe
  goes to %LOCALAPPDATA%\Programs\magpie\magpie.exe (installPath in
  internal/update/install_windows.go), that folder goes first on the user's
  PATH with no administrator, and the app opened from there adds the rest as
  it starts:

    - %APPDATA%\...\Start Menu\Programs\Magpie.lnk (internal/shortcut),
    - Win+R and `start magpie` (the App Paths key),
    - magpie.com beside the exe, which a shell waits for so `magpie plugin
      login` can read a hidden password (internal/update/com.go).

  Nothing here changes the data in ~/.config/magpie, so a rebuild keeps the
  providers, agents and sessions already set up.

.PARAMETER Arch
  amd64 or arm64; the host's own by default.

.PARAMETER NoOpen
  Install without opening magpie (MAGPIE_OPEN=0 for install-app.sh).

.EXAMPLE
  powershell -NoProfile -ExecutionPolicy Bypass -File build/install-app.ps1
#>
[CmdletBinding()]
param(
    [ValidateSet('amd64', 'arm64')]
    [string]$Arch = $(if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }),
    [switch]$NoOpen
)

$ErrorActionPreference = 'Stop'

$root = Split-Path -Parent $PSScriptRoot

function Say($msg) { Write-Host "  $msg" }
function Die($msg) { Write-Host "install-app: $msg" -ForegroundColor Red; exit 1 }

# 1. Build. make release-windows is the release's own recipe: the icon, the
# DPI manifest and the version resources from build/windows, the magpie.com
# launcher embedded, and the GUI subsystem so no console window opens.
if (-not (Get-Command make -ErrorAction SilentlyContinue)) {
    Die "make is not on PATH; run make release-windows yourself, or install it with: winget install ezwinports.make"
}
Push-Location $root
try {
    Say "building windows/$Arch"
    & make release-windows
    if ($LASTEXITCODE -ne 0) { Die "make release-windows failed" }
} finally {
    Pop-Location
}

$built = Join-Path $root "dist\magpie-windows-$Arch.exe"
if (-not (Test-Path -LiteralPath $built)) { Die "$built is missing after the build" }

# 2. Stop a running magpie so the exe can be replaced and the gateway port is
# free: a close first, as a click on the window's X is, then forced.
if (Get-Process -Name magpie -ErrorAction SilentlyContinue) {
    Say "stopping the running magpie"
    # cmd, not taskkill.exe directly: its "not found" chatter on stderr would
    # end the script under $ErrorActionPreference = 'Stop'
    & cmd.exe /c 'taskkill /IM magpie.exe >nul 2>&1'
    for ($i = 0; $i -lt 25; $i++) {
        if (-not (Get-Process -Name magpie -ErrorAction SilentlyContinue)) { break }
        Start-Sleep -Milliseconds 200
    }
    if (Get-Process -Name magpie -ErrorAction SilentlyContinue) {
        & cmd.exe /c 'taskkill /F /IM magpie.exe >nul 2>&1'
        Start-Sleep -Milliseconds 300
    }
}

# 3. Install: the user's own programs folder, where per-user installers put
# their apps. No administrator, and no Program Files.
$dir = Join-Path ([Environment]::GetFolderPath('LocalApplicationData')) 'Programs\magpie'
$exe = Join-Path $dir 'magpie.exe'
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Copy-Item -LiteralPath $built -Destination $exe -Force
Say "installed $exe"

# 4. Put that folder first on the user's PATH, so a terminal opened from now
# on finds `magpie` (cliPlace in internal/update/cli_path_windows.go, which
# keeps the value's own type so %USERPROFILE% in it still expands).
$key = [Microsoft.Win32.Registry]::CurrentUser.OpenSubKey('Environment', $true)
$cur = [string]$key.GetValue('Path', '', [Microsoft.Win32.RegistryValueOptions]::DoNotExpandEnvironmentNames)
$kind = [Microsoft.Win32.RegistryValueKind]::String
try { $kind = $key.GetValueKind('Path') } catch { }
$parts = @($cur -split ';' | Where-Object { $_ -ne '' -and -not $_.TrimEnd('\').Equals($dir.TrimEnd('\'), 'OrdinalIgnoreCase') })
$new = (@($dir) + $parts) -join ';'
if ($new -ne $cur) {
    $valueKind = [Microsoft.Win32.RegistryValueKind]::String
    if ($kind -eq [Microsoft.Win32.RegistryValueKind]::ExpandString) {
        $valueKind = [Microsoft.Win32.RegistryValueKind]::ExpandString
    }
    $key.SetValue('Path', $new, $valueKind)
    Say "put $dir first on your PATH"

    # Tell Explorer, so a terminal opened from the Start menu has it too.
    try {
        Add-Type -Namespace Win32 -Name Env -MemberDefinition @'
[DllImport("user32.dll", SetLastError = true, CharSet = CharSet.Auto)]
public static extern IntPtr SendMessageTimeout(IntPtr hWnd, uint Msg, UIntPtr wParam,
    string lParam, uint fuFlags, uint uTimeout, out UIntPtr result);
'@
        $res = [UIntPtr]::Zero
        [Win32.Env]::SendMessageTimeout([IntPtr]0xffff, 0x1A, [UIntPtr]::Zero, 'Environment', 2, 5000, [ref]$res) | Out-Null
    } catch {
        # the PATH is set either way; only Explorer's copy lags until it restarts
    }
}
$key.Close()

# 5. Open it. The first start makes the shortcut, the App Paths entry and
# magpie.com (see the top of this file).
if (-not $NoOpen) {
    Start-Process -FilePath $exe | Out-Null
    Say "opened magpie"
    Start-Sleep -Seconds 3
}

# 6. Say what is there, so a step that failed silently doesn't read as done.
$com = Join-Path $dir 'magpie.com'
$lnk = Join-Path ([Environment]::GetFolderPath('Programs')) 'Magpie.lnk'
Write-Host ''
Say "exe      $exe"
if (Test-Path -LiteralPath $com) {
    Say "command  magpie.com beside it: a new terminal runs magpie"
} else {
    Say "command  magpie.com appears as the app starts; open it once if this line still says so"
}
if (Test-Path -LiteralPath $lnk) {
    Say "start    $lnk"
} else {
    Say "start    the Magpie shortcut appears as the app starts"
}
