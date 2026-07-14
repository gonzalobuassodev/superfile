## Exploration: Clipboard Copy/Paste for Files

### Current State

Superfile already has a **complete internal file clipboard** system, but it operates entirely within superfile's own process memory — there is **no integration with the operating system's file clipboard**.

**What exists today:**

1. **Internal Clipboard Model** (`src/internal/ui/clipboard/model.go`):
   - Stores file paths (`items []string`) and a `cut bool` flag
   - Rendered as a "Clipboard" footer panel alongside Process/Metadata
   - Supports `Add()`, `SetItems()`, `Reset()`, `GetItems()`, `IsCut()`, `PruneInaccessibleItemsAndGet()`
   - Clipboard is cleared after a successful cut-paste (in `PasteOperationMsg.ApplyToModel()`)
   - Prunes inaccessible items before paste operations

2. **Keybinding System**:
   - `Common.HotkeysType` struct in `config_type.go` defines: `CopyItems`, `CutItems`, `PasteItems`
   - Default bindings: `ctrl+c`=copy, `ctrl+x`=cut, `ctrl+v`=paste
   - Vim preset: `y`=copy, `x`=cut, `p`=paste
   - Hotkeys loaded from TOML via `LoadHotkeysFile()`, validated for non-empty lists
   - State-machine priority in `handleKeyInput()` resolves the `ctrl+c` conflict between copy and cancel_typing

3. **Copy/Cut Operations** (`handle_file_operations.go`):
   - `copySingleItem(cut bool)` — adds focused item to internal clipboard
   - `copyMultipleItem(cut bool)` — adds all selected items to internal clipboard
   - Both store paths in `m.clipboard` — **not** the OS clipboard

4. **Paste Operation** (`handle_file_operations.go`):
   - `getPasteItemCmd()` → validates (no self-paste, no ancestor paste) → `executePasteOperation()` → `makePasteProcessor()`
   - `pasteDir()` handles directory copy with progress tracking and duplicate renaming
   - `copyFile()` does actual file I/O with `io.Copy`
   - `moveElement()` uses `os.Rename` on same partition, falls back to copy+delete

5. **OS Clipboard (Text Only)**:
   - Uses `github.com/atotto/clipboard` library (cross-platform text clipboard)
   - Only used for: `copyPath()` (Ctrl+P) and `copyPWD()` (C)
   - `clipboardWriter func(string) error` field in model allows test injection
   - Default: `clipboard.WriteAll`

6. **Tests**:
   - `TestCopy` — basic copy/paste with duplicate renaming (file(1).txt)
   - `TestPasteItem` — 5 table-driven test cases covering copy, cut, prevention, multi-items, duplicates
   - `TestCopyPath` — text clipboard integration test
   - `clipboard/model_test.go` — render and prune tests
   - Utility helpers: `verifyClipboardState()`, `performCopyOrCutOperation()`, `verifySuccessfulPasteResults()`

**The gap**: Copying files in superfile (`Ctrl+C`) stores paths internally. You cannot `Ctrl+V` those files into Finder (macOS), Explorer (Windows), or Nautilus (Linux). Conversely, files copied externally cannot be pasted into superfile.

---

### Affected Areas

