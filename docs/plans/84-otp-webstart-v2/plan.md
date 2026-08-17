# Implementation Plan

## Overview

`FetchOTP` fetches `game_start_step2.aspx`, decodes `LaunchTicket` out of that page's
`m_objData.data`, POSTs the v2 payload to `get_webstart_otp_v2.ashx`, and decrypts the reply's
`data` into the OTP. Steps 2-4 and the retired GET are removed.

## Prerequisite for a fresh worktree

`main.go:20` embeds `all:frontend/dist`, and `frontend/dist` is gitignored — so in a fresh
worktree `go build ./...` fails with `pattern all:frontend/dist: no matching files found`
before any code is touched. Run once after `npm ci`:

```
cd frontend && npm run build
```

`go test ./internal/...` does not need this (it never builds the root package), but every
`go build ./...` checkbox below does. A failure with that message is a missing prerequisite,
not a regression.

## Existing tests this plan will break

All in `internal/beanfun/otp_test.go`. Listed here so implementation expects the red, not
discovers it:

| Test | Line | Phase | Fate |
|---|---|---|---|
| `TestDecryptOTP` | 52 | 2 | rewritten — `<status>;` envelope parsing is gone |
| `happyOTPMux` | 129 | 2 | gains a v2 POST route; loses steps 2-4 routes in phase 3 |
| `TestBeanfunClient_FetchOTP_HappyPath` | 159 | 2 | step-5 query assertions (`:198-208`) become v2 body assertions |
| `TestBeanfunClient_FetchOTP_Step1MissingLongPollingKey` | 212 | 3 | becomes "missing `m_objData`" |
| `TestBeanfunClient_FetchOTP_Step5ServerRejects` | 269 | 2 | becomes v2 `result != 1` |

Unaffected: `Step1SessionExpired:239`, `NilSession:302`, `TestZero:317`.
`parser_test.go` has no tests for the three scrapers phase 3 deletes — they are only covered
indirectly through `otp_test.go`.

---

## Phase 1: Decode the handoff blob

### Changes

#### 1. Extract the DES core

**File**: `internal/beanfun/wcdes.go`
**Action**: modify

Split the raw decryption out of `decryptOTP` (currently `wcdes.go:18-66`) so the launch blob
and the v2 reply share one implementation. `decryptOTP` keeps its current behaviour in this
phase; phase 2 replaces its envelope half.

```go
// desECBDecryptHex decrypts hex-encoded ciphertext with an 8-byte
// ASCII key using DES-ECB and no padding, trimming NUL padding.
func desECBDecryptHex(key []byte, cipherHex string) ([]byte, error) {
	cipherBytes, err := hex.DecodeString(cipherHex)
	if err != nil {
		return nil, ErrOTPDecrypt("hex decode: " + err.Error())
	}
	if len(cipherBytes)%des.BlockSize != 0 {
		return nil, ErrOTPDecrypt(fmt.Sprintf("cipher not block-aligned (%d bytes)", len(cipherBytes)))
	}
	block, err := des.NewCipher(key)
	if err != nil {
		return nil, ErrOTPDecrypt("des.NewCipher: " + err.Error())
	}
	plain := make([]byte, len(cipherBytes))
	for i := 0; i < len(cipherBytes); i += des.BlockSize {
		block.Decrypt(plain[i:i+des.BlockSize], cipherBytes[i:i+des.BlockSize])
	}
	return bytes.Trim(plain, "\x00"), nil
}
```

#### 2. New error kind

**File**: `internal/beanfun/errors.go`
**Action**: modify

Append to the **end** of the `LoginErrorKind` block (`errors.go:14-27`) — inserting renumbers
the values the frontend receives.

```go
	KindLaunchDataDecode
```

```go
func ErrLaunchDataDecode(reason string) *LoginError {
	return &LoginError{Kind: KindLaunchDataDecode, Msg: "launch data decode failed: " + reason}
}
```

`reason` must never carry blob content — only structural facts (lengths, which field was
missing).

#### 3. Scrape the handoff literal

**File**: `internal/beanfun/parser.go`
**Action**: modify

`m_objData` is a JSON object literal, so match the block and hand it to `encoding/json` rather
than adding a fourth greedy capture.

