# Structure Outline

## Approach

Build the blob decoder first as a self-contained, offline-testable unit; then swap step 5 to
the v2 POST while the old priming steps are still in place, so a live failure is unambiguously
about v2; then remove the dead steps and scrapers; then close the credential-leak path; then
update the protocol doc.

**Note on slicing.** This change does not cross into the UI. `LauncherService.GetOTP` and
`LaunchAccount` keep their signatures, so `frontend/bindings/` is untouched and no frontend
work exists in any phase. The slices below run along the data path — page bytes in, OTP out —
with each phase independently verifiable. Phases 2 and 3 each end in a live run, which is the
only place the real contract can be checked.

---

## Phase 1: Decode the handoff blob

Turns the page's `m_objData` literal into a validated `LaunchTicket`. Delivers the whole
"bytes → ticket" capability with no network involved.

**Files**: `internal/beanfun/launch_data.go` (new), `internal/beanfun/launch_data_test.go` (new),
`internal/beanfun/parser.go`, `internal/beanfun/parser_test.go`, `internal/beanfun/wcdes.go`,
`internal/beanfun/errors.go`

**Key changes**:
- `type launchHandoff struct { Region, SN, Data string }` — new
- `extractLaunchHandoff(body string) (launchHandoff, bool)` — new, `parser.go`, parses the JSON
  object literal with `encoding/json` (no greedy regex)
- `type launchInfo struct { LaunchTicket, ServiceCode, ServiceRegion, ServiceAccount string }` — new
- `decodeLaunchData(data string) (launchInfo, error)` — new, `launch_data.go`; validates its own
  output per design decision 7
- `desECBDecryptHex(key []byte, cipherHex string) ([]byte, error)` — extracted from `decryptOTP`
- `KindLaunchDataDecode` — appended to the end of the `LoginErrorKind` enum (appending only;
  inserting renumbers the wire values)

**Verify**: `go test ./internal/beanfun/` passes with three new falsifiable checks — a
round-trip (build a synthetic blob with the inverse transform, decode it, assert the exact
field values), an absolute assertion that each of the four substitution tables is a permutation
of all 16 hex digits, and a negative case asserting a corrupted blob returns
`KindLaunchDataDecode` rather than garbage. `gofmt -l .` silent, `golangci-lint run` clean.
**Platform**: macOS-verifiable

---

## Phase 2: Swap step 5 to the v2 POST

Makes `FetchOTP` produce a real OTP again. Steps 2-4 stay in place so that a live failure here
can only be about the v2 call or the decoder, not about removed priming.

**Files**: `internal/beanfun/otp.go`, `internal/beanfun/client_integrity.go` (new),
`internal/beanfun/wcdes.go`, `internal/beanfun/otp_test.go`

**Key changes**:
- `otpStep1` also returns the scraped `launchHandoff`
- `otpFetchV2(ctx context.Context, sn, launchTicket string) ([]byte, error)` — new; POSTs JSON to
  `generic_handlers/get_webstart_otp_v2.ashx` and decrypts the reply's `data`
- `type otpV2Response struct { Result *int; Data *string; Message *string }` — new; pointer
  fields distinguish missing from zero, matching `initQRLoginResponse` (`qr_init.go:24-25`)
- `ggmCV`, `ggmDLLSHA256`, `ggmArch` — new pinned constants with a provenance comment
- `pppppLiteral` deleted; `decryptOTP`'s `<status>;` envelope parsing deleted, replaced by a
  `{8-char key}{hex}` payload decrypt

**Verify**: `go test ./internal/beanfun/` — recorder asserts the POST body has exactly the five
expected JSON keys with the pinned `CV`/`Hash` values and `Content-Type: application/json`, and
a response test asserts a known `data` decrypts to a known OTP. Then **live**: `task dev`, QR
login, 複製帳密 — the log must show `FetchOTP: token acquired`, and a `result != 1` reply must
surface as `KindOTPServerRejected` carrying the server's `message`.
**Platform**: macOS-verifiable (including the live run)

