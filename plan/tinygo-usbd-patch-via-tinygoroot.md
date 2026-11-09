# Apply a TinyGo patch through a patched TINYGOROOT copy

> **Status: completed.** Implemented in commit `ba65dae` ("fix the console
> locking up while browsing history"), followed by `968317a` (2026-10-17).
> The plan was given as a chat answer, not in plan mode, so it is taken from
> the conversation; the user's replies follow it. 968317a replaced the patch
> with a single-DMA one, see `console-lockup-on-paste.md`; f6f36ca dropped
> both with TinyGo 0.43.0. Recovered from the planning session's transcript:
> the session reused one plan file for all its plans, so only the last one
> was left on disk.

## Context

The question: "How would this patch be applied?", about a fix to TinyGo
0.42.0's nRF52840 USB driver for the console locking up.

## Proposal

There are three ways to apply it. The `machine` package ships inside TinyGo
itself, not as a Go module, so a `replace` line in `go.mod` can't override it.
The patch has to go into a TinyGo root that the build uses.

**The change** (`src/machine/machine_nrf52840_usb.go`, around line 207):

```diff
 			if inDataDone {
 				if usbTxHandler[i] != nil {
 					usbTxHandler[i]()
 				}
-			} else if outDataDone {
+			}
+			if outDataDone {
 				enterCriticalSection()
```

Handling the received data in the same pass as a finished send can overlap two
EasyDMA transfers. The nRF52840 USB peripheral allows only one at a time. The
current driver already allows that overlap, because it starts sends from
normal code without the guard, so the patch adds no new class of problem. It's
still worth watching for when you test.

**Option 1: edit the system install in place.** This is the quickest, but I'd
avoid it.
- `/usr/local/lib/tinygo` is root-owned and installed by `dpkg` from
  `tinygo_0.42.0_amd64.deb`, so editing it needs `sudo`.
- A reinstall or upgrade silently overwrites the fix.
- Every TinyGo project on the machine picks up the change.

**Option 2: a patched copy of the TinyGo root, selected with `TINYGOROOT`.**
This is the one I recommend.
- I checked that `tinygo` honors the `TINYGOROOT` variable; it rejects a
  directory that isn't a valid root.
- The steps, from `/home/icarus75/dev/knixx-go`:
  ```sh
  # one-time: patched copy next to the .deb you already keep in tinygo/
  cp -a /usr/local/lib/tinygo tinygo/root-0.42.0
  patch -p1 -d tinygo/root-0.42.0 < patches/tinygo-0.42.0-nrf52840-usbd-epdata.patch

  # builds
  TINYGOROOT=$PWD/tinygo/root-0.42.0 tinygo build -target=feather-nrf52840 -o uf2/knixx.uf2 ./src/nrf
  ```
- What goes in the repo:
  - Only the small `.patch` file under `patches/` would be committed.
  - The copied root lives under `tinygo/`, which is already untracked, so it
    stays out of git.
  - A short `Makefile` or script would set `TINYGOROOT` and apply the patch if
    needed, so nobody has to remember.
- What it guarantees: the system install stays untouched and other projects
  are unaffected. The fix is versioned with knixx and pinned to TinyGo 0.42.0.
  If the patch stops applying after an upgrade, `patch` fails loudly.
- Run `tinygo clean` once after switching roots, so no cached build of the
  unpatched `machine` package gets reused.

**Option 3: send it upstream to tinygo-org/tinygo.** Do this as well, so you
can drop the local patch once a release includes it. The pull request would
carry the same diff plus the explanation: the CDC port shares endpoint 2 for
sending and receiving, and the `else if` drops received data when both events
land in one interrupt.

If you agree with option 2, I'll write the patch file and the build script,
and make the single-write `lineedit` change. You'd then rebuild and test on
the board.

## Decision (the user's reply)

- go with option 2
