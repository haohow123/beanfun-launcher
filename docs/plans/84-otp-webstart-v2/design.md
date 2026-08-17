# Design Discussion

## Current State

`FetchOTP` (`internal/beanfun/otp.go:41`) runs six steps to obtain a game credential. Step 5
GETs `get_webstart_otp.ashx` with nine query parameters assembled by `fmt.Sprintf`
(`otp.go:253-265`) and step 6 decrypts a `1;{8-char key}{hex}` envelope (`wcdes.go:18`).

Gamania retired that endpoint on 2026-08-17. It now answers `0;` + eight spaces +
`Query String Error` (28 bytes), which `decryptOTP` classifies as `KindOTPServerRejected`
(`wcdes.go:42-43`). The request itself is well formed — a temporary audit confirmed nine
parsed parameters, no characters needing encoding, and no `url.ParseQuery` error — and the
same log file shows the identical code succeeding on 2026-05-13 (`envelope_len=42`).

The replacement contract is a JSON POST to `get_webstart_otp_v2.ashx` carrying
`SN`, `LaunchTicket`, `CV`, `Hash`, `arch`, answering `{result, data, message}`. Of the nine
old parameters only `SN` survives. `LaunchTicket` is not a new request: it is encrypted
inside `var m_objData`'s `data` property on `game_start_step2.aspx` — the page step 1 already
downloads. A live capture confirmed that object carries `region` (`TW;Production`), a 36-char
`sn` byte-identical to the long-polling key, and a 553-char `data` whose length satisfies the
decoder arithmetic (selector 8 → 544 hex → 272 bytes → 34 whole DES blocks).

## Desired End State

`FetchOTP` fetches the step-1 page, decodes `LaunchTicket` out of `m_objData.data`, POSTs the
v2 payload, and decrypts the reply's `data` into the OTP.

Verified by:
- `go test ./internal/beanfun/` — decoder round-trip, `m_objData` scrape, v2 request body
  fields, v2 response decryption
- Live on the macOS dev build: QR login → 複製帳密 → `FetchOTP: token acquired` in the log
- `gofmt -l .` silent, `golangci-lint run` clean, `go build ./...` green

## Patterns to Follow

- **Flat package layout** — 12 source files, no subdirectories (`internal/beanfun/`). The new
  decoder is another file in that directory.
- **Typed error kinds** — `LoginErrorKind` iota enum (`errors.go:11-27`), one constructor per
  kind (`errors.go:105`). New kinds append to the end; inserting renumbers the wire values.
- **Recorder-based HTTP tests** — `httptest` server plus a recorder struct capturing the
  outgoing request (`otp_test.go:126`, `:152`) and asserting on it (`otp_test.go:198-208`).
- **Pinned constant with provenance comment** — `pppppLiteral` (`otp.go:13-18`) is the shape
  to imitate for the attestation values, including naming what is unknown.
- **`Zero` for credential byte slices** (`zero.go`), as the OTP token already does.

Patterns the research found that this change must **not** follow:

- **Unencoded URL assembly** (`otp.go:253-265`) — the v2 body is JSON via `encoding/json`, so
  this disappears rather than being reproduced.
- **Greedy `(.*)` captures** (`parser.go:47`, `:50`, `:56`) — the new scrape targets a JSON
  object literal and parses it with `encoding/json`, not a greedy regex.
- **Response bodies embedded in errors** (`withBody`, `errors.go:149-154`, used at
  `otp.go:108`) — see decision 6.
- **URL-safe-only test fixtures** (`"TKN"`, `"z"`) — new tests use values containing
  characters that would break a naive encoder.

## Design Decisions

1. **v2 only; delete the legacy path.** The repo is TW-only and the old endpoint is gone
   server-side, so a legacy branch would be permanently unreachable and untestable. Removes
   `pppppLiteral` (`otp.go:18`) and the `<status>;` envelope parsing in `decryptOTP`
   (`wcdes.go:22-43`). Recoverable from git history if Gamania rolls back.

2. **Drop steps 2, 3 and 4.** `SecretCode` is no longer a v2 parameter, so step 2 is dead by
   contract. Steps 3 and 4 are described as priming server state (`otp.go:160-162`) — a
   comment, not a verified fact. Dropping them takes the OTP fetch from four requests to
   `tw.beanfun.com` down to one, which also reduces exposure to the frequency lock in #83.
   The phase ordering that de-risks this (make v2 work first, then remove) is a
   `4_structure` decision, not a design one; the end state is one step plus the POST.

3. **Source `SN` from `m_objData.sn`.** It is the value the page itself hands to the launcher
   for this purpose, and it is the same object as `data`. This retires the third greedy
   capture. Caveat: `sn == long-polling key` was verified on one capture — see Open Risks.

4. **Consequently, three scrapers and their `otpStep1` fields are deleted**:
   `extractSecretCode`, `extractUnkData`, `extractCreateTimeFallback`, and the
   `unkDataKey`/`unkDataValue`/`createTime` fields. Step 1's job becomes "fetch the page,
   extract one JSON literal".

