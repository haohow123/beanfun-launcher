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

## Step 9 — Post-login: launch OTP (6-step flow)

Game launch needs a single-use OTP that the game.exe accepts on its
command line. Beanfun's portal issues the OTP through a 6-step
choreography that primes server-side state across multiple endpoints
before the actual ticket can be read out. All calls share the same
cookie jar that `bfWebToken` lives in.

### Step 9.1 — `game_start_step2.aspx`

```
GET https://tw.beanfun.com/beanfun_block/game_zone/game_start_step2.aspx
  ?service_code={ServiceCode}
  &service_region={ServiceRegion}
  &sotp={SSN}
  &dt={UTC YYYYMMDDHHMMSS}
```

The response is HTML with three inline-JS literals we scrape:

- `GetResultByLongPolling&key=KEYVAL"` — the long-polling key,
  reused as `SN` on step 9.5 and as `key` on step 9.4.
- `MyAccountData.ServiceAccountCreateTime + "K=V";` (TW only) —
  per-account form field reposted verbatim on step 9.3. Both halves
  arrive URL-encoded; we percent-decode them before forwarding.
- `ServiceAccountCreateTime: "..."` — the account creation
  timestamp, forwarded on step 9.3 and step 9.5.

Missing any of these → `KindOTPInit` (server is in an unexpected
state; user should retry).

### Step 9.2 — `get_cookies.ashx`

```
GET https://tw.newlogin.beanfun.com/generic_handlers/get_cookies.ashx
```

Body has one inline JS literal: `var m_strSecretCode = 'CODE';`.
Scrape it; forward to step 9.5. Note this is the only call in the
whole flow that hits `tw.newlogin.beanfun.com` rather than
`tw.beanfun.com` — the `NewloginBase` endpoint exists for this single
purpose.

### Step 9.3 — `record_service_start.ashx`

```
POST https://tw.beanfun.com/beanfun_block/generic_handlers/record_service_start.ashx
Content-Type: application/x-www-form-urlencoded

service_code, service_region, service_account_id=<SID>, sotp=<SSN>,
service_account_display_name=<SName>, service_account_create_time=<createTime>,
<unkDataKey>=<unkDataValue>
```

Response body discarded. The call primes server-side state for step
9.5's OTP issuance — skipping it fails step 9.5 with a generic
rejection.

### Step 9.4 — `get_result.ashx` long-poll

```
GET https://tw.beanfun.com/generic_handlers/get_result.ashx
  ?meth=GetResultByLongPolling
  &key=<long-polling key from step 9.1>
  &_=<ISO timestamp cache buster>
```

Response body discarded. Like step 9.3, this is a side-effect call
that drives server-side OTP generation; skipping it makes step 9.5
return an empty / rejecting envelope.

### Step 9.5 — `get_webstart_otp.ashx`

```
GET https://tw.beanfun.com/beanfun_block/generic_handlers/get_webstart_otp.ashx
  ?SN=<long-polling key>
  &WebToken=<bfWebToken>
  &SecretCode=<from step 9.2>
  &ppppp=1F552AEAFF976018F942B13690C990F60ED01510DDF89165F1658CCE7BC21DBA
  &ServiceCode=<ServiceCode>
  &ServiceRegion=<ServiceRegion>
  &ServiceAccount=<SID>
  &CreateTime=<createTime with space %20-encoded>
  &d=<UnixMilli cache buster>
```

Two pieces of WPF-specific encoding the standard form-urlencoder
gets wrong:

1. **`CreateTime`** contains a literal space (`2024-01-15 12:34:56`)
   that WPF encodes as `%20`, not `+`. We replace the space manually
   before assembling the URL string.
2. **`ppppp`** is a 64-char uppercase hex literal the server
   validates as a protocol constant. It must appear verbatim — no
   encoding, no case folding. We don't know its provenance; the
   value comes from pungin's WPF analysis and works.

`SN` is the **long-polling key from step 9.1**, not the SSN. The
naming is confusing but matches the wire format.

Response body: `1;{8-char-key}{hex-cipher}` literal (or
`0;{reason}` / similar on rejection — anything where the first
segment isn't `1` triggers `KindOTPServerRejected`).

### Step 9.6 — DES-ECB decrypt (local)

The envelope is `<status>;<payload>` (split on the first `;`; ignore
later segments). If `<status> != "1"` → `KindOTPServerRejected` with
`<payload>` as the message.

For status=1, the payload is `{8-char-key}{hex-cipher}`:

- Key: first 8 ASCII bytes of payload, used directly as a DES key
  (DES is 56-bit; the eighth bit of each key byte is parity that the
  Go API ignores).
- Cipher: remainder of payload, hex-decoded. Length must be a
  multiple of 8 bytes (DES block size).
- Decrypt each block with `crypto/des.Block.Decrypt` (ECB; no IV
  needed).
- NUL-trim both ends of the assembled plaintext. The OTP is an 8-char
  ASCII string padded to a single block.

We keep the decrypted token as `[]byte` (not `string`) so the
launcher can `Zero` it after the game.exe spawn consumes the
command-line copy. Per [[security-principles-beanfun]] this matters
even though the OTP is single-use server-side.

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
| 5 | `<OTP>` | The 10-char decrypted token from step 9.5 |

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
| OTP 6-step orchestration | `src/services/beanfun/otp.rs:122-145` |
| OTP per-step impls | `src/services/beanfun/otp.rs:171-323` |
| OTP scrapers (long-polling key, unk_data, secret code) | `src/services/beanfun/otp.rs:393-478` |
| WCDES decrypt (DES-ECB-NoPadding) | `src/core/wcdes/mod.rs:76-88` |
| `ppppp` 64-hex literal | `src/services/beanfun/otp.rs:489` |
| Game launcher + command-line template | `src/services/game/launcher.rs:286-375` |
| Test golden fixtures | `tests/{session_key,qr_init,qr_poll,qr_finalize,account,otp}.rs` |

Our test suite mirrors the cases in those files where they apply to
our scope (TW only; QR-only).