```go
type launchHandoff struct {
	Region string `json:"region"`
	SN     string `json:"sn"`
	Data   string `json:"data"`
}

// extractLaunchHandoff pulls the m_objData literal the page hands to
// the native launcher.
func extractLaunchHandoff(htmlBody string) (launchHandoff, bool) {
	m := launchHandoffRE().FindStringSubmatch(htmlBody)
	if len(m) < 2 {
		return launchHandoff{}, false
	}
	var h launchHandoff
	if err := json.Unmarshal([]byte(m[1]), &h); err != nil {
		return launchHandoff{}, false
	}
	if h.SN == "" || h.Data == "" {
		return launchHandoff{}, false
	}
	return h, true
}
```

Regex added to the `var` block alongside the existing ones: `var\s+m_objData\s*=\s*(\{[^}]*\})`
— bounded by `[^}]*` rather than `.*` so it cannot run past the literal.

#### 4. The decoder

**File**: `internal/beanfun/launch_data.go`
**Action**: create

```go
// launchDataTables are the four hex-digit permutations the handoff
// blob's selector chooses between.
var launchDataTables = [4]string{
	"bac987d65e432f10",
	"3bc4d5e6f2a79108",
	"cdbeaf9012456378",
	"4e6fb81a3c5d7092",
}

// launchInfo is the subset of the decoded handoff we consume.
type launchInfo struct {
	LaunchTicket   string
	ServiceCode    string
	ServiceRegion  string
	ServiceAccount string
}

func decodeLaunchData(data string) (launchInfo, error)
```

Steps, in order:

1. `len(data) < 10` → `ErrLaunchDataDecode("data too short (%d)")`.
2. `n`, err := `strconv.ParseUint(data[:1], 16, 8)`; non-hex → error.
3. `table := launchDataTables[n%4]`.
4. `key := []byte(data[n+1 : n+9])` — raw ASCII, taken from the original string. Bounds-check
   `n+9 <= len(data)` first.
5. `cipherChars := data[1:n+1] + data[n+9:]` — everything except the selector and the key.
6. Normalise: for each rune of `cipherChars`, its index within `table` emitted as a lowercase
   hex digit. A rune absent from `table` → `ErrLaunchDataDecode("byte outside table at N")`.
7. `plain, err := desECBDecryptHex(key, normalised)`.
8. Parse: take everything before the first `;`, split on `&`, split each on the first `=`.
9. **Validate (design decision 7)**: `LaunchTicket` must be present and exactly 64 lowercase
   hex characters, else `ErrLaunchDataDecode("LaunchTicket missing or malformed")`. This is
   what makes a wrong step ordering fail loudly.

Sanity check against the live capture recorded in `research.md`: `len(data)=553`, first char
`8` → `n=8`, table 0, key `data[9:17]`, `cipherChars` = 8 + 536 = 544 hex = 272 bytes = 34
whole DES blocks.

**If validation fails on real data**, the fallback interpretation is to normalise the whole
post-selector string first and lift the key out of the normalised text at the same offsets.
Try that before assuming the tables are wrong.

#### 5. Tests

**File**: `internal/beanfun/launch_data_test.go`
**Action**: create

Table-driven. A helper builds a synthetic blob by inverting step 6 (each hex digit `d` of the
ciphertext maps to `table[d]`), so no real credential is needed as a fixture:

```go
func buildLaunchBlob(t *testing.T, selector int, key, plaintext string) string
```

Cases:
- **Round-trip**: encrypt a known plaintext (`LaunchTicket=<64 hex>&ServiceCode=610074&...`)
  under a known 8-char key, build the blob, decode it, assert each `launchInfo` field equals
  the literal expected value.
- **All four selectors**: the same round-trip with selectors 0-3 (and one selector > 3 to
  exercise `n%4`).
- **Absolute table assertion**: each of the four tables is exactly 16 characters and is a
  permutation of `0123456789abcdef`. Pins the constants against a typo.
- **Corrupted blob** → `KindLaunchDataDecode`, asserted via `errors.As`, and the error message
  contains none of the blob.
- **Malformed `LaunchTicket`** (63 chars, or non-hex) → `KindLaunchDataDecode`.

**File**: `internal/beanfun/parser_test.go`
**Action**: modify — add `extractLaunchHandoff` cases: a page containing the literal (assert
`region == "TW;Production"`, `sn` and `data` exact), a page without it (`ok == false`), and a
page whose literal is not valid JSON (`ok == false`).

