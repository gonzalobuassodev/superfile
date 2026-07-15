<div align="center">
<br>
<picture>
  <source width="300" media="(prefers-color-scheme: dark)" srcset="website/src/assets/superfile-night.svg" />
  <source width="300" media="(prefers-color-scheme: light)" srcset="website/src/assets/superfile-day.svg" />
  <img alt="superfile logo" src="website/src/assets/superfile-day.svg" />
</picture>
<br><br>

**Pretty fancy, good-looking terminal file manager** — with SSH/SFTP remote file support & macOS-first polish.

[![License MIT](https://img.shields.io/badge/license-MIT-blue.svg)](https://raw.githubusercontent.com/yorukot/superfile/refs/heads/main/LICENSE)

</div>

## What is this?

This is a **fork** of [superfile](https://github.com/yorukot/superfile) by [yorukot](https://github.com/yorukot), with added remote file management over SSH/SFTP and macOS quality-of-life improvements.

## What's different from upstream?

- **SSH/SFTP remote file panel** — browse, navigate, and manage files on remote servers directly from the sidebar
- **Remote-aware clipboard** — copy/paste works across local and remote panels; files stay on the server
- **SSH connection manager** — add/delete connections from the sidebar with confirmation dialogs
- **macOS Cmd+C/V passthrough** — native keyboard shortcuts work in terminals like Ghostty, iTerm2, and Kitty
- **macOS Cmd+Backspace delete** — permanent file deletion with a confirmation prompt
- **Preview panel hidden by default** — cleaner startup, toggle on when needed
- **`cd on quit` for Fish shell** — exit superfile and cd to the last browsed directory
- **Ghostty keybindings** auto-configured on install

## Quick install

```bash
sh -c "$(curl -fsSL https://raw.githubusercontent.com/gonzalobuassodev/superfile/main/scripts/install.sh)"
```

Requires [Go](https://go.dev/dl/). Installs to `~/.local/bin/spf`, sets up Fish integration and Ghostty keybindings automatically.

### Manual build

```bash
git clone https://github.com/gonzalobuassodev/superfile.git --depth=1
cd superfile
go build -tags embed -o ~/.local/bin/spf .
```

## Usage

```bash
spf
```

Or with the Fish alias (auto-configured on install):

```bash
s
```

### Remote file management

1. Open the sidebar and navigate to the SSH section
2. Add a connection (host, port, username — uses key-based auth)
3. Browse remote files like a local panel
4. Copy, cut, paste, delete, extract, and compress files across local and remote panels

## Hotkeys

| Key | Action |
|---|---|
| `Cmd+C` | Copy selected file(s) |
| `Cmd+X` | Cut selected file(s) |
| `Cmd+V` | Paste from clipboard |
| `Cmd+Backspace` | Delete selected file(s) |
| `Space` | Toggle multi-select in file panel |
| `?` / `Cmd+/` | Toggle help menu |

See the [upstream hotkey docs](https://superfile.dev/configure/custom-hotkeys/) for the full reference.

## Supported systems

- **macOS** (primary target)
- **Linux**
- **Windows** (not fully supported yet)

## Config

```
~/Library/Application Support/superfile/config.toml
```

Or wherever `$XDG_CONFIG_HOME/superfile/config.toml` resolves on your system.

Run `spf --fix-config-file` to regenerate the config with default values.

## Credits

This fork is based on [superfile](https://github.com/yorukot/superfile) by [yorukot](https://github.com/yorukot). All credit for the original design, architecture, and most of the code goes to the upstream maintainers and contributors.

Upstream links:
- [yorukot/superfile](https://github.com/yorukot/superfile)
- [superfile.dev](https://superfile.dev/)
