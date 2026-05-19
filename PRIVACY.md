# Privacy Policy

beanfun-launcher does not collect, transmit, store, or share any
personal data beyond what is strictly required to operate.

## What the app does

- Logs in to Beanfun via QR code against Gamania's own servers.
- Launches the MapleStory game executable on your local machine.
- Probes Gamania game-server IPs to display server-up status
  (TCP SYN + close, no application data sent).

## What the app does NOT do

- No telemetry, no analytics, no error reporting "phone home".
- No data sent to any third-party domain. All network traffic
  stays within Gamania-owned endpoints (`bfweb.gamania.com`,
  `tw.beanfun.com`, `tw.newlogin.beanfun.com`, etc., plus the
  game-server IPs `202.80.104.24-29:8484` for the status probe).
- No third-party login providers, no OAuth proxies.
- No auto-update mechanism that downloads code from external
  sources.
- No reading of files outside what is needed to locate the game
  executable (the `BEANFUN_GAME_EXE` environment variable or
  Beanfun's published registry entry).

## Local data

- **Session tokens** (SKey, WebToken) live only in process
  memory and are zeroed on Reset / logout. Never written to disk.
- **OTP** credentials are zeroed after being injected into the
  game's login form.
- **Log file** under `~/Library/Caches/beanfun-launcher/` (macOS)
  or `%LOCALAPPDATA%\beanfun-launcher\` (Windows) records
  operational events with all token values redacted. The file
  is local — never uploaded, never shared.

## Open source

The full source is publicly auditable at
<https://github.com/haohow123/beanfun-launcher>. Anyone can
verify that the claims above match the code.

## Affiliation

beanfun-launcher is **not** affiliated with, endorsed by, or
sponsored by Gamania. All trademarks belong to their respective
owners. Use at your own discretion regarding Gamania's Terms of
Service.

## Contact

Open an issue at
<https://github.com/haohow123/beanfun-launcher/issues>.
