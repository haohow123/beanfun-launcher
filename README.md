# beanfun-launcher

> ⚠️ **個人實驗性專案 / Personal experimental project** — usable on Windows, still early: features and UI may change between releases.

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

最新版本 / Latest: **[Releases](https://github.com/haohow123/beanfun-launcher/releases/latest)** (Windows)

- Installer: [`beanfun-launcher-amd64-installer.exe`](https://github.com/haohow123/beanfun-launcher/releases/latest/download/beanfun-launcher-amd64-installer.exe)
- Portable: [`beanfun-launcher.exe`](https://github.com/haohow123/beanfun-launcher/releases/latest/download/beanfun-launcher.exe)

## 使用方式 / How to use

### 開啟

安裝版或免安裝版擇一即可。每次開啟時 Windows 都會跳出系統管理員權限確認（UAC）：
新楓之谷本身以系統管理員身分執行，launcher 也要同樣的權限才能把帳密送進遊戲視窗。

### 登入 Beanfun

1. 開啟後畫面中央是 QR code，用 **Beanfun 手機 app** 掃描並在手機上確認。
2. QR code 過期會變淡，按「重新產生 QR code」換一張。
3. 登入狀態只存在記憶體裡，不會寫進磁碟，所以每次重開 launcher 都要重新掃碼。

### 進入遊戲

首頁上方是「啟動(可切換帳號)」按鈕，下面每個 Beanfun 帳號一組，列出它底下的遊戲帳號。
每個遊戲帳號有兩個按鈕：

| 按鈕 | 做什麼 |
|---|---|
| 進入遊戲 | 遊戲還沒開：用這個帳號直接開遊戲，略過遊戲的登入畫面。<br>遊戲已經開在登入畫面：把帳號和一次性密碼打進遊戲視窗並送出（遊戲的帳號欄要是游標所在的位置）。 |
| 複製帳密 | 產生新的一次性密碼，把「帳號、換行、密碼」複製到剪貼簿，自己貼進遊戲。 |

- **啟動(可切換帳號)**：開遊戲但停在遊戲自己的登入畫面，不帶任何帳號。直接帶帳號開的遊戲回不到登入畫面；
  想在同一個遊戲視窗裡換帳號，就先用這個按鈕開遊戲，再按要用的帳號的「進入遊戲」。
- 如果打字進遊戲失敗，帳號旁會顯示「手動貼上」並列出一次性密碼，按「複製」自己貼上。
- 「進入遊戲」開遊戲或打字進遊戲視窗前，會先確認遊戲主程式有 NEXON 簽章；簽章不符就停下並說明原因，不會送出帳密。

### 多個 Beanfun 帳號

- 按「＋ 新增 Beanfun 帳號」再掃一次 QR code，最多同時登入 5 個；「← 返回」可取消。
  掃到已經登入的帳號時，會取代原本那一組，不會多開一組。
- 群組標題左邊的箭頭可以收合；名稱旁的 ✎ 可以改名（最多 12 字，Enter 或點旁邊就儲存、Esc 取消、清空恢復預設名稱），
  名稱會依 Beanfun 帳號記住，下次登入同一個帳號會自動套用。
- 每組的「登出」只登出那一組；右上角的「全部登出」會登出全部並回到登入畫面。
- 某個帳號在別處登入或登入失效時，launcher 下一次檢查連線（約每 8 分鐘一次）就會發現，那一組會消失並出現黃色提示。

### 設定（右上角齒輪）

- **遊戲位置**：預設從登錄檔自動偵測。找不到或想指定別的位置時，按「瀏覽…」選 `MapleStory.exe`，
  再按「測試」確認。齒輪上出現黃色警示，代表遊戲位置沒通過檢查，或找不到提示音檔案。
- **伺服器開機提示音**：伺服器從關閉變成開啟時播放，並跳出 Windows 通知。可選 Windows 內建音效、
  用「加入音檔…」加入自己的 WAV 檔，或選「無聲」（通知仍會出現）。
- 畫面上方標題旁的圓點是伺服器狀態，每 2 分鐘檢查一次。

### 出問題時

- Log：`%LOCALAPPDATA%\beanfun-launcher\launcher-<版本>.log`
- 設定檔：`%APPDATA%\beanfun-launcher\settings.json`（存提示音、遊戲位置和群組名稱，不含登入憑證，見 [`PRIVACY.md`](./PRIVACY.md)）
- 回報問題：[GitHub Issues](https://github.com/haohow123/beanfun-launcher/issues)

## Why this exists

Community launchers exist and work, but I want to audit every line that touches
my Gamania credentials. This is a personal rewrite for that reason — not a
replacement product, and not maintained as such. If you want a polished
community launcher, look at [pungin/Beanfun](https://github.com/pungin/Beanfun).

## Security principles (non-negotiable)

1. **No plaintext passwords** — OS keyring only (macOS Keychain / Windows DPAPI).
   In practice this launcher uses QR-code login, so passwords are never typed.
2. **Network requests only to Gamania-owned domains** (`tw.beanfun.com`,
   `login.beanfun.com`, `tw.newlogin.beanfun.com`, `tw.hicdn.beanfun.com`), plus a
   bare TCP connect to MapleStory's login servers (`202.80.104.24` / `.27`, port 8484)
   to show whether the game server is up; no data is sent on that connection.
3. **Tokens cleared from memory immediately after use.**
4. **No third-party login providers.** Direct to Gamania only.
5. **No telemetry. No analytics. No auto-updater without signature checks.**

See [`CLAUDE.md`](./CLAUDE.md) for the full project conventions and threat model.

## Stack

- [Wails v3](https://v3.wails.io) (beta) — desktop shell, uses native WebView2 on Windows
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