| File | Why Affected |
|------|-------------|
| `src/internal/ui/clipboard/model.go` | Core clipboard model — needs new fields/methods for OS-level clipboard sync |
| `src/internal/handle_file_operations.go` | `copySingleItem()`, `copyMultipleItem()`, `getPasteItemCmd()` — entry points for copy/paste flow |
| `src/internal/file_operations.go` | File I/O functions (`copyFile`, `pasteDir`, `moveElement`) — need OS clipboard integration hook |
| `src/internal/type.go` | `model` struct — `clipboard` and `clipboardWriter` fields may need extension |
| `src/internal/default_config.go` | Initialization of clipboard-related fields |
| `src/internal/model_msg.go` | `PasteOperationMsg` — clipboard cleanup after paste (currently only clears on cut) |
| `src/internal/key_function.go` | Key handling entry points for copy/cut/paste (already wired, but paste needs OS clipboard read) |
| `src/internal/common/config_type.go` | `HotkeysType` — may need new fields for OS-clipboard-specific bindings |
| `src/superfile_config/hotkeys.toml` | Default hotkey bindings (may need additional entries) |
| `src/internal/common/string_function.go` | `ClipboardPrettierName` — existing render utility |
| `src/internal/ui/clipboard/model_test.go` | Need tests for OS clipboard integration |
| `src/internal/handle_file_operation_test.go` | Need tests for cross-app paste scenarios |
| `src/internal/ui/spf_renderers.go` | Clipboard footer renderer (may need cut/copy visual indicator) |
| `go.mod` | May need new dependency for OS file clipboard (e.g., a Go library for NSPasteboard/Win32 CF_HDROP) |

---

### Approaches

#### Approach 1: OS Clipboard via file:// URI Scheme + Platform Scripts

**Description**: When user copies files in superfile, write file:// URIs to the OS clipboard using platform-specific commands (pbcopy/osascript on macOS, PowerShell on Windows, xclip/wl-clipboard on Linux). When pasting, read file URIs from the OS clipboard and convert to file paths.

- **macOS**: Use `osascript` to write AppleScript `set the clipboard to (read (POSIX file path) as «class furl»)`, or call NSPasteboard via a small Go CGO helper
- **Linux**: Write `xclip -selection clipboard -t text/uri-list` or `wl-clipboard` with `x-copy` for file URIs
- **Windows**: Use PowerShell to set `[System.Windows.Forms.Clipboard]::SetFileDropList()` or call `OleSetClipboard` with `CF_HDROP`

**Pros**:
- True cross-application file clipboard integration
- Can paste files from Finder/Explorer/Nautilus into superfile
- Conceptually clean — one clipboard to rule them all

**Cons**:
- Three platform-specific implementations (high maintenance)
- No pure Go library exists for file-level OS clipboard (only text/image)
- `github.com/atotto/clipboard` only does text — would need new dependency or CGO
- macOS AppleScript approach is fragile and slow
- Platform-specific testing complexity
- Must handle race conditions with external clipboard changes

**Effort**: High

#### Approach 2: Enhanced Internal Clipboard + Optional OS Sync

**Description**: Keep the internal clipboard as the primary mechanism, but add a background sync layer. When items are copied internally, optionally write a text list of paths to the OS clipboard (as plain text). Add a CLI/config toggle for "integrate with system clipboard". Reading from OS clipboard when pasting is an opt-in feature.

**Pros**:
- Low complexity — reuses existing `github.com/atotto/clipboard` for text
- Users can paste file paths into other apps (useful for scripting)
- Minimal platform-specific code
- Easy to test — just verify text was written
- Backward compatible

**Cons**:
- Limited cross-app file transfer (only paths as text, not actual file objects)
- Finder/Explorer won't recognize plain text paths as files to paste
- Splits the clipboard into "internal" and "external" — can cause confusion

**Effort**: Low-Medium

#### Approach 3: Internal-Only (Current Behavior) + Visual Improvements

**Description**: Leave the internal clipboard as-is, but improve the UX: add visual indicators (icon showing copy vs cut), better keyboard shortcut discoverability, and ensure the clipboard panel is always visible during copy operations.

**Pros**:
- Zero platform complexity
- Pure Go, no new dependencies
- Easy to test
- Already works for the superfile-only workflow

**Cons**:
- No cross-app clipboard support
- Users can't integrate superfile with their OS file manager
- Feature parity gap vs other file managers (ranger, lf, yazi)

**Effort**: Low

#### Approach 4: Hybrid — Internal Clipboard + OS File Clipboard (Your Recommendation)

**Description**: Implement a dual-clipboard system:
- **Internal clipboard** (existing): stores paths for cut/copy within superfile, with cut state tracking
- **OS file clipboard** (new): syncs file:// URIs to the platform clipboard for cross-app paste

