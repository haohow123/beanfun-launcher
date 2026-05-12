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
| Test golden fixtures | `tests/{session_key,qr_init,qr_poll,qr_finalize}.rs` |

Our test suite mirrors the cases in those files where they apply to
our scope (TW only; QR-only).