---

## Phase 3: Remove the dead steps and scrapers

Realises design decisions 2-4: the OTP fetch becomes one request to `tw.beanfun.com` plus the
v2 POST.

**Files**: `internal/beanfun/otp.go`, `internal/beanfun/parser.go`,
`internal/beanfun/parser_test.go`, `internal/beanfun/otp_test.go`

**Key changes**:
- `otpStep2`, `otpStep3`, `otpStep4` deleted
- `extractSecretCode`, `extractUnkData`, `extractCreateTimeFallback` deleted with their regexes
  (`parser.go:47`, `:50`, `:56`) and their tests removed, not left asserting nothing
- `otpStep1`'s `unkDataKey`, `unkDataValue`, `createTime` fields deleted
- `SN` sourced from `launchHandoff.SN`

**Verify**: `go test ./internal/beanfun/` passes; the recorder asserts the fetch issues
**exactly two** HTTP requests (step-1 page, v2 POST) — a count that fails if a step survives or
a new one creeps in; `grep -rn "otpStep2\|otpStep3\|otpStep4\|extractSecretCode\|extractUnkData\|extractCreateTimeFallback" internal/`
returns nothing. Then **live** again: 複製帳密 still reaches `FetchOTP: token acquired`. If it
now fails, steps 3-4 were load-bearing (design.md Open Risks) — restore them and stop.
**Platform**: macOS-verifiable (including the live run)

---

## Phase 4: Close the credential-leak path

Realises design decision 6: OTP-path errors stop embedding response bodies, now that the
step-1 page carries an encrypted `LaunchTicket`.

**Files**: `internal/beanfun/otp.go`, `internal/beanfun/errors.go`,
`internal/beanfun/otp_test.go`

**Key changes**:
- OTP-path error constructors report structural facts (which literal was missing, its length)
  instead of `withBody(...)` output
- `withBody` itself stays for non-OTP callers unless the plan finds it has none left

**Verify**: `go test ./internal/beanfun/` — a test plants a marker string inside the fake
step-1 page body, forces a scrape failure, and asserts the resulting error message contains
neither the marker nor any body excerpt. That check fails today, which is the point.
**Platform**: macOS-verifiable

---

## Phase 5: Update the protocol doc

**Files**: `docs/beanfun-login-protocol.md`

**Key changes**: § 9 rewritten for the v2 contract — the JSON POST, the handoff decode, the
attestation inputs and their staleness, and the removal of steps 2-4. The retired endpoint and
`ppppp` are described as historical, not current.

**Verify**: read-back — `grep -n "get_webstart_otp.ashx" docs/beanfun-login-protocol.md` appears
only in a historical note, and the documented step count matches the implemented one.
**Platform**: macOS-verifiable

---

## Testing Checkpoints

After each phase, the following should be true:

1. **Phase 1** — the decoder round-trips a synthetic blob and rejects a corrupted one; no
   network code has changed yet, so `FetchOTP` still fails exactly as it does today.
2. **Phase 2** — a real OTP is obtainable on the dev machine. This is the checkpoint that
   proves the contract understanding; everything after it is cleanup.
3. **Phase 3** — the OTP fetch issues exactly two HTTP requests and still works live. The three
   greedy captures are gone from the package.
4. **Phase 4** — a forced step-1 failure produces an error with no body excerpt in it.
5. **Phase 5** — the doc describes the implemented flow, not the retired one.

Phases 1-4 all touch credential handling or outbound network calls, so each runs the
`security-reviewer` agent before its commit, per `CLAUDE.md`. Every phase also gets a
fresh-context `verifier` pass against the checkpoint above rather than self-certification.

If a context reset happens mid-implementation, `plan.md`'s checkboxes are the source of truth
for what is done; this file is the source of truth for what "done" means.