When `Ctrl+C` is pressed in superfile:
1. Store paths in internal clipboard (existing)
2. Write file:// URIs to OS clipboard using platform-specific tooling

When `Ctrl+V` is pressed in superfile:
1. Check internal clipboard first (priority)
2. If internal clipboard is empty, try reading OS clipboard for file:// URIs
3. If neither has file references, show notification

On macOS, a clean approach would be to shell out to a small Swift helper or use `osascript` to write NSPasteboard with file promises.

**Pros**:
- Best of both worlds — internal tracking + external compatibility
- Cut-state preserved internally even when OS clipboard only supports copy
- Users can copy from superfile and paste in Finder and vice versa
- Pattern matches established file managers (Yazi uses this approach)

**Cons**:
- Medium complexity — platform-specific integration required
- Need new dependency or embedded platform helpers
- OS clipboard is "copy-only" (no cut state) — must maintain internal cut state
- Edge cases: what happens when OS clipboard has both files and text?

**Effort**: Medium-High

---

### Recommendation

**Approach 4 (Hybrid)** is the right call for a file manager aiming at production quality. However, I'd recommend implementing it **in phases**:

1. **Phase 1 (Low effort, immediate value)** — Write file paths as text to the OS clipboard during copy/cut. This is a 3-line change in `copySingleItem()`/`copyMultipleItem()` to call `m.writeClipboard()` with the joined paths. Users can at least paste paths into text editors, terminals, and chat. No read-from-OS needed yet.

2. **Phase 2 (Medium effort)** — Implement OS file clipboard **writing** using platform mechanisms. On macOS, embed a small CGO helper or shell out to `osascript -e 'set the clipboard to ...'` with file URLs. On Linux, use `xclip` or `wl-clipboard`. On Windows, use PowerShell.

3. **Phase 3 (Higher effort)** — Implement OS file clipboard **reading** when internal clipboard is empty. Parse file:// URIs from OS clipboard and treat them as paste sources.

4. **Phase 4** — Add visual indicator in the clipboard footer panel showing whether items are also synced to OS clipboard.

---

### Approach Comparison

| Approach | Cross-App Paste | Complexity | New Deps | Test Effort | Risk |
|----------|----------------|------------|----------|-------------|------|
| 1. Pure OS Clipboard | ✅ Full | High | Yes (CGO or platform helpers) | High | Medium |
| 2. Internal + Text Sync | ❌ Partial (text only) | Low | None | Low | Low |
| 3. Internal Only | ❌ None | None | None | None | None |
| 4. Hybrid (Recommended) | ✅ Full | Medium | Yes (platform helpers) | Medium | Low-Med |

---

### Risks

- **ctrl+c conflict**: `cancel_typing` also uses `ctrl+c`. This is already handled by state priority in `handleKeyInput()` (typing modal checked before `mainKey()`), but any change to copy behavior must not regress this.
- **OS clipboard format fragmentation**: Each platform has a different file clipboard format. macOS uses NSPasteboard with file promises, Linux uses `text/uri-list`, Windows uses `CF_HDROP`. Getting all three right is non-trivial.
- **Clipboard polling**: If we want to "paste from OS clipboard," we need to decide when to poll the OS clipboard. Polling on every `Ctrl+V` is simplest, but we could also watch for changes.
- **Cut state**: The OS clipboard has no "cut" concept. Internal state must track whether an operation is a cut (to delete originals after paste). If the user copies in superfile, then copies something else externally, the internal cut state becomes stale.
- **Large file lists**: The OS clipboard has limits on how many file URIs can be stored. Internal clipboard is unconstrained.
- **Duplicate rename**: Currently handled for internal operations. Same logic applies regardless of clipboard source.

---

### Ready for Proposal

**Yes** — the exploration is thorough and the hybrid approach is clear. The orchestrator should present the phased plan to the user, confirm which platforms are priority (macOS only? all three?), and decide whether Phase 1 (text sync only) is sufficient for now or if full cross-app integration is required.
