# Research Findings

Back-filled from a live debugging session on 2026-08-17/18 rather than from a fresh agent
sweep, per the "Skipping this phase" clause in `.claude/commands/qrspi/2_research.md`. The
session produced two live reproductions on the macOS dev build, a temporary parameter audit,
a cross-version log comparison, and a page capture. Findings below cite either `file:line`
or the observation that produced them.

## Q1: How the OTP fetch runs end to end

`FetchOTP` (`internal/beanfun/otp.go:41`) runs six steps in fixed order and returns the
decrypted token.

| Step | Call | Host | Purpose |
|---|---|---|---|
| 1 | `game_zone/game_start_step2.aspx` (`otp.go:75`) | `tw.beanfun.com` | scrape long-polling key, `unk_data` pair, createTime |
| 2 | `generic_handlers/get_cookies.ashx` (`otp.go:132`) | `tw.newlogin.beanfun.com` | scrape `m_strSecretCode` |
| 3 | `generic_handlers/record_service_start.ashx` (`otp.go:164`) | `tw.beanfun.com` | POST form; body discarded, primes server state |
| 4 | `generic_handlers/get_result.ashx` (`otp.go:201`) | `tw.beanfun.com` | long-poll trigger; body discarded |
| 5 | `generic_handlers/get_webstart_otp.ashx` (`otp.go:248`) | `tw.beanfun.com` | GET; returns the OTP envelope |
| 6 | `decryptOTP` (`wcdes.go:18`) | — | local DES-ECB decrypt |

Steps 1 and 2 guard against empty scrapes and fail at their own step (`otp.go:106-117`,
`otp.go:152-155`). Step 1 also short-circuits on an expired session (`otp.go:103`).

Host bases are defined in `client.go:26-28` and constructed in `client.go:41-43`. The
User-Agent is a pinned Chrome string (`client.go:16`).

Observed live (both reproductions): steps 1-4 all returned HTTP 200 and step 5 returned
HTTP 200 with a 28-byte body. Step 1 reported `long_polling_key_len=36` and
`create_time="2022-05-02 16:31:44"`; step 2 reported `secret_code_len=32`.

## Q2: How step 5's query string is assembled

`otpStep5` builds one format string with `fmt.Sprintf` (`otp.go:253-265`) carrying nine
parameters in this order: `SN`, `WebToken`, `SecretCode`, `ppppp`, `ServiceCode`,
`ServiceRegion`, `ServiceAccount`, `CreateTime`, `d`.

- **No value is URL-encoded.** Only `CreateTime`'s space is hand-substituted to `%20`
  (`otp.go:252`). The function's own comment (`otp.go:229-247`) states the assumption that
  every other value is URL-safe.
- `ppppp` is a hardcoded 64-char uppercase hex constant whose comment says
  "Provenance unknown" (`otp.go:13-18`).
- `SN` is the long-polling key scraped in step 1, not the account SSN
  (`docs/beanfun-login-protocol.md:464`).
- By contrast, the account-list call encodes the same `WebToken` properly through
  `url.Values` (`accounts.go:98`).

A temporary audit inserted before the request confirmed the assembled query is well formed:
all four scraped/session values reported no characters requiring encoding,
`len(parsed.Query()) == 9`, and `url.ParseQuery(parsed.RawQuery)` returned no error.

## Q3: Error classification and how it surfaces

`LoginErrorKind` is an iota enum (`errors.go:11-27`); `KindOTPServerRejected` is the twelfth
member, so it serialises as `11`. `decryptOTP` raises it whenever the envelope's first
segment is not `1` (`wcdes.go:42-43`), passing the server's payload through as the message
(`errors.go:105`).

Session expiry is detected by a single substring test on the response body —
`strings.Contains(body, "尚未登入")` (`errors.go:167-168`) — and folded into
`ErrLoginRequired` by the launcher service (`internal/launcher/service.go:506-508`). The
frontend reacts only to the literal `"login required"` substring (`HomePage.tsx:85`); any
other error string leaves the app in its logged-in state.

Errors from several paths embed up to 500 bytes of the server response
(`bodyPreviewLimit`, `errors.go:138`; `withBody`, `errors.go:149-154`), for example
`otp.go:108`. Those errors reach both the log file and the UI.

Log destination is a per-version file under the OS cache dir (`main.go:108-160`):
`~/Library/Caches/beanfun-launcher/launcher-dev.log` on the dev machine.

## Q4: What the step-1 page contains

A capture of the live `game_start_step2.aspx` response (16,934 bytes) shows:

- The three literals the current scrapers consume: the `GetResultByLongPolling&key=` key
  (`parser.go:47`), the `MyAccountData` `unk_data` pair, and `ServiceAccountCreateTime`.
- A `var m_objData` object literal with exactly three properties: `region`, `sn`, `data`.
  `region` is the literal string `TW;Production`. `sn` is a 36-char GUID and is **byte-for-byte
  the same value as the long-polling key** the existing scraper extracts. `data` was 553
  characters.
- A `supportService` allowlist containing `610074` (MapleStory TW), which selects
  `SmartLaunch` over `LaunchGame` in the page's own handoff function.
- **No occurrence of `get_webstart_otp` and no occurrence of `ppppp`.** The page's referenced
  scripts do not contain them either; `game_zone/scripts/ggm.js` (fetched separately, 11,649
  bytes) has no OTP-related identifier. The browser flow hands off to a locally installed
  native helper instead of calling the OTP endpoint itself.

Arithmetic on the captured `data`: first character `8` → selector 8; removing the selector
and eight more characters leaves 544 hex characters = 272 bytes = 34 whole DES blocks.

## Q5: Code paths that contact `tw.beanfun.com`

- OTP steps 1, 3, 4, 5 — four requests per OTP fetch (`otp.go:75`, `:164`, `:201`, `:248`).
- Session-key handshake, one request per QR login start (`session_key.go`, called from
  `service.go:88`+). `StartQRLogin` builds a fresh client and jar on every call
  (`service.go:118`).
- Keep-alive ping to `echo_token.ashx` (`client.go:173`+), scheduled at 60 s on success and
  10 s after a failure (`service.go:61-62`) by an uncapped heartbeat loop
  (`internal/bgtask/manager.go:69-88`). `Ping` only errors on transport failure or HTTP ≥ 400.

Measured live against the portal: roughly five requests to the login entry path within a
short window trigger an IP frequency lock that 302s to `/TW/BlockIPMessage.htm` and returns
**HTTP 200**, so the `>= 400` guard at `session_key.go:33` does not catch it and the flow
fails at the regex instead (`session_key.go:44-47`). The lock is scoped to that path —
`game_start_step2.aspx`, `echo_token.ashx` and the portal home page kept serving normally
while it was active — and clears after a couple of minutes. Tracked separately as #83.

## Q6: The existing DES helper

`decryptOTP` (`wcdes.go:18`) parses a `<status>;<payload>` envelope, takes the payload's
first 8 characters as an ASCII DES key and the remainder as hex ciphertext, then runs
DES-ECB with no padding and trims NUL bytes. The envelope parsing and the raw decryption are
currently one function; the decryption half is not separately callable.

## Cross-Cutting Observations

- The server-side contract changed under unchanged client code. The same log file records a
  successful fetch on 2026-05-13 (`envelope_len=42`, followed by `FetchOTP: token acquired`)
  and the current failure (`envelope_len=28`). 42 matches the documented
  `1;{8-char key}{32 hex}` success shape; the 28-byte failure body is `0;`, eight spaces,
  then `Query String Error`.
- Test fixtures use trivially URL-safe values for tokens (`"TKN"`, `"z"`,
  `"CANONICAL_TOKEN"`), so no existing test would detect an encoding defect in step 5.
- Three scrapers use greedy `(.*)` captures (`parser.go:47`, `:50`, `:56`). They produced
  normal lengths in both reproductions, so they are not implicated in the current failure,
  but they capture to the last terminator on the line.
- The independently observable attestation inputs for the replacement endpoint are properties
  of an installed file, not of the session: on this machine
  `C:\Program Files\gamania Games\gamania Games Manager\GGMWebStart.dll` (1,289,232 bytes)
  reports assembly version `1.5.0.2`, matching its PE file version, and SHA-256
  `dfd568a69d87abcd8f4a93d1a4481ebb57712d1d28ab0b6fc018fcf140101e06`.

## Open Areas

- Whether steps 2, 3 and 4 are still required once step 5 changes. Nothing in the capture
  proves they are, and nothing proves they are not.
- Whether the replacement endpoint's response field is the same
  `{8-char key}{hex ciphertext}` envelope as the retired one. Its documented length of 40
  is consistent with 8 + two DES blocks, but no plaintext has been observed.
- The exact ordering inside the blob decoder — whether the substitution mapping is applied
  before or after the embedded key is lifted out. Both readings satisfy the length
  arithmetic in Q4.
- Whether the four substitution tables referenced by the community analysis match the
  installed helper. Not verified against the binary.
