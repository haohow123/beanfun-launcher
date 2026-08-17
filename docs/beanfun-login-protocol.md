# Beanfun QR-login protocol

Wire specification for the Beanfun (遊戲橘子) QR login flow as implemented
by `internal/beanfun/`. This document describes the Gamania portal's
observed behaviour and the decisions our Go client makes in response.

Cross-references to the upstream pungin/Beanfun Rust launcher are
collected in the [References](#references) appendix for maintainers
who want to compare wire shape against another implementation.

## Overview

A seven-call sequence whose front half is driven by frontend polling
and whose back half (steps 4–7, the "finalize" handshake) is a single
backend-internal cascade.

```
                                                         CLIENT
Step 0  getSessionKey         GET   tw.beanfun.com/beanfun_block/bflogin/default.aspx
Step 1  Login/Index           GET   login.beanfun.com/Login/Index?pSKey={skey}
Step 2  Login/InitLogin       GET   login.beanfun.com/Login/InitLogin?pSKey={skey}
        ──── frontend now polls step 3 every 2 seconds ────
Step 3  QRLogin/CheckLoginStatus    POST  login.beanfun.com/QRLogin/CheckLoginStatus
        ──── when step 3 returns "Success", finalize runs ────
Step 4  QRLogin/QRLogin       GET   login.beanfun.com/QRLogin/QRLogin
Step 5  Login/SendLogin       GET   login.beanfun.com/Login/SendLogin
Step 6  return.aspx (3rd)     POST  tw.beanfun.com/beanfun_block/bflogin/return.aspx
Step 7  return.aspx (4th)     POST  tw.beanfun.com/beanfun_block/bflogin/return.aspx   ← bfWebToken read from jar after
```

Output: an authenticated session keyed by the `bfWebToken` cookie
value, kept in process memory only.

## Step 0 — `getSessionKey`

```
GET https://tw.beanfun.com/beanfun_block/bflogin/default.aspx?service=999999_T0
User-Agent: Mozilla/5.0 … Chrome/130.0.0.0 …
```

Follow redirects. The portal returns a 302 chain that lands on
`https://tw.newlogin.beanfun.com/checkin_step2.aspx?skey={value}&display_mode=2`
or similar. The session-key value lives in the final URL's query
string.

**Scrape regex:** `[sp][Ss]?[Kk]ey=([^&]+)`.

The permissiveness is deliberate. At the WPF-era the portal emitted
`pSKey=`. The current portal emits `skey=` lowercase. The corresponding
`login.beanfun.com` endpoints still expect the canonical `pSKey=`
name on the way in, regardless of which spelling we extracted.

Failure: if the regex misses, the final URL is logged at WARN with
a 500-byte body preview, and `KindMissingSessionKey` is returned.

## Step 1 — `Login/Index`

```
GET https://login.beanfun.com/Login/Index?pSKey={skey}
Accept: text/html
User-Agent: …
```

Response: HTML portal page. Scrape `__RequestVerificationToken` from
the hidden `<input>` with regex:

```
__RequestVerificationToken[^>]+value="([^"]+)"
```

Absent token is tolerated — downstream endpoints conditionally send
the `RequestVerificationToken` header only when the scraped value is
non-empty.

## Step 2 — `Login/InitLogin`

```
GET https://login.beanfun.com/Login/InitLogin?pSKey={skey}
Accept: application/json, text/plain, */*
Referer: <step-1 URL with pSKey>
Origin: https://login.beanfun.com
X-Requested-With: XMLHttpRequest
User-Agent: …
```

Response (JSON envelope):

```json
{
  "Result": 0,
  "ResultData": {
    "QRImage": "<base64-encoded PNG>",
    "DeepLink": "<optional URL>"
  }
}
```

Validation (any failure → `KindQRInitResult`):

- `Result` is present and equals `0`
- `ResultData` is present
- `ResultData.QRImage` is present and non-empty
- `ResultData.DeepLink` is optional; when present and non-empty, run
  through the deeplink unwrapper

### Deeplink unwrap

When the server wraps the deeplink as
`https://play.games.gamania.com/<path>/deeplink/?url=<encoded inner URL>`,
return the decoded inner URL. Unwrap conditions:

- input is an absolute URL
- host is `play.games.gamania.com` (case-insensitive)
- path contains `"deeplink"` (case-insensitive)
- the `url` query parameter exists and is non-empty

Otherwise pass the input through verbatim.

## Step 3 — `QRLogin/CheckLoginStatus`

Polled every 2 seconds by the frontend while the QR is on screen.

```
POST https://login.beanfun.com/QRLogin/CheckLoginStatus
Accept: application/json, text/plain, */*
Referer: <step-1 URL with pSKey>
Origin: https://login.beanfun.com
Content-Type: application/x-www-form-urlencoded
Content-Length: 0                              ← see Quirks
RequestVerificationToken: <token>               ← only if non-empty
User-Agent: …
Body: <empty>
```

**`Content-Length: 0` must be set explicitly.** Without it Go's
transport picks chunked encoding and the server rejects with HTTP 411
"Length Required".

Response:

```json
{ "ResultMessage": "Wait Login" | "Failed" | "Token Expired" | "Success" }
```

Dispatch:

- `"Wait Login"` → `Pending`. Keep polling.
- `"Failed"` → `Retry`. Server's umbrella term for transient errors
  and user-rejected scan; keep polling (server eventually transitions
  to `Token Expired` if no further action).
- `"Token Expired"` → `Expired`. Caller restarts via `StartQRLogin`.
- `"Success"` → `Approved`. Caller proceeds to finalize.
- Any other / missing field → `KindServerMessage` with raw body preview.

## Step 4 — `QRLogin/QRLogin`

Plain handshake — primes server-side session state. Body is discarded.

```
GET https://login.beanfun.com/QRLogin/QRLogin
Accept: application/json, text/plain, */*
Referer: <step-1 URL with pSKey>
User-Agent: …
```

## Step 5 — `Login/SendLogin`

```
GET https://login.beanfun.com/Login/SendLogin
Accept: text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8
Referer: <step-1 URL with pSKey>
User-Agent: …
```

The Accept header differs from non-QR flows — the `image/avif` /
`image/webp` / `image/apng` tokens are QR-specific.

Response: HTML with a `<form>` of hidden inputs. We scrape every
non-`type="submit"` `<input>` with both `name` and `value` attributes
using a regex parser (no HTML library — the markup shape is stable
enough that this is simpler than walking an AST). Zero scraped inputs
→ `KindSendLoginNoFormData`.

Typical fields seen in production:

```
SessionKey       (server-generated inner key)
AuthKey          (server-generated)
ServiceCode      (empty)
ServiceRegion    (empty)
ServiceAccountSN  "0"
```

These are forwarded verbatim in step 6's POST.

## Step 6 — `return.aspx` (first POST)

```
POST https://tw.beanfun.com/beanfun_block/bflogin/return.aspx
Content-Type: application/x-www-form-urlencoded
Referer: https://login.beanfun.com/
User-Agent: …
Body: <step-5 scraped fields, x-www-form-urlencoded>
```

The response is discarded — we only need the server to accept the
form and advance session state. Any `bfWebToken` cookie set by this
response is not consumed; the canonical token comes from step 7.

Pungin keeps a separate no-redirect client just for this step (so
they can read `Set-Cookie` directly from a 302). Our QR-only flow
doesn't need that — we don't read the cookie here, and step 7's jar
read captures it regardless. We use the default redirect-following
client.

4xx/5xx → `KindHTTP`.

## Step 7 — `return.aspx` (redirect-following, AuthKey=OK)

```
POST https://tw.beanfun.com/beanfun_block/bflogin/return.aspx
Content-Type: application/x-www-form-urlencoded
Referer: https://login.beanfun.com/
User-Agent: …
Body (5 fields):
  SessionKey       = <outer skey from step 0>
  AuthKey          = OK
  ServiceCode      = (empty)
  ServiceRegion    = (empty)
  ServiceAccountSN = 0
Client: BeanfunClient.http  (follows redirects)
```

Notes:

- `SessionKey` here is the **outer** session key from step 0, NOT the
  inner `SessionKey` scraped in step 5.
- `AuthKey=OK` is the literal string `"OK"` — the QR-specific
  approval sentinel.
- `ServiceCode` and `ServiceRegion` empties are intentional.

After the redirect chain settles, read `bfWebToken` from the **shared
cookie jar** — `c.jar.Cookies(portalBase)`. Do **not** read from
`resp.Cookies()` only: the cookie can be set on a later redirect hop
rather than the immediate 302, and only the jar sees all of them.

Missing `bfWebToken` from the jar → `KindMissingWebToken` (fatal).

## Step 8 — Post-login: `GetAccounts`

Once login completes the `bfWebToken` cookie is the bearer for every
subsequent portal call. The first one we need is the user's list of
game accounts under their Beanfun ID — rendered on the post-login
home screen.

Two sequential GETs:

### Call 1 — `auth.aspx` (cookie refresh)

```
GET https://tw.beanfun.com/beanfun_block/auth.aspx
  ?channel=game_zone
  &page_and_query=game_start.aspx?service_code_and_region={ServiceCode}_{ServiceRegion}
  &web_token={bfWebToken}
```

The inner `game_start.aspx?...` value gets URL-encoded by `net/url`
when the query is serialised — that's correct, the server expects the
percent-encoded form.

Response body is discarded. Non-2xx is fatal (`KindHTTP`). The call's
side effect on the server is to rebind `bfWebToken` to a game-zone
session — without it the next call may 302 to a login interstitial.

### Call 2 — `game_server_account_list.aspx`

```
GET https://tw.beanfun.com/beanfun_block/game_zone/game_server_account_list.aspx
  ?sc={ServiceCode}
  &sr={ServiceRegion}
  &dt={time.Now().UTC().Format("20060102150405")}
```

`dt` is a 14-character UTC timestamp — purely a cache-buster. Server
doesn't validate the value, but the parameter is required.

Response is HTML. Each account row matches:

```
<a onclick="(handler)"><div id="(sid)" sn="(ssn)" name="(sname)">
```

Implemented as `accountRowRE` in `internal/beanfun/parser.go`. Rules:

- **`onclick` empty** → row is server-disabled (frozen / suspended);
  keep it in the result with `Enabled=false` so the UI can render
  greyed-out instead of dropping silently.
- **`sname`** is HTML-entity decoded (`html.UnescapeString`). Real
  responses use named entities (`&amp;`, `&lt;`) for special chars in
  display names, and numeric entities (`&#23567;`) for Chinese.
- Result slice is sorted ascending by `ssn`. Since `ssn` is a
  fixed-width digit string, lexicographic order equals numeric order.

Empty list is a valid response: user has zero game accounts under this
service code. Frontend surfaces it as an empty state, not an error.

### Deferred

Pungin also issues a per-row `game_start_step2.aspx` GET to fetch
`screatetime` (account creation timestamp) for each account. It is
concurrent, has a 5s per-row timeout, and tolerates partial failure.
We skip it for now — the timestamp is cosmetic UI ("created on
2018-05-13") and adds real complexity for marginal value. Revisit if
the UX needs it.

Pungin's `AmountLimitNotice` (quota / re-auth banner) is also deferred —
low-frequency edge case.

## Cookie + client design

`BeanfunClient` holds one redirect-following `http.Client` plus a
`cookiejar.Jar`. All seven calls go through the same client; cookies
set by any response (including via redirects) are visible to all
subsequent calls.

`LoginService.StartQRLogin` mints a **fresh** `BeanfunClient` (and a
fresh jar) on every call. Re-using a jar across login attempts caused
the portal to route subsequent requests differently and sometimes
land on a final URL with no session key at all; fresh jar per attempt
is the simplest fix.

## Token storage

`bfWebToken` and the outer session key are session bearers. They are
stashed in `LoginService.session *Session` in process memory only:

- Never persisted (no disk write, no OS keyring — at least not yet)
- Never logged literally — `Session.String()` redacts both fields
  as `***`
- Never crossed back to the frontend over the Wails IPC boundary

## Service code / region defaults

For TW MapleStory:

- `ServiceCode = "610074"`
- `ServiceRegion = "T9"`

Static for the current scope. Broader launcher support would
generalise them.

## Quirks summary

A condensed list of things that look weird but are deliberate.

| Quirk | Where | Why |
|---|---|---|
| `Content-Length: 0` on POST CheckLoginStatus | Step 3 | Server rejects chunked encoding with 411 |
| Permissive `[sp][Ss]?[Kk]ey=` regex | Step 0 | Portal toggles between `pSKey=` and `skey=` over time |
| Empty `__RequestVerificationToken` tolerated | Step 1 | Production server allows it; we don't bail when scrape misses |
| QR-specific Accept on SendLogin | Step 5 | Differs from non-QR flow; mirrors the upstream library byte-for-byte |
| Read cookie from jar, not response | Step 7 | `bfWebToken` may be `Set-Cookie`'d on a later redirect hop |
| Fresh client per `StartQRLogin` | Service layer | Stale cookies in a re-used jar caused observably different redirects |
| `AuthKey=OK` literal | Step 7 | QR-only approval sentinel |
| `ServiceAccountSN=0` literal | Step 7 | Required by server even though it's not an account-specific value |
| Pinned `CV` / `Hash` of a DLL we don't ship | Step 9.3 | The v2 OTP endpoint gates on a claim that the caller is Gamania's own launcher; the values are properties of a GGM release, not of the session |
| Hand-rolled JSON body | Step 9.3 | Keeps the launch ticket in `[]byte` so it can be zeroed; marshalling from a struct would copy it into an immutable string |
| Decoder validates its own output | Step 9.2 | The blob's step ordering has no first-party source; a wrong reading must fail at the decode, not at the server |

## Step 9 — Post-login: launch OTP (2 calls + a local decode)

Game launch needs a single-use OTP that the game.exe accepts on its
command line. Gamania replaced this endpoint on 2026-08-17 (see
[History](#history--the-retired-6-step-otp-flow) below); the current
flow is two HTTP calls plus a local decode:

```
Step 9.1  game_start_step2.aspx   GET   tw.beanfun.com   ← scrape m_objData
Step 9.2  decode the handoff blob (local)                ← yields LaunchTicket
Step 9.3  get_webstart_otp_v2.ashx  POST tw.beanfun.com  ← JSON in, JSON out
Step 9.4  DES-ECB decrypt (local)
```

Both HTTP calls share the cookie jar that `bfWebToken` lives in.

### Step 9.1 — `game_start_step2.aspx`

```
GET https://tw.beanfun.com/beanfun_block/game_zone/game_start_step2.aspx
  ?service_code={ServiceCode}
  &service_region={ServiceRegion}
  &sotp={SSN}
  &dt={UTC YYYYMMDDHHMMSS}
```

Two things are read from the HTML:

- The `尚未登入` session-expiry notice → `KindSessionExpired`, checked
  before anything else.
- `var m_objData = {"region":…,"sn":…,"data":…}` — the literal the page
  hands to Gamania's native launcher. Parsed as JSON, not scraped field
  by field. Missing or unparseable → `KindOTPInit`.

Observed field shapes (capture, 2026-08-18): `region` is the literal
`TW;Production`; `sn` is a 36-char GUID and is byte-identical to the
long-polling key the retired flow scraped separately; `data` was 553
characters.

**No response-body preview may be attached to errors from this page.**
`data` decodes to a live `LaunchTicket` using only the substitution
tables in `internal/beanfun/launch_data.go` and a DES key embedded in
the blob itself — a leaked blob is a leaked credential. Errors here
report `len(body)` only, and a regression test plants a marker in a
fake body and asserts it never reaches the error string.

### Step 9.2 — decode the handoff blob (local)

`data` is an obfuscated, DES-encrypted `key=value` bundle. The decode,
in order:

1. First character is a hex digit — the *selector*. Both captures so far
   used a different value (8, then 0).
2. Substitution table = `launchDataTables[selector % 4]`. Each table is
   a permutation of the 16 hex digits; a unit test asserts that
   invariant so a typo cannot silently break decoding.
3. Normalise **the whole remainder** by replacing each character with
   its index in the table, emitted as a hex digit.
4. The 8 characters at offset `selector + 1` **of the normalised text**
   are the DES key, taken as ASCII. Because they come out of the
   normalised text, the key is always 8 hex characters.
5. The rest of the normalised text is the ciphertext hex.
6. DES-ECB, no padding, NUL-trimmed.
7. Plaintext is `k=v` pairs joined by `&`, with a trailing `;`-prefixed
   tail that is discarded. Fields observed: `LaunchTicket`,
   `ServiceCode`, `ServiceRegion`, `ServiceAccount`, `BeanfunUrl`,
   `WebStartPatch`.
8. **Validate**: `LaunchTicket` must be exactly 64 hex characters, else
   `KindLaunchDataDecode`.

Step 8 is load-bearing. The ordering in steps 3–4 was not documented
anywhere first-party — the alternative reading (lift the key out of the
*raw* blob, then normalise) satisfies the same length arithmetic and was
tried first. Validating the output is what turned a wrong guess into an
immediate, named failure instead of a rejected request. Arithmetic check
for a 553-char blob: `553 - 1 - 8 = 544` hex characters = 272 bytes = 34
whole DES blocks.

The decoded ticket is a live credential: it is held as `[]byte`, copied
rather than aliased out of the plaintext buffer, and the buffer, the DES
key, and the request body are all zeroed after use.

### Step 9.3 — `get_webstart_otp_v2.ashx`

```
POST https://tw.beanfun.com/beanfun_block/generic_handlers/get_webstart_otp_v2.ashx
Content-Type: application/json

{"SN":"<36-char GUID>","LaunchTicket":"<64 hex>","CV":"1.5.0.2","Hash":"<64 hex>","arch":"x64"}
```

- `SN` comes from `m_objData.sn`.
- `CV` and `Hash` are a **client-integrity claim**: the assembly version
  and SHA-256 of Gamania's `GGMWebStart.dll`, pinned in
  `internal/beanfun/client_integrity.go`. They go stale whenever
  Gamania ships a new helper, and the symptom is every OTP fetch being
  rejected at once.
- `arch` mirrors the helper's `Environment.Is64BitProcess` — the
  calling process's bitness, not the OS's.
- The body is assembled by hand rather than marshalled from a struct so
  the ticket never becomes an unzeroable Go string.

Response:

```json
{ "result": 1, "data": "<40 chars>", "message": null }
```

`result != 1` → `KindOTPServerRejected` carrying the server's own
`message`. Two rejections seen in practice: `Query String Error` (sent
to the retired endpoint) and `Invalid_Start_Ticket` (ticket stale, or
posted without the session that minted it).

### Step 9.4 — DES-ECB decrypt (local)

`data` is `{8-char-key}{hex-cipher}` — the same construction the retired
endpoint wrapped in a `<status>;` prefix, minus the prefix, because the
status now arrives as the JSON `result`.

- Key: first 8 ASCII bytes, used directly as a DES key (DES is 56-bit;
  the eighth bit of each key byte is parity that the Go API ignores).
  Zeroed after use.
- Cipher: remainder, hex-decoded, length a multiple of 8 bytes.
- Decrypt each block with `crypto/des.Block.Decrypt` (ECB; no IV).
- NUL-trim both ends. Observed: a 40-character `data` yields a 10-char
  OTP, matching the length the retired flow produced.

We keep the decrypted token as `[]byte` (not `string`) so the launcher
can `Zero` it after the game.exe spawn consumes the command-line copy.
Per [[security-principles-beanfun]] this matters even though the OTP is
single-use server-side.

### History — the retired 6-step OTP flow

Until 2026-08-17 the OTP came from a GET to
`generic_handlers/get_webstart_otp.ashx` with nine query-string
parameters, preceded by three priming calls — `get_cookies.ashx` (for a
`SecretCode`), `record_service_start.ashx`, and a `get_result.ashx`
long-poll — and by scraping three inline-JS literals out of
`game_start_step2.aspx` (the long-polling key, a TW-only `unk_data`
pair, and `ServiceAccountCreateTime`).

One of those nine parameters was `ppppp`, a hardcoded 64-char uppercase
hex constant of unknown provenance.

From 2026-08-17 that endpoint answers `0;` + eight spaces +
`Query String Error` to a well-formed request. The three priming calls
and all four scrapers were removed along with it: none of their outputs
appear in the v2 contract, and dropping them took the OTP fetch from
four requests against `tw.beanfun.com` down to one, which also reduces
exposure to the portal's IP frequency lock (see issue #83). A test
asserts the whole fetch issues exactly two HTTP requests, so a revived
step fails immediately.

Diagnosis trail for this change is in `docs/plans/84-otp-webstart-v2/`.

## Command-line spawn

`MapleStory.exe` is launched (via ShellExecuteW for manifest-aware
UAC) with five **positional** arguments — no slash-prefixed flags:

```
MapleStory.exe <host> <port> BeanFun <SID> <OTP>
```

| Position | Value | Notes |
|----------|-------|-------|
| 1 | `tw.login.maplestory.beanfun.com` | TW login-server hostname (resolves to the `202.80.104.24-29` IPs that `internal/maple/status` TCP-probes) |
| 2 | `8484` | TCP port — game.exe connects directly |
| 3 | `BeanFun` | Channel marker; replaces the older `/hb` flag |
| 4 | `<SID>` | Service-account ID (NOT the SSN — see `Account.SID`) |
| 5 | `<OTP>` | The decrypted token from step 9.4 |

On a fresh spawn the game contacts `<host>:<port>`, validates the OTP
server-side, and proceeds **straight to character select** — no login
form is rendered, no WM_CHAR injection needed.

After `ShellExecuteW` returns, the launcher calls `beanfun.Zero(token)`
on the OTP byte slice. The Go-heap copy lives until GC; the OS-side
argv lives in the game process's memory for its lifetime, which is
unavoidable and bounded by the OTP's server-side ticket lifetime
(single use).

### History — earlier `/hb /u: /p:` format

Through alpha.31 the launcher passed `/hb /u:<SID> /p:<OTP>` flags
based on docs scraped from `pungin/Beanfun`. M8 attempted to use this
format, found that the current TW build no longer honored it
(verified during alpha.6), and pivoted to WM_CHAR injection. M10
piled an automated form-ready detector on top of that detour — which
collapsed in alpha.31 testing once we realized the form's actual
render produces zero Win32 events (only the painted pixels move,
inside the game's own DirectX surface).

The current 5-arg positional format was discovered in M10.1 by
inspecting the official launcher's running `MapleStory.exe`:

```
wmic process where name="MapleStory.exe" get commandline /value
```

This technique generalizes: anytime Gamania rotates the protocol, the
official client itself is the ground-truth reference. Repeat the wmic
probe before assuming any encoded constant is stable.

### `Launch` retained as M8 fallback

The argv path requires a fresh process — credentials can't be
retroactively supplied to an already-running game. For users who
opened the game outside our launcher, M8's `Launch` (PostMessage
WM_CHAR injection into the running window) survives as the fallback,
routed via the FE's `useGameStateQuery`: button label flips to
「帶入帳密」 when `running=true`, calling `LauncherService.Launch`
instead of `SpawnGame`.

### Deferred: Locale_Remulator

Milestone 6 spawns the game directly via `CreateProcessW`. Users on
Chinese-Windows (the developer's environment) see the game in the
correct locale because their system codepage is already `950`
(zh-TW). Users on non-Chinese Windows would see garbled text — that
case is solved by Locale_Remulator (LR), which injects a DLL hook
that intercepts locale-related Win32 API calls and reports `zh-TW`.

Adding LR is its own scope:

- 5 LR artifacts to bundle (`LRConfig.xml`, `LRHookx32.dll`,
  `LRHookx64.dll`, `LRProc.exe`, `LRSubMenus.dll`).
- Choice between (a) spawning `LRProc.exe` with `ShellExecuteW("runas",
  …)` (per pungin — requires UAC every launch) or (b) doing the
  DLL injection ourselves via `CreateProcessW(CREATE_SUSPENDED)` +
  `VirtualAllocEx` + `WriteProcessMemory` + `CreateRemoteThread` +
  `LoadLibraryW` (no UAC; more Win32 surface area to own).
- 32-vs-64-bit DLL selection from `IsWow64Process2`.

Deferred to **Milestone 6.5**.

## References

The wire spec above was reverse-engineered with help from the
pungin/Beanfun (Tauri + Rust) launcher's source. For maintainers who
need to verify a specific behaviour against the upstream library,
clone <https://github.com/pungin/Beanfun> (default branch `code`) and
consult the corresponding files:

| Topic | Pungin file (path under `src-tauri/`) |
|---|---|
| HTTP client + cookie jar + dual-client setup | `src/services/beanfun/client.rs` (lines 228–271 for the dual-client setup) |
| getSessionKey (TW path + regex) | `src/services/beanfun/login/session_key.rs` (lines 33–60, regex at 117) |
| Login/Index + Login/InitLogin | `src/services/beanfun/login/qr_init.rs` (lines 129–225) |
| `__RequestVerificationToken` scrape | `src/services/beanfun/login/qr_init.rs:11-14` |
| Deeplink unwrap | `src/services/beanfun/login/qr_init.rs:227-280` |
| Hidden-input regex parser | `src/core/parser/form.rs:52-122` |
| Login/SendLogin call | `src/services/beanfun/login/send_login.rs` |
| QRLogin/CheckLoginStatus | `src/services/beanfun/login/qr_poll.rs:89-130` |
| Finalize orchestration (4 calls) | `src/services/beanfun/login/qr_finalize.rs:149-250` |
| return.aspx no-redirect POST | `src/services/beanfun/login/return_aspx.rs` |
| return.aspx AuthKey=OK + jar read | `src/services/beanfun/login/completed.rs` |
| Session struct + Debug redaction | `src/services/beanfun/session.rs` |
| GetAccounts orchestration | `src/commands/account.rs` (lines 243–272 for the Tauri command, 608–660 for the auth.aspx refresh, 667–685 for the list fetch) |
| Account-row regex | `src/core/parser/account.rs:69-91` |
| `screatetime` per-row fetch (we defer) | `src/commands/account.rs:695-721` |
| OTP 6-step orchestration (retired flow) | `src/services/beanfun/otp.rs:122-145` |
| OTP per-step impls (retired flow) | `src/services/beanfun/otp.rs:171-323` |
| OTP scrapers, long-polling key / unk_data / secret code (retired flow) | `src/services/beanfun/otp.rs:393-478` |
| WCDES decrypt (DES-ECB-NoPadding) | `src/core/wcdes/mod.rs:76-88` |
| `ppppp` 64-hex literal (retired flow) | `src/services/beanfun/otp.rs:489` |
| Game launcher + command-line template | `src/services/game/launcher.rs:286-375` |
| Test golden fixtures | `tests/{session_key,qr_init,qr_poll,qr_finalize,account,otp}.rs` |

Our test suite mirrors the cases in those files where they apply to
our scope (TW only; QR-only).

The v2 OTP flow in Step 9 is our own implementation — the upstream
launcher's v2 code was deliberately not ported. Its published protocol
notes were used as a starting point for the endpoint name and JSON
shape; the decoder ordering, the validation, and the credential
lifecycle were established here by capture and test.