### Verification

#### Automated
- [x] `gofmt -l .` prints nothing
- [x] `golangci-lint run` passes
- [x] `go build ./...` succeeds
- [x] `go test ./internal/beanfun/` passes
- [x] `go test ./internal/beanfun/ -run 'LaunchData|LaunchHandoff' -v` shows the new cases running

#### Manual
- [ ] `FetchOTP` still fails exactly as before this phase — no network path changed yet
- [x] `security-reviewer` agent over `launch_data.go`, `parser.go`, `errors.go`, `wcdes.go`
- [x] `verifier` agent against this phase's checkpoint in `structure.md`

---

## Phase 2: Swap step 5 to the v2 POST

### Changes

#### 1. Attestation constants

**File**: `internal/beanfun/client_integrity.go`
**Action**: create

```go
// The v2 OTP endpoint gates on a claim that the caller is Gamania's
// own GGMWebStart.dll: CV is that assembly's version and Hash is the
// SHA-256 of its bytes. Values below were read from a local install
// of GGM 1.5.0.2 (1,289,232 bytes) on 2026-08-18; they go stale when
// Gamania ships a new helper, and the symptom is every OTP fetch
// being rejected at once.
const (
	ggmCV        = "1.5.0.2"
	ggmDLLSHA256 = "dfd568a69d87abcd8f4a93d1a4481ebb57712d1d28ab0b6fc018fcf140101e06"
)

// ggmArch mirrors the helper's Environment.Is64BitProcess, which
// reports the calling process rather than the OS.
func ggmArch() string {
	if runtime.GOARCH == "386" {
		return "x86"
	}
	return "x64"
}
```

#### 2. The v2 call

**File**: `internal/beanfun/otp.go`
**Action**: modify

`otpStep1` returns the handoff alongside what it already returns:

```go
type otpStep1 struct {
	longPollingKey string
	unkDataKey     string
	unkDataValue   string
	createTime     string
	handoff        launchHandoff // new in this phase; the rest go in phase 3
}
```

Add after the existing scrapes in `otpStep1` (`otp.go:106-117`):

```go
	handoff, ok := extractLaunchHandoff(bodyStr)
	if !ok {
		return otpStep1{}, ErrOTPInit(fmt.Sprintf(
			"m_objData not found in game_start_step2.aspx (body %d bytes)", len(bodyStr)))
	}
```

Note the error carries a length, not `withBody(...)` — the page is now a credential carrier.

New request/response types and call:

```go
type otpV2Request struct {
	SN           string `json:"SN"`
	LaunchTicket string `json:"LaunchTicket"`
	CV           string `json:"CV"`
	Hash         string `json:"Hash"`
	Arch         string `json:"arch"`
}

type otpV2Response struct {
	Result  *int    `json:"result"`
	Data    *string `json:"data"`
	Message *string `json:"message"`
}

// otpFetchV2 POSTs the v2 payload and returns the still-encrypted
// data field.
func (c *BeanfunClient) otpFetchV2(ctx context.Context, sn, launchTicket string) (string, error)
```

Implementation notes:
- URL via `c.portalURL("beanfun_block/generic_handlers/get_webstart_otp_v2.ashx")`.
- Body via `json.Marshal`; header `Content-Type: application/json`; keep the shared
  `c.newRequest` so the pinned User-Agent applies (`client.go:102`).
- `c.boundedRead(resp)`, then `resp.StatusCode >= 400` → `ErrHTTP`, matching every other step.
- `env.Result == nil` → `ErrOTPServerRejected("missing result field")`.
- `*env.Result != 1` → `ErrOTPServerRejected` carrying `*env.Message` when non-nil, else
  `fmt.Sprintf("result = %d", *env.Result)`.
- `env.Data == nil || *env.Data == ""` → `ErrOTPServerRejected("missing data field")`.
- Log at INFO with `status` and `len(*env.Data)` only — never the value.

`FetchOTP` (`otp.go:41-70`) becomes: step 1 → `decodeLaunchData(step1.handoff.Data)` →
`otpFetchV2(ctx, step1.handoff.SN, info.LaunchTicket)` → decrypt. Steps 2-4 still run in this
phase, and their results are simply unused by the new step 5.

#### 3. Reply decryption