5. **Pinned attestation constants.** `CV = 1.5.0.2` and the DLL SHA-256 ship as constants in
   their own file, with a comment stating plainly what they are (a claim to be the official
   `GGMWebStart.dll`), where they came from, and that they go stale when Gamania ships a new
   helper. Reading a locally installed helper to make the claim truthful is the better answer
   philosophically but needs Windows-only code that cannot be verified on the dev machine; it
   is recorded in #84 as a follow-up.

6. **Fix the `withBody` credential leak in this change.** `m_objData.data` is an encrypted
   `LaunchTicket`, and `otp.go:108`-style errors embed 500 bytes of that page
   (`bodyPreviewLimit`, `errors.go:138`) into a message that reaches both the log file and the
   UI. This change is what turns that page into a credential carrier, so it fixes the leak in
   the same PR: OTP-path errors report structural facts (which literal was missing, its
   length) instead of body text.

7. **Self-validating decoder.** The community write-up is ambiguous about whether the
   substitution mapping is applied before or after the embedded DES key is lifted out; both
   readings satisfy the length arithmetic. The decoder therefore validates its own output —
   the plaintext must parse into `key=value` pairs including a 64-hex `LaunchTicket` — so a
   wrong ordering fails loudly at decode time instead of sending garbage to the server. One
   live run then settles it.

8. **Extract the DES core.** `decryptOTP` currently fuses envelope parsing and decryption
   (`wcdes.go:18-66`). Split out a DES-ECB-no-padding helper so both the launch blob and the
   v2 reply use one audited implementation.

9. **Decoder lives in `internal/beanfun/launch_data.go`**, flat with the rest of the package,
   matching decision Patterns. No new exported surface.

10. **No new dependencies.** `crypto/des`, `encoding/hex`, `encoding/json` are all stdlib.

## Security Compliance

- **Hosts.** One new call, `POST tw.beanfun.com/beanfun_block/generic_handlers/get_webstart_otp_v2.ashx`
  — the same Gamania-owned host the flow already used (`client.go:27`). No new host, no
  telemetry, nothing outside Gamania.
- **Secrets in flight.** Two credential-bearing values are new: the encrypted `data` blob and
  the decoded `LaunchTicket`. Neither may appear in a log line, an error string, or a test
  fixture. Logging stays at the existing shape — lengths and status codes only
  (`otp.go:118-120` is the precedent). Decision 6 closes the one path that would have leaked
  the blob.
- **Zeroing.** The OTP stays `[]byte` and keeps its existing zero-after-use contract
  (`zero.go`; callers in `internal/launcher/service.go`). `LaunchTicket` follows the same
  contract: `launchInfo.LaunchTicket` is `[]byte`, the decoder zeroes the DES key and the
  decrypted plaintext on the way out, and the ticket is copied rather than aliased so the
  caller receives something it can zero itself. A round-trip test guards the copy — with an
  alias the ticket arrives as 64 NUL bytes, verified by temporarily introducing the alias and
  watching the test fail. The residual exposure is the single `string` conversion at JSON
  marshal time, which cannot be avoided without hand-rolling the encoder.

  This revises an earlier draft of this document, which accepted a `string` for the whole
  lifecycle as a "known limitation". The security review pointed out that `zero.go:8-10`
  already prescribes the `[]byte` lifecycle and that the sibling OTP path already follows it,
  so the limitation was self-inflicted rather than inherent.
- **Storage.** Nothing new is persisted. No keyring change.
- **Dependencies.** None added; see decision 10.
- **Attestation.** Decision 5 ships a claim to be the official launcher. That is a deliberate,
  owner-approved trade-off recorded in #84, not an oversight.
- Per `CLAUDE.md`, the implementation phases that touch this run the `security-reviewer` agent.

## What We're NOT Doing

- Route B (delegate to the official helper, intercept the game's argv). Recorded in #84 as the
  fallback if the attestation gate tightens.
- Reading a locally installed `GGMWebStart.dll` to compute `CV`/`Hash` at runtime.
- Automating `CheckVersion.ashx` polling to detect helper updates.
- Fixing the IP frequency lock (#83) — separate issue, though decision 2 incidentally reduces
  the request volume that triggers it.
- HK support or any non-TW region.
- Rewriting the remaining greedy captures beyond the three that decisions 3 and 4 delete.
- Touching the keep-alive heartbeat's uncapped 10-second retry (`service.go:61-62`).

## Open Risks

- **The v2 reply envelope is assumed.** Its documented 40-char `data` is consistent with an
  8-char key plus two DES blocks, but no plaintext OTP has been observed. If wrong, the
  symptom is a decrypt failure or mojibake, not a silent wrong answer.
- **`sn` vs long-polling key equality rests on one capture.** If v2 rejects the request, the
  first thing to test is sourcing `SN` from the old long-polling key instead.
- **Decoder ordering ambiguity** — mitigated by decision 7, but it may still cost a live
  round trip to resolve.
- **The four substitution tables are unverified against the installed helper.** A mismatch
  surfaces as a decode failure, which decision 7 makes loud.
- **Attestation values go stale** when Gamania ships a new helper. Symptom will be a v2
  rejection for every user at once.
- **Steps 3 and 4 might have been load-bearing.** If v2 rejects after they are removed,
  restoring them is the first hypothesis to test.
