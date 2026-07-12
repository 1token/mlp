# MEP-001: Fulfillment-window override for custody forwarding

| | |
|---|---|
| **Status** | **Accepted** |
| **Type** | Additive (optional member; no wire-version bump per D-101) |
| **Filed** | 2026-07-06 · **Decided** | 2026-07-12 |
| **Affects** | Spec §3.4.1 (`fulfillment_sources`), §9.5, §10.3; no registry changes (member additions are spec-governed per D-100) |
| **Origin** | D-126 — surfaced by the editor's day-40 correction (Stage 3, S3.1) |

## Motivation

A recipient who holds accepted objects may forward the original Medialet
with custody fulfillment (§9.7) — preserving the author's signature —
but the forwarded, byte-immutable Manifest carries the **original**
`available_until`. §10.3 transitions an `offered` reference to
`unavailable(expired-remote)` when that date has passed, so forwarding a
window-expired Medialet yields immediately-expired offers at the new
recipient **even though the custody holder is willing and able to
fulfill**. The custody holder's own offer window has no wire
representation. (The v1 workaround — compose-new from library, D-124/
D-125 — works but discards the original author's cryptographic
provenance.)

## Specification change

§3.4.1, the `fulfillment_sources` entry gains one OPTIONAL member:

> `until` — string, OPTIONAL, RFC 3339 UTC. The declaring source's own
> offer window for the URNs this entry covers: its promise that it will
> honor grants (as enveloping origin, §7.6) or delegation requests
> (§9.4) for those objects until the stated time.

§10.3, the `offered → unavailable(expired-remote)` trigger is amended:

> the transition fires when the **effective offer deadline** passes —
> the latest of the Manifest `available_until` and the `until` of every
> listed fulfillment source covering the URN. Absent any `until`, the
> Manifest value governs (unchanged behavior).

§9.5 gains one sentence:

> A source honoring requests as a custody holder is bound by the
> `until` **it itself declared** in its dispatch (validated against its
> own records); a root origin remains bound by its own Manifest
> `available-until`. **No party's declaration ever extends another
> party's obligations.**

## Semantics

The `until` binds only its declaring source. The original author's
promise is untouched — the immutable Manifest still says what the
author said; the Envelope (hop-signed by the declaring forwarder,
§3.4.3) carries the forwarder's own, separately-attributed promise.
Recipient clients MAY display per-source provenance of the effective
deadline ("offered by target.example until Sep 1").

## Compatibility

Old receivers ignore the unknown member (D-43 rule 5) and apply the
Manifest date — conservative degradation: offers appear expired though
the source would honor them; nothing breaks, nothing is over-promised.
Old senders never emit the member; new receivers see unchanged
behavior.

## Security & privacy considerations

A forwarder can declare an `until` beyond its real retention; the
existing honesty posture already covers this (`not-available`
degradation, §9.5/D-88 — promises are floors backed by graceful
failure, not enforcement). No new metadata is disclosed: the member
travels only where the forwarder's dispatch already travels.

## Conformance impact

TV-004 extension (or TV-006): a custody forward carrying `until` past
the Manifest window; §10.3 transition tests updated for the
effective-deadline rule; a validation case for the §9.5 sentence
(declared-`until` checked against the source's own records).

## Editor decision

**Accepted** (2026-07-12). Rationale (D-40): the day-40 correction
(D-126) exposed a real provenance loss — the v1 compose-new workaround
discards the author's signature exactly where custody forwarding
exists to preserve it. The member is additive, binds only its
declarant (the no-extension sentence is the load-bearing line), and
degrades conservatively under D-43 rule 5. Conformance lands as
TV-006 with the §10.3 effective-deadline and §9.5 own-record cases;
frozen vectors stay untouched.