**File**: `internal/beanfun/wcdes.go`
**Action**: modify

Replace `decryptOTP`'s `<status>;` envelope parsing with a payload-only decoder — the status
now lives in the JSON `result`, so the old prefix cannot appear.

```go
// decryptOTPPayload unpacks the `{8-char key}{cipher hex}` payload
// returned in the v2 reply's data field.
func decryptOTPPayload(payload string) ([]byte, error) {
	if len(payload) < 8 {
		return nil, ErrOTPDecrypt(fmt.Sprintf("payload < 8 bytes (got %d)", len(payload)))
	}
	return desECBDecryptHex([]byte(payload[:8]), payload[8:])
}
```

Delete `decryptOTP` and the now-unused `indexByte` helper (`wcdes.go:68-79`).

#### 4. Delete the retired constant

**File**: `internal/beanfun/otp.go`
**Action**: modify — delete `pppppLiteral` (`otp.go:13-18`) and `otpStep5` entirely.

#### 5. Tests

**File**: `internal/beanfun/otp_test.go`
**Action**: modify

- `happyOTPMux` gains a `POST get_webstart_otp_v2.ashx` route that records the request body
  and answers `{"result":1,"data":"<8-char key><32 hex>","message":null}`. The recorder gains
  `v2Body []byte`.
- `TestBeanfunClient_FetchOTP_HappyPath`: replace the query-string assertions (`:198-208`) with
  body assertions — unmarshal `rec.v2Body` and assert **exactly five keys**, `SN` equals the
  page's `sn`, `CV == "1.5.0.2"`, `Hash` equals the pinned constant literal, `arch` non-empty,
  and `LaunchTicket` is 64 hex. Also assert the request's `Content-Type`.
- `TestBeanfunClient_FetchOTP_Step5ServerRejects`: v2 answers `{"result":0,"message":"Query String Error"}`
  → assert `KindOTPServerRejected` and that the message contains `Query String Error`.
- `TestDecryptOTP`: rewrite as `TestDecryptOTPPayload` — a literal key + hex pair decrypting to
  a literal expected OTP, plus a too-short payload case.
- Fixtures use a `WebToken` containing `&` and a space to prove nothing hand-builds a query
  string any more.

### Verification

#### Automated
- [ ] `gofmt -l .` prints nothing
- [ ] `golangci-lint run` passes
- [ ] `go build ./...` succeeds
- [ ] `go test ./internal/beanfun/` passes
- [ ] `grep -rn "pppppLiteral\|get_webstart_otp.ashx" internal/` returns nothing

#### Manual
- [ ] `task dev`, QR login, click 複製帳密 — `launcher-dev.log` shows
      `FetchOTP: token acquired` and the OTP reaches the clipboard
- [ ] the log contains no `LaunchTicket`, no `data` blob, and no OTP value
- [ ] `security-reviewer` agent over `otp.go`, `client_integrity.go`, `wcdes.go`
- [ ] `verifier` agent against this phase's checkpoint

---

## Phase 3: Remove the dead steps and scrapers

### Changes

#### 1. Delete the steps

**File**: `internal/beanfun/otp.go`
**Action**: modify

Delete `otpStep2` (`:131-158`), `otpStep3` (`:163-197`), `otpStep4` (`:200-227`) and their
calls in `FetchOTP`. Reduce `otpStep1` to:

```go
type otpStep1 struct {
	handoff launchHandoff
}
```

Delete the three scrape calls and their guards (`otp.go:106-117`) and the
`long_polling_key_len` / `create_time` log fields (`:118-120`), replacing the log with
`slog.Info("FetchOTP step 1: game_start_step2.aspx", "data_len", len(handoff.Data))`.

`SN` now comes from `step1.handoff.SN`.

#### 2. Delete the scrapers

**File**: `internal/beanfun/parser.go`
**Action**: modify — delete `extractLongPollingKey`, `extractUnkData`,
`extractCreateTimeFallback`, `extractSecretCode` and their regexes (`parser.go:46-57`).

Check with a grep before deleting each: `extractSecretCode` and `extractUnkData` have no
callers left once steps 2-3 are gone, and `extractCreateTimeFallback` was only used by steps
3 and 5.

#### 3. Tests

**File**: `internal/beanfun/otp_test.go`
**Action**: modify

