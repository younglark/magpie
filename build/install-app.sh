#!/bin/sh
# `make install-app`: put this tree's desktop app where the release
# installer puts one, then open it. macOS: magpie.app in /Applications
# (~/Applications when that is not writable) and a magpie command in
# ~/.local/bin. Linux: the binary there, plus a menu entry. MAGPIE_BIN_DIR
# and MAGPIE_APPS_DIR override those paths; MAGPIE_OPEN=0 skips launching.
set -eu
cd "$(dirname "$0")/.."

bin="${MAGPIE_BIN_DIR:-$HOME/.local/bin}"
open="${MAGPIE_OPEN:-1}"

say() { printf '  %s\n' "$*"; }
die() { printf 'install-app: %s\n' "$*" >&2; exit 1; }

# Stop a running magpie so the bundle or binary can be replaced and the
# gateway port is free. magpie-dev is left alone.
quit_magpie() {
  pgrep -x magpie >/dev/null 2>&1 || return 0
  if command -v osascript >/dev/null 2>&1; then
    osascript -e 'tell application "magpie" to quit' >/dev/null 2>&1 || true
  fi
  i=0
  while pgrep -x magpie >/dev/null 2>&1 && [ "$i" -lt 25 ]; do
    sleep 0.2
    i=$((i + 1))
  done
  if pgrep -x magpie >/dev/null 2>&1; then
    killall magpie 2>/dev/null || true
    sleep 0.3
  fi
  if pgrep -x magpie >/dev/null 2>&1; then
    killall -9 magpie 2>/dev/null || true
    sleep 0.2
  fi
}

install_cli_link() {
  mkdir -p "$bin"
  ln -sf "$1" "$bin/magpie"
  say "installed $bin/magpie"
  case ":$PATH:" in
    *":$bin:"*) ;;
    *) say "add $bin to your PATH to run magpie from a terminal" ;;
  esac
}

os=$(uname -s)
case "$os" in
  Darwin)
    [ -d magpie.app ] || die "magpie.app is missing; run make app first"
    apps="${MAGPIE_APPS_DIR:-}"
    if [ -z "$apps" ]; then
      apps=/Applications
      [ -w "$apps" ] || { apps="$HOME/Applications"; mkdir -p "$apps"; }
    else
      mkdir -p "$apps"
    fi
    quit_magpie
    rm -rf "$apps/magpie.app"
    ditto magpie.app "$apps/magpie.app"
    codesign --force --deep --sign - "$apps/magpie.app" >/dev/null 2>&1 || true
    xattr -dr com.apple.quarantine "$apps/magpie.app" 2>/dev/null || true
    say "installed $apps/magpie.app"
    install_cli_link "$apps/magpie.app/Contents/MacOS/magpie"
    if [ "$open" != 0 ]; then
      open "$apps/magpie.app"
      say "opened magpie"
    fi
    ;;
  Linux)
    [ -f magpie ] || die "./magpie is missing; run make build first"
    quit_magpie
    mkdir -p "$bin"
    install -m 755 magpie "$bin/magpie"
    share="${XDG_DATA_HOME:-$HOME/.local/share}"
    mkdir -p "$share/applications" "$share/icons/hicolor/256x256/apps"
    if [ -f build/windows/icon-256.png ]; then
      cp build/windows/icon-256.png "$share/icons/hicolor/256x256/apps/magpie.png"
    elif [ -f internal/gui/icon-1024.png ]; then
      cp internal/gui/icon-1024.png "$share/icons/hicolor/256x256/apps/magpie.png"
    fi
    cat > "$share/applications/magpie.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=magpie
Comment=Every agent's model. One place.
Exec=$bin/magpie %u
Icon=magpie
Categories=Development;Utility;
MimeType=x-scheme-handler/magpie;
Terminal=false
EOF
    command -v update-desktop-database >/dev/null && update-desktop-database "$share/applications" 2>/dev/null || true
    command -v xdg-mime >/dev/null && xdg-mime default magpie.desktop x-scheme-handler/magpie 2>/dev/null || true
    say "installed the magpie desktop app (menu entry: magpie)"
    install_cli_link "$bin/magpie"
    if [ "$open" != 0 ]; then
      "$bin/magpie" >/dev/null 2>&1 &
      say "opened magpie"
    fi
    ;;
  *)
    die "this machine is not macOS or Linux; use make install"
    ;;
esac
