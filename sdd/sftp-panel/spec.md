# SSH/SFTP File Panel — Specification

## Overview

Add remote SFTP file panels to superfile. Users open SSH connections from the sidebar, navigate remote directories, and transfer files across local↔remote boundaries — all within the same Bubbletea TUI.

Architectural keystone: a `backend.FileSystem` interface where `nil` = local OS (zero changes to existing local-only code paths).

---

## Capability 1: `filesystem-interface` — Backend Abstraction Layer

### Requirements

| # | Requirement | Strength |
|---|-------------|----------|
| FI-1 | A `backend.FileSystem` interface MUST define: `ReadDir`, `Stat`, `Lstat`, `Open`, `Create`, `Remove`, `RemoveAll`, `Rename`, `MkdirAll`, `ReadLink`, `Walk`, `Close` | MUST |
| FI-2 | `localFS` MUST delegate every interface method to `os.*`/`filepath.*` — zero new behavior | MUST |
| FI-3 | `sftpFS` MUST delegate every interface method to `github.com/pkg/sftp` v1.13.x Client | MUST |
| FI-4 | A `backend.FileInfo` type alias for `os.FileInfo` MUST exist for backward compatibility | MUST |
| FI-5 | Each FS implementation MUST provide path helpers: `Join`, `Dir`, `Base`, `Abs` | MUST |
| FI-6 | The panel's `fs` field MUST accept `nil` to mean "use local OS" — existing code unchanged | MUST |

### Scenarios

**FI-S1: localFS wraps os operations**
```
GIVEN a localFS instance
WHEN ReadDir("/home/user") is called
THEN it returns the same result as os.ReadDir("/home/user")
```

**FI-S2: sftpFS wraps sftp operations**
```
GIVEN an sftpFS backed by a connected sftp.Client
WHEN ReadDir("/remote/path") is called
THEN it delegates to client.ReadDir("/remote/path")
```

**FI-S3: nil fs falls back to local**
```
GIVEN a filepanel.Model with fs == nil
WHEN getDirectoryElements is called
THEN it uses os.ReadDir as it does today
```

---

## Capability 2: `sftp-panel` — Remote File Panel

### Requirements

| # | Requirement | Strength |
|---|-------------|----------|
| SP-1 | Panel creation MUST accept an SSH connection config (host, port, user, auth) and establish an SFTP session | MUST |
| SP-2 | Panel Location MUST be a rooted remote path (e.g. `/home/user`) — NOT an `sftp://` URL | MUST |
| SP-3 | `getDirectoryElements` MUST dispatch to `fs.ReadDir` when `fs != nil`, else `os.ReadDir` | MUST |
| SP-4 | `UpdateCurrentFilePanelDir` MUST use `fs.Stat` when `fs != nil`, else `os.Stat` | MUST |
| SP-5 | Element sorting and column rendering MUST work identically with SFTP `os.FileInfo` (backend.FileInfo is `os.FileInfo`) | MUST |
| SP-6 | Panel top bar MUST render `connectionName:path` for remote panels | MUST |
| SP-7 | Panel close MUST call `fs.Close()` on non-nil FS to disconnect SFTP session | MUST |
| SP-8 | Multiple panels MAY each have their own independent FS instance | MAY |

### Scenarios

**SP-S1: Navigate remote directory**
```
GIVEN an SFTP panel at /home/user with fs != nil
WHEN user presses Enter on "projects" directory
THEN panel.Location changes to "/home/user/projects"
AND getDirectoryElements reads from fs.ReadDir("/home/user/projects")
```

**SP-S2: Remote parent directory**
```
GIVEN an SFTP panel at /home/user/projects
WHEN user presses Backspace
THEN panel.Location changes to "/home/user"
```

**SP-S3: Close remote panel**
```
GIVEN an SFTP panel connected to "myserver"
WHEN the panel is closed
THEN fs.Close() is called
AND the SFTP session is terminated
```

---

## Capability 3: `remote-clipboard` — Cross-Boundary Transfers

### Requirements

