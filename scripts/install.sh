#!/bin/sh
set -e

REPO="gonzalobuassodev/superfile"
BINARY="spf"

# ── helpers ──────────────────────────────────────────────────────────────
info()  { printf "\033[34m➜\033[0m %s\n" "$1"; }
ok()    { printf "\033[32m✔\033[0m %s\n" "$1"; }
err()   { printf "\033[31m✖\033[0m %s\n" "$1"; exit 1; }

# ── detect OS ────────────────────────────────────────────────────────────
OS="$(uname -s)"
case "$OS" in
  Darwin) ;;
  *) err "This install script supports macOS only (detected: $OS)" ;;
esac

# ── install dir ──────────────────────────────────────────────────────────
BIN_DIR="${HOME}/.local/bin"
mkdir -p "$BIN_DIR"

# ── build or download ────────────────────────────────────────────────────
if command -v go >/dev/null 2>&1; then
  info "Building superfile from source (requires Go)..."

  BUILD_DIR=$(mktemp -d)
  trap 'rm -rf "$BUILD_DIR"' EXIT

  git clone --depth 1 "https://github.com/${REPO}.git" "$BUILD_DIR/superfile"
  cd "$BUILD_DIR/superfile"
  go build -tags embed -o "$BIN_DIR/$BINARY" .
  ok "Binary installed to $BIN_DIR/$BINARY"
else
  err "Go is not installed. Install Go first: https://go.dev/dl/"
fi

# ── fish function ────────────────────────────────────────────────────────
if command -v fish >/dev/null 2>&1; then
  info "Installing fish wrapper function for cd on quit..."

  FUNC_DIR="${HOME}/.config/fish/functions"
  mkdir -p "$FUNC_DIR"

  cat > "$FUNC_DIR/spf.fish" << 'FISHFUNC'
function spf --wraps spf --description 'Superfile — pretty fancy terminal file manager. Runs the binary and cds to last browsed dir on quit.'
    command spf $argv
    set -l lastdir_file "$HOME/Library/Application Support/superfile/lastdir"
    if test -f "$lastdir_file"
        set -l cd_cmd (string trim (cat "$lastdir_file"))
        if string match -qr "^cd " "$cd_cmd"
            eval "$cd_cmd"
        end
    end
end
FISHFUNC
  ok "Fish function installed to $FUNC_DIR/spf.fish"

  # ── alias s=spf ──────────────────────────────────────────────────────
  CONF="${HOME}/.config/fish/config.fish"
  if [ -f "$CONF" ] && ! grep -q 'alias s="spf"' "$CONF" 2>/dev/null; then
    printf '\nalias s="spf"\n' >> "$CONF"
    ok "Added alias s=\"spf\" to $CONF"
  elif [ ! -f "$CONF" ]; then
    mkdir -p "$(dirname "$CONF")"
    printf 'alias s="spf"\n' > "$CONF"
    ok "Created $CONF with alias s=\"spf\""
  else
    ok "Alias s=\"spf\" already exists in $CONF"
  fi
else
  info "Fish shell not detected — skipping fish integration."
  info "Install fish and run: fish scripts/install-macos.fish"
fi

# ── ghostty config ─────────────────────────────────────────────────────
if [ -f "${HOME}/.config/ghostty/config" ]; then
  if ! grep -q "cmd+c=text:\\\\x03" "${HOME}/.config/ghostty/config" 2>/dev/null; then
    info "Adding Cmd+C/V passthrough to Ghostty config..."
    cat >> "${HOME}/.config/ghostty/config" << 'GHOSTTY'

# Superfile: Cmd macOS shortcuts para copy/paste/delete
keybind = cmd+c=text:\x03
keybind = cmd+v=text:\x16
keybind = cmd+backspace=text:\x04
GHOSTTY
    ok "Added Ghostty keybindings for superfile"
  else
    ok "Ghostty already has Cmd+C/V passthrough"
  fi
fi

printf "\n\033[32m✅ Done!\033[0m Open a new terminal and run \033[1ms\033[0m to start superfile.\n"
printf "   Press \033[1mq\033[0m to quit and cd to the last browsed directory.\n"
printf "\033[90m   Restart Ghostty if Cmd+C/V don't work (close & reopen terminal).\033[0m\n"
