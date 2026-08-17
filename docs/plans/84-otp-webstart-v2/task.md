# Task

Gamania retired `get_webstart_otp.ashx` on 2026-08-17, so every OTP fetch now fails with
`OTP server rejected: Query String Error` — the launcher can log in and list accounts but
cannot produce a game credential. Migrate the OTP fetch to the replacement endpoint
`get_webstart_otp_v2.ashx`, whose `LaunchTicket` input is carried in an obfuscated blob on a
page the flow already downloads.

Issue: [#84](https://github.com/haohow123/beanfun-launcher/issues/84)
Symptom report: [#82](https://github.com/haohow123/beanfun-launcher/issues/82)

## Decisions already made by the repo owner

- **Route A** — decode the blob ourselves and POST the v2 payload directly. Route B
  (delegate to the official GGM helper and intercept the game's argv) is recorded in #84 as
  the fallback if the attestation gate tightens.
- **Own implementation** — the upstream community launcher's source is not ported. Its
  documented protocol facts are used as a reference; the code is ours.