| # | Requirement | Strength |
|---|-------------|----------|
| RC-1 | Clipboard items MUST carry a `SourceFS` identifier (e.g. `"local"` or a connection name) | MUST |
| RC-2 | Paste handler MUST compare source FS and target FS — if different, use streaming `io.Copy` | MUST |
| RC-3 | File download (remote→local): `sftpFS.Open` → `os.Create` → `io.Copy` | MUST |
| RC-4 | File upload (local→remote): `os.Open` → `sftpFS.Create` → `io.Copy` | MUST |
| RC-5 | Directory copy across boundaries MUST use `fs.Walk` on source + per-file `io.Copy` | MUST |
| RC-6 | On transfer error, partial destination file MUST be removed | MUST |
| RC-7 | Progress bar MUST show remote file names during cross-boundary transfers | MUST |
| RC-8 | Cut from remote MUST `fs.RemoveAll` source only after successful paste | MUST |

### Scenarios

**RC-S1: Upload local file to remote**
```
GIVEN a local file "/home/user/file.txt" in clipboard (sourceFS="local")
AND an SFTP panel at "/home/remote" with its own FS
WHEN user pastes (Ctrl+V)
THEN os.Open("/home/user/file.txt") is read
AND sftpFS.Create("/home/remote/file.txt") writes the data
AND progress bar shows "Uploading file.txt"
```

**RC-S2: Download remote file to local**
```
GIVEN clipboard has "/home/remote/file.txt" (sourceFS="myserver")
AND focused panel is local at "/home/user"
WHEN user pastes
THEN sftpFS.Open("/home/remote/file.txt") is read
AND os.Create("/home/user/file.txt") writes the data
```

**RC-S3: Transfer fails mid-stream**
```
GIVEN a 500MB file transfer from local to remote
WHEN the connection drops at 60%
THEN the partial remote file is removed
AND an error notification is shown
```

**RC-S4: Cut from remote**
```
GIVEN clipboard has "/home/remote/file.txt" (sourceFS="myserver", cut=true)
WHEN paste to local completes successfully
THEN sftpFS.Remove("/home/remote/file.txt") is called
```

---

## Capability 4: `connection-manager` — SSH Connection UX

### Requirements

| # | Requirement | Strength |
|---|-------------|----------|
| CM-1 | Connection config MUST be stored at `~/.config/superfile/ssh_connections.toml` | MUST |
| CM-2 | Connection TOML structure: `name`, `host`, `port`, `user`, `auth_type` (`"key"`\|`"password"`), `key_path` | MUST |
| CM-3 | Sidebar MUST render an "SSH Connections" section with configured connection names | MUST |
| CM-4 | Selecting a connection MUST attempt SSH dial + SFTP start, showing a loading state | MUST |
| CM-5 | SSH key auth MUST search `~/.ssh/id_ed25519`, `~/.ssh/id_rsa`, `~/.ssh/id_ecdsa` in order | MUST |
| CM-6 | Password auth MUST show a prompt modal when `auth_type = "password"` | MUST |
| CM-7 | Connection errors MUST surface via existing `notify`/`spferror` modals | MUST |
| CM-8 | App quit MUST disconnect all active SFTP sessions | MUST |
| CM-9 | Connections SHOULD use TCP keepalive + periodic `sftp.ReadDir(".")` as keepalive | SHOULD |

### Scenarios

**CM-S1: Connect via sidebar**
```
GIVEN "myserver" is configured in ssh_connections.toml (key auth)
WHEN user selects it in sidebar
THEN a loading indicator is shown
AND an SFTP panel opens at the remote home directory
AND the top bar shows "myserver:/home/user"
```

**CM-S2: Connection failure**
```
GIVEN "badhost" has wrong host or invalid key
WHEN user selects it
THEN an error notification says "SSH connection failed: dial tcp ... no route to host"
AND no panel is opened
```

**CM-S3: Two panels to same server**
```
GIVEN user opens "myserver" in panel 1
AND opens "myserver" in panel 2
WHEN both are active
THEN each has an independent sftp.Client
AND disconnecting panel 1 does not affect panel 2
```

