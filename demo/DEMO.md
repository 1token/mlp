# The minimum credible demo (Stage 3 Closing §5, D-41 extended)

Two real domains, one delivery lifecycle, every bullet observable.
`server/cmd/mlpd/main_test.go` (`TestTwoDomainDemo`) executes this
same journey programmatically over real TCP sockets — the record CI
keeps. This document is the on-camera script for the human run.

## Start

    ./demo/run.sh

- petra → http://127.0.0.1:8441 (origin.demo), password `correct horse`
- novak → http://127.0.0.1:8442 (target.demo), password `correct horse`

`-peer` switches the two domains onto localhost through
`discovery.NewDemoFetcher`; mlpd logs **DEMO MODE** at startup. Every
relaxation (loopback, plain HTTP, non-443) is demo-scoped; production
composition never touches those paths.

## The bullets

**1. Two real domains.** Two processes, two SQLite stores, two object
roots, discovery documents served at `/.well-known/medialet.json` and
fetched cross-domain — watch the logs on first contact.

**2. A delivery composed with a job tag.** As petra: Compose → attach
a large file (it is hashed IN THE BROWSER first — watch the progress
line; the address is the question, D-135) and a small preview →
subject, `novak@target.demo`, job tag `novak wedding` → Send (10 s
undo hold). *Store routing note: the accept-time store selector and
routing rules are client backlog (S3.7/D-160); stores exist
server-side, the demo uses `default`.*

**3. Tier-2 deferral with an auto-granted preview.** Petra and novak
are strangers: the verdict defers the large master but auto-grants
the small preview (D-139: ≤4 MiB within a 32 MiB envelope budget —
a recipient-policy knob, `sn.D139AutoGrant`, which mlpd ships on; the
spec default TV-002 freezes remains defer-all). In novak's inbox the
delivery renders alive: the preview arrived without any action;
`preview_of` (MEP-002) folds it under its master.

**4. Kill mid-flight, resume, zero redundant bytes.** As novak,
Accept the master. Watch origin's log: chunks PATCH toward target.
Ctrl-C origin mid-transfer; restart `./demo/run.sh`. The pusher HEADs
the receiver's durable checkpoint and continues FROM IT — the bytes
already there are never re-sent, and the object verifies by address
at completion (§8). The CI test performs this with a 2 MiB chunk and
a source that dies after the first chunk: offset lands at exactly
2097152, state `pushing`, and the resumed pass completes.

**5. `have` answering a resend.** After novak replies (bullet 6 —
which also makes petra a *correspondent*), petra resends the preview.
Tier 1 discloses possession (§7.5): the verdict answers `have`,
nothing is pushed, and novak's accept completes instantly
(`{"state":"available","instant":true}` — the D-241 short-circuit).

**6. The reply threads back; swept to zero.** As novak, Reply. The
reply joins petra's original thread (D-110), and Done sweeps it.
*Topic bundles and the sweep gesture proper are the parked S3.11
backlog (D-233); the thread-level lifecycle is what ships.*

**7. Guest → claim → instant-have.** As petra: Compose → a guest
recipient (`friend@example.net`) with a PIN → Send. Open the `/g/…`
link in a private window, enter the PIN (petra's second channel),
view the delivery — rendered by the same sanitizer the inbox uses —
download, then Claim a name. The mailbox exists, the delivery is in
its inbox via a real re-dispatch, and the files are available the
instant they are accepted, because the bytes never had to move.
