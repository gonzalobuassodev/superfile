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
echo "✅ Done! Open a new terminal and run 's' to start superfile."
echo "   Press 'q' to quit and cd to the last browsed directory."
