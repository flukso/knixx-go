# Commit the NCN5121 diagnostics work

> **Status: completed.** Implemented in commit `a421856` ("stat command and
> decoding of NCN5121 indications") (2026-10-14). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context

The intermittent `switch` failures were caused by a loose GND wire between the
FTDI
adapter and the NCN5121 HAT. The user tested the current code against the
target and
approved it ("LGTM"). What's left is to record the work, which is uncommitted
on `master`:

- `src/ncn/ncn.go`: `TxData` sends the whole request in one `Write`; adds
  `U_SystemStatReq`.
- `src/ncn/service.go` (new): `Parser` plus the `Describe`, `DecodeState`,
  `DecodeFrameState` and `DecodeSystemStat` decoders.
- `src/ncn/ncn_test.go` (new).
- `src/ftdi/tx.go`: RX decoding under sniff and the stat request/reply
  handling.
- `src/ftdi/main.go`: `stat` command and the `enqueue` helper.
- `src/ftdi/tx_test.go`: stat tests.

## Steps

1. `cd src && go vet ./ncn ./ftdi ./light ./config && go test ./ncn ./light
   ./config ./ftdi`
2. Stage only the files above. Leave the long-standing untracked items alone
   (`pcb/`,
   `src/main.go`, `src/nrf/`, `test/`, `tinygo/`, `uf2/`).
3. Commit in the repo's `[pkg|pkg]` subject style:

   ```
   [ncn|ftdi] stat command and decoding of NCN5121 indications

   Add a "stat" shell command that queries U_State and U_SystemStat and
   prints both decoded. With sniff on, indications in the RX stream
   (L_Data.con, U_State.ind, U_FrameState.ind, U_Reset.ind) are decoded
   below the hex dump; frame octets are skipped so they aren't mistaken for
   services. ncn.TxData now sends a frame in a single write.

   Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
   ```
4. No push.

## Verification

`git show --stat HEAD` lists exactly the six files above, and `git status`
shows only the
pre-existing untracked items.
