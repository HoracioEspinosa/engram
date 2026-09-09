[← Back to README](../README.md)

# Installation

- [Homebrew (macOS only)](#homebrew-macos-only)
- [Windows](#windows)
- [Install from source (macOS / Linux)](#install-from-source-macos--linux)
- [Download binary (all platforms)](#download-binary-all-platforms)
- [Requirements](#requirements)
- [Environment Variables](#environment-variables)
- [Windows Config Paths](#windows-config-paths)

---

## Homebrew (macOS only)

The tap ships a Homebrew **cask**, and Homebrew Cask only installs on macOS — there is no
Homebrew path on Linux, regardless of the platforms the fork itself builds for. Linux users want
[Install from source](#install-from-source-macos--linux) or [Download binary](#download-binary-all-platforms)
below.

```bash
brew install --cask HoracioEspinosa/tap/engram-custom
```

This installs from **this fork's own tap** (`HoracioEspinosa/homebrew-tap`) — not
`gentleman-programming/tap`, which ships upstream engram without the ClaroDrive cloud
configuration or this fork's fixes.

Upgrade to latest:

```bash
brew update && brew upgrade --cask engram-custom
```

> **Migrating from the formula?** `HoracioEspinosa/homebrew-tap` used to also carry a
> hand-written `Formula/engram.rb`, pinned to an older version than the cask published by the same
> release. It has been retired — the cask is now the tap's only artifact. If you installed through
> that formula, switch over:
> ```bash
> brew uninstall engram; brew install --cask HoracioEspinosa/tap/engram-custom
> ```

> **Keep `engram serve` running across `brew upgrade`?** On macOS, `brew upgrade --cask engram-custom` replaces the binary and kills any running `engram serve` process — autosync stops silently until you relaunch it. To make autosync survive upgrades and reboots, use the launchd template in [Running as a Service → Using launchd (macOS)](../DOCS.md#using-launchd-macos). Run `engram cloud status` afterwards: the `Local daemon:` line should report `running`.

---

## Windows

**Option A: Build from source (recommended for technical users)**

If you have Go installed, this is the cleanest and most trustworthy path — the binary is compiled on your machine from source, so no antivirus will flag it. A network `go install github.com/HoracioEspinosa/engram/...@latest` does not work for this fork: its `go.mod` still declares the upstream import path, so `go install` rejects a module resolved under this fork's path with a "module declares its path as" mismatch. Cloning first sidesteps that, since a local build resolves its own package paths instead of a remote module path:

```powershell
git clone https://github.com/HoracioEspinosa/engram.git
cd engram
go install ./cmd/engram
# Binary goes to %GOPATH%\bin\engram.exe (typically %USERPROFILE%\go\bin\)
```

Ensure `%GOPATH%\bin` (or `%USERPROFILE%\go\bin`) is on your `PATH`.

> **Want a real version string instead of `dev`?**
>
> `go install` always stamps the binary as `dev`. To get a meaningful version, pick one of these — not both. Running them both leaves two binaries on disk and `engram version` keeps reporting `dev` because PATH still resolves to the `go install` build.
>
> **Option A1 — version-stamped `go install` (binary stays on PATH):**
>
> ```powershell
> $v = git describe --tags --always
> go install -ldflags="-X main.version=local-$v" ./cmd/engram
> ```
>
> **Option A2 — `go build` and move the result onto PATH:**
>
> ```powershell
> $v = git describe --tags --always
> go build -ldflags="-X main.version=local-$v" -o engram.exe ./cmd/engram
> Move-Item -Force engram.exe "$env:USERPROFILE\go\bin\engram.exe"
> ```
>
> After either option, `engram version` should print `local-<git-describe>` instead of `dev`.

**Option B: Download the prebuilt binary**

1. Go to [GitHub Releases](https://github.com/HoracioEspinosa/engram/releases) (this fork's
   own repository, not `Gentleman-Programming/engram`)
2. Download `engram_<version>_windows_amd64.zip` (or `arm64` for ARM devices)
3. Extract `engram.exe` to a folder in your `PATH` (e.g. `C:\Users\<you>\bin\`)

```powershell
# Example: extract and add to PATH (PowerShell)
Expand-Archive engram_*_windows_amd64.zip -DestinationPath "$env:USERPROFILE\bin"
# Add to PATH permanently (run once):
[Environment]::SetEnvironmentVariable("Path", "$env:USERPROFILE\bin;" + [Environment]::GetEnvironmentVariable("Path", "User"), "User")
```

> **Antivirus false positives on prebuilt binaries**
>
> Windows Defender and other antivirus tools (ESET, Brave's built-in scanner) have flagged some
> engram prebuilt releases as malware (`Trojan:Script/Wacatac.H!ml` or similar). This is a
> **heuristic false positive**. The binary is built reproducibly from the public source code
> via GoReleaser and contains no malicious code.
>
> **Why does this happen?** Prebuilt binaries from small open-source projects are unsigned (code
> signing certificates cost hundreds of dollars per year). Many AV engines automatically flag
> unsigned executables from unknown publishers, especially recently compiled Go binaries. The
> same alert has been observed on Claude Code's own MSIX installer, which confirms this is an
> AV heuristic issue, not a code problem.
>
> **Maintainer stance:** We will not pay for a code signing certificate at this time. This is a
> distribution trust problem, not a security problem. The source code is fully auditable.
>
> **Recommended workaround:** Technical Windows users should prefer **Option A (build from
> source)**. Binaries you compile locally will not trigger AV alerts because they originate from
> your own machine.

> **Other Windows notes:**
> - Data is stored in `%USERPROFILE%\.engram\engram.db`
> - Override with `ENGRAM_DATA_DIR` environment variable
> - All core features work natively: CLI, MCP server, TUI, HTTP API, Git Sync
> - No WSL required for the core binary — it's a native Windows executable

---

## Install from source (macOS / Linux)

```bash
git clone https://github.com/HoracioEspinosa/engram.git
cd engram
go install ./cmd/engram
# Binary goes to $GOPATH/bin (typically ~/go/bin/)
```

> **Want a real version string instead of `dev`?**
>
> `go install` always stamps the binary as `dev`. To get a meaningful version, pick one of these — not both. Running them both leaves two binaries on disk and `engram version` keeps reporting `dev` because PATH still resolves to the `go install` build.
>
> **Option 1 — version-stamped `go install` (binary stays on PATH):**
>
> ```bash
> go install -ldflags="-X main.version=local-$(git describe --tags --always)" ./cmd/engram
> ```
>
> **Option 2 — `go build` and move the result onto PATH:**
>
> ```bash
> go build -ldflags="-X main.version=local-$(git describe --tags --always)" -o engram ./cmd/engram
> mv engram "$(go env GOPATH)/bin/engram"
> ```
>
> After either option, `engram version` should print `local-<git-describe>` instead of `dev`.

---

## Download binary (all platforms)

Grab the latest release for your platform from
[GitHub Releases](https://github.com/HoracioEspinosa/engram/releases) (this fork's repository,
not `Gentleman-Programming/engram`). Every release here is marked pre-release, so GitHub's
`/releases/latest` alias does not resolve to it — name the tag explicitly:

```bash
curl -LO https://github.com/HoracioEspinosa/engram/releases/download/<tag>/engram_<version>_darwin_arm64.tar.gz
curl -LO https://github.com/HoracioEspinosa/engram/releases/download/<tag>/checksums.txt
# or, with gh:
gh release download <tag> --repo HoracioEspinosa/engram --pattern 'engram_*_darwin_arm64.tar.gz' --pattern checksums.txt
```

| Platform | File |
|----------|------|
| macOS (Apple Silicon) | `engram_<version>_darwin_arm64.tar.gz` |
| macOS (Intel) | `engram_<version>_darwin_amd64.tar.gz` |
| Linux (x86_64) | `engram_<version>_linux_amd64.tar.gz` |
| Linux (ARM64) | `engram_<version>_linux_arm64.tar.gz` |
| Windows (x86_64) | `engram_<version>_windows_amd64.zip` |
| Windows (ARM64) | `engram_<version>_windows_arm64.zip` |

Every release also carries a `checksums.txt`. Verify the archive before extracting it —
whichever way you downloaded it:

```bash
grep engram_<version>_darwin_arm64.tar.gz checksums.txt | shasum -a 256 -c -
```

---

## Requirements

- **Go 1.24+** to build from source (not needed if installing via Homebrew or downloading a binary)
- That's it. No runtime dependencies.

The binary includes SQLite (via [modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) — pure Go, no CGO). Works natively on **macOS**, **Linux**, and **Windows** (x86_64 and ARM64).

---

## Environment Variables

| Variable | Description | Default |
|---|---|---|
| `ENGRAM_DATA_DIR` | Data directory | `~/.engram` (Windows: `%USERPROFILE%\.engram`) |
| `ENGRAM_PORT` | HTTP server port | `7437` |

---

## Windows Config Paths

When using `engram setup`, config files are written to platform-appropriate locations:

| Agent | macOS / Linux | Windows |
|-------|---------------|---------|
| OpenCode | `~/.config/opencode/` | `%APPDATA%\opencode\` |
| Gemini CLI | `~/.gemini/` | `%APPDATA%\gemini\` |
| Codex | `~/.codex/` | `%APPDATA%\codex\` |
| Claude Code | Managed by `claude` CLI | Managed by `claude` CLI |
| Antigravity CLI | `~/.gemini/config/mcp_config.json` + `~/.gemini/GEMINI.md` | `%APPDATA%\gemini\config\mcp_config.json` + `%APPDATA%\gemini\GEMINI.md` |
| Windsurf | `~/.codeium/windsurf/mcp_config.json` + `.../memories/global_rules.md` | `%USERPROFILE%\.codeium\windsurf\...` |
| Qwen Code | `~/.qwen/settings.json` + `~/.qwen/QWEN.md` | `%USERPROFILE%\.qwen\...` |
| Kiro | `~/.kiro/settings/mcp.json` + `~/.kiro/steering/engram.md` | `%USERPROFILE%\.kiro\...` |
| Cursor | `~/.cursor/mcp.json` + `~/.cursor/rules/engram.mdc` | `%USERPROFILE%\.cursor\...` |
| VS Code Copilot | `~/.config/Code/User/mcp.json` + `.../prompts/engram.instructions.md` (macOS: `~/Library/Application Support/Code/User/`) | `%APPDATA%\Code\User\...` |
| Kilo Code | `~/.config/kilo/opencode.json` + `~/.config/kilo/AGENTS.md` | `%USERPROFILE%\.config\kilo\...` |
| Data directory | `~/.engram/` | `%USERPROFILE%\.engram\` |