- `happyOTPMux` drops the `get_cookies.ashx`, `record_service_start.ashx` and `get_result.ashx`
  routes and gains a **request counter**.
- `TestBeanfunClient_FetchOTP_HappyPath` asserts the counter is **exactly 2**.
- `TestBeanfunClient_FetchOTP_Step1MissingLongPollingKey` → renamed
  `..._Step1MissingHandoff`, page without `m_objData`, asserts `KindOTPInit`.

### Verification

#### Automated
- [ ] `gofmt -l .` prints nothing
- [ ] `golangci-lint run` passes
- [ ] `go build ./...` succeeds
- [ ] `go test ./internal/beanfun/` passes
- [ ] `grep -rn "otpStep2\|otpStep3\|otpStep4\|extractSecretCode\|extractUnkData\|extractCreateTimeFallback\|extractLongPollingKey" internal/` returns nothing
- [ ] `grep -c 'MustCompile' internal/beanfun/parser.go` is **exactly 3** — `sessionKeyRE`,
      `verificationTokenRE`, `launchHandoffRE`. It is 6 today, phase 1 adds one (7), and this
      phase deletes four. An absolute number, because a relative one passes whether or not the
      right regexes went.

#### Manual
- [ ] `task dev`, QR login, 複製帳密 — still `FetchOTP: token acquired`.
      **If this now fails, steps 3-4 were load-bearing** (design.md Open Risks): restore them,
      stop, and report rather than debugging forward.
- [ ] `security-reviewer` agent over `otp.go`, `parser.go`
- [ ] `verifier` agent against this phase's checkpoint

---

## Phase 4: Lock the credential-leak fix in place

Phases 1-3 remove every `withBody` call on the OTP path by construction. This phase adds the
regression test that keeps it that way, and removes anything the greps above missed.

### Changes

**File**: `internal/beanfun/otp_test.go`
**Action**: modify

```go
// A marker planted in the fake page must never reach the error text.
const leakMarker = "SENSITIVE-BLOB-MARKER"
```

Test: serve a `game_start_step2.aspx` body containing `leakMarker` but no `m_objData`, call
`FetchOTP`, and assert the returned error's `Error()` contains neither `leakMarker` nor the
literal `body=`. Repeat for a body that has `m_objData` with an undecodable `data`.

**File**: `internal/beanfun/otp.go`, `internal/beanfun/errors.go`
**Action**: modify — only if the grep below finds a remaining OTP-path caller. `withBody` and
`withBodyBytes` stay for `qr_init.go` and other non-credential pages.

### Verification

#### Automated
- [ ] `gofmt -l .` prints nothing
- [ ] `golangci-lint run` passes
- [ ] `go test ./internal/beanfun/` passes
- [ ] `grep -n "withBody" internal/beanfun/otp.go` returns nothing
- [ ] the new leak test fails when `withBody(bodyStr)` is temporarily reinstated — confirm the
      check can actually fail, then revert the experiment

#### Manual
- [ ] `security-reviewer` agent over the diff
- [ ] `verifier` agent against this phase's checkpoint

---

## Phase 5: Update the protocol doc

### Changes

**File**: `docs/beanfun-login-protocol.md`
**Action**: modify

- § 9's step list: six steps become two (page fetch, v2 POST) plus the local decode.
- Replace § 9.5's `get_webstart_otp.ashx` block (`:438-469`) with the v2 JSON contract.
- New subsection for the handoff decode: `m_objData` shape, selector/table/key arithmetic, and
  the `LaunchTicket` validation.
- New subsection for the attestation inputs: what `CV`/`Hash`/`arch` are, that they are pinned,
  and that they go stale on a helper release.
- Move the retired endpoint and `ppppp` into a clearly-labelled historical note, with the
  2026-08-17 date.
- Update the "Deliberate quirks" table: drop the `CreateTime` `%20` and `ppppp` rows, add the
  pinned-attestation row.

### Verification

#### Automated
- [ ] `grep -n "get_webstart_otp.ashx" docs/beanfun-login-protocol.md` — every hit is inside the
      historical note
- [ ] `grep -n "ppppp" docs/beanfun-login-protocol.md` — same
- [ ] the documented step count matches `FetchOTP`'s implementation

#### Manual
- [ ] `verifier` agent read-back: every section the plan says to write exists and is non-empty,
      and every endpoint path named in the doc exists in the code