---

## Types & Integration Points

### New Types (in `src/internal/backend/`)

```go
// FileSystem is the abstraction over local OS and remote SFTP filesystems.
// nil pointer means "use local OS" — existing code is unchanged.
type FileSystem interface {
    ReadDir(name string) ([]os.FileEntry, error)
    Stat(name string) (os.FileInfo, error)
    Lstat(name string) (os.FileInfo, error)
    Open(name string) (io.ReadCloser, error)
    Create(name string) (io.WriteCloser, error)
    Remove(name string) error
    RemoveAll(name string) error
    Rename(oldpath, newpath string) error
    MkdirAll(name string, perm os.FileMode) error
    ReadLink(name string) (string, error)
    Walk(root string, fn filepath.WalkFunc) error
    Close() error
    Join(elem ...string) string
    Dir(path string) string
    Base(path string) string
    Abs(path string) (string, error)
}

// FileInfo is a type alias for backward compatibility
type FileInfo = os.FileInfo
```

### Modified Types

| File | Change |
|------|--------|
| `filepanel/types.go` | Add `FS backend.FileSystem` field to `Model` |
| `filepanel/model.go` | `New()` accepts optional `backend.FileSystem` parameter |
| `clipboard/model.go` | Add `sourceFS string` to `copyItems`, add `sourceFS` to `SetItems`/`Add` |
| `sidebar/type.go` | No change — SSH entries use existing `directory` struct with special location prefix |
| `sidebar/consts.go` | Add `sshDividerDir` constant, add `SidebarSectionSSH = "ssh"` |
| `sidebar/directory_utils.go` | Add `utils.SidebarSectionSSH` case in `formDirctorySlice`; add `getSSHConnections()` |
| `sidebar/render.go` | Add SSH divider render case |
| `internal/type.go` | Add `sshConnections` and `activeSSHPanels` fields to main `model` |

### Config File: `~/.config/superfile/ssh_connections.toml`

```toml
[[connection]]
name = "myserver"
host = "192.168.1.100"
port = 22
user = "gonzalo"
auth_type = "key"
key_path = ""

[[connection]]
name = "web-prod"
host = "web.example.com"
port = 22
user = "deploy"
auth_type = "password"
key_path = ""
```

### Integration Map

| Current Code | Change for SFTP |
|---|---|
| `os.ReadDir(m.Location)` → `fs.ReadDir(m.Location)` | `filepanel/get_elements.go:18` |
| `os.Stat(path)` in `UpdateCurrentFilePanelDir` → `fs.Stat(path)` | `filepanel/update.go:42` |
| `os.Lstat(m.items.items[i])` in clipboard render → check sourceFS, skip if remote | `clipboard/model.go:59` |
| `file_operations.go` copy/paste/move — all `os.*` → dispatch to source/target `FileSystem` | Phase 2 |
| Sidebar `getDirectories()` → append SSH connections from config | `sidebar/directory_utils.go` |
| App quit cleanup → iterate active panels with non-nil FS, call `Close()` | `internal/type.go` model |

### Delivery Phases

| Phase | Capabilities | LOC |
|-------|-------------|-----|
| 1 — Core | `filesystem-interface`, `sftp-panel` (minimal), sidebar section | ~350 |
| 2 — Transfers | `remote-clipboard`, cross-boundary paste, progress | ~300 |
| 3 — Connection UX | `connection-manager`, modal, auth prompts, keepalive | ~400 |
| 4 — Polish | Remote preview, metadata, error recovery, reconnect | ~350 |

### Edge Cases

- **Stale clipboard path**: If source panel is closed, paste shows "source unavailable"
- **Permission denied on remote**: Surface SFTP error via existing `spferror` modal
- **Very large directories** (>5000 entries): SFTP fetches all at once — defer lazy pagination to post-MVP
- **Partial transfer cleanup**: Defer cleanup to Phase 2; Phase 1 shows error without cleanup
