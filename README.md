# beanfun-launcher

> ⚠️ **個人實驗性專案 / Personal experimental project** — pre-alpha, not yet usable.

第三方 [Beanfun!](https://tw.beanfun.com/) 啟動器，從零打造、以「每行 code 都自己審過」為前提。

A personal third-party launcher for Beanfun (遊戲橘子) games, rewritten from scratch
so every line that touches my Gamania credentials is something I audit and own.

## Disclaimer

本專案為**個人**第三方專案，**並非** Gamania / Beanfun 官方產品。
Beanfun™、樂豆™、Gamania™ 商標屬其各自擁有者。
使用本工具登入 Gamania 帳號時，使用者需自行確認是否符合 Gamania 服務條款。

This project is **not** affiliated with, endorsed by, or sponsored by Gamania.
All trademarks belong to their respective owners. Use is at your own risk and
discretion regarding Gamania's Terms of Service.

## Download

> ⚠️ GitHub Releases 頁面排序是字典順,不是版本順 — `alpha.10+` 會被擠到 `alpha.9` 後面。請直接用下方連結下載最新版,不要在 Releases 頁面挑。

最新版本 / Latest: **[v0.1.0-alpha.18](https://github.com/haohow123/beanfun-launcher/releases/tag/v0.1.0-alpha.18)** (Windows)

- Installer: [`beanfun-launcher-amd64-installer.exe`](https://github.com/haohow123/beanfun-launcher/releases/download/v0.1.0-alpha.18/beanfun-launcher-amd64-installer.exe)
- Portable: [`beanfun-launcher.exe`](https://github.com/haohow123/beanfun-launcher/releases/download/v0.1.0-alpha.18/beanfun-launcher.exe)

## Why this exists

Community launchers exist and work, but I want to audit every line that touches
my Gamania credentials. This is a personal rewrite for that reason — not a
replacement product, and not maintained as such. If you want a polished
community launcher, look at [pungin/Beanfun](https://github.com/pungin/Beanfun).

## Security principles (non-negotiable)

1. **No plaintext passwords** — OS keyring only (macOS Keychain / Windows DPAPI).
   In practice this launcher uses QR-code login, so passwords are never typed.
2. **Network requests only to Gamania-owned domains** (`bfweb.gamania.com`,
   `tw.beanfun.com`, `login.beanfun.com`).
3. **Tokens cleared from memory immediately after use.**
4. **No third-party login providers.** Direct to Gamania only.
5. **No telemetry. No analytics. No auto-updater without signature checks.**

See [`CLAUDE.md`](./CLAUDE.md) for the full project conventions and threat model.

## Stack

- [Wails v3](https://v3.wails.io) (alpha) — desktop shell, uses native WebView2 on Windows
- Go — backend (Beanfun API client, OS keyring, Win32 DLL injection for locale emulation)
- React + TypeScript + Vite + Tailwind v4 + shadcn/ui — frontend
- [Locale_Remulator](https://github.com/InWILL/Locale_Remulator) — pre-built C++ DLL for
  region emulation so games with locale dependencies run on a TW system. We integrate
  via injection; we do **not** author the DLL.

## Platforms

- **Development**: macOS
- **Target**: Windows 10 / 11 only

Linux and macOS as launch targets are out of scope.

## Build

Uses [Task](https://taskfile.dev) as the canonical entry point (wraps `wails3` with
the project's `build/config.yml` and pinned Vite port):

```
task dev               # Hot reload dev server
task build             # Build for current OS
task windows:build     # Cross-compile Windows binary (the deploy target)
```

## License

[MIT](./LICENSE)
