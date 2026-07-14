#!/usr/bin/env fish

set -g SCRIPT_DIR (dirname (status --current-filename))
set -g REPO_DIR (dirname $SCRIPT_DIR)

echo "📦 Building superfile from $REPO_DIR..."
cd $REPO_DIR
go build -o /tmp/superfile-build .

echo "📋 Installing binary to ~/.local/bin/spf..."
cp /tmp/superfile-build ~/.local/bin/spf
chmod +x ~/.local/bin/spf

echo "🐟 Installing fish function..."
set -l functions_dir "$HOME/.config/fish/functions"
if not test -d "$functions_dir"
    mkdir -p "$functions_dir"
end

set -l spf_func_file "$functions_dir/spf.fish"
echo "\
function spf --wraps spf --description 'Superfile — pretty fancy terminal file manager. Runs the binary and cds to last browsed dir on quit.'
    command spf \$argv
    set -l lastdir_file \"\$HOME/Library/Application Support/superfile/lastdir\"
    if test -f \"\$lastdir_file\"
        set -l cd_cmd (string trim (cat \"\$lastdir_file\"))
        if string match -qr \"^cd \" \"\$cd_cmd\"
            eval \"\$cd_cmd\"
        end
    end
end" > "$spf_func_file"

echo "🔗 Adding alias s=spf if missing..."
if not grep -q 'alias s="spf"' "$HOME/.config/fish/config.fish" 2>/dev/null
    echo '\nalias s="spf"' >> "$HOME/.config/fish/config.fish"
    echo "  -> added alias s=spf to config.fish"
else
    echo "  -> alias s=spf already exists"
end

echo ""
echo "🔧 Checking Ghostty config..."
set -l ghostty_config "$HOME/.config/ghostty/config"
if test -f "$ghostty_config"
    if not grep -q "cmd+c=text:\\\\x03" "$ghostty_config" 2>/dev/null
        echo "  -> Adding Cmd+C/V passthrough to Ghostty config..."
        echo "
# Superfile: Cmd macOS shortcuts para copy/paste/delete
keybind = cmd+c=text:\\x03
keybind = cmd+v=text:\\x16
keybind = cmd+backspace=text:\\x04" >> "$ghostty_config"
        echo "  -> Done. Restart Ghostty to apply."
    else
        echo "  -> Ghostty already configured."
    end
else
    echo "  -> Ghostty config not found (skipped)."
end

echo ""
echo "✅ Done! Open a new terminal and run 's' to start superfile."
echo "   Press 'q' to quit and cd to the last browsed directory."
