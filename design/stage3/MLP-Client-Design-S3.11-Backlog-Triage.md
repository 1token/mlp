# MLP Flagship Client Design — S3.11: Backlog Triage

> **Status.** Stage 3, session 11. Judgment calls D-177–D-180 pending
> editor confirmation. This document draws the line Stage 4 builds to.

---

## 1. The v1 definition (D-177)

**V1 is everything designed in S3.1–S3.10, minus the items triaged
below.** The demo-critical path it must serve flawlessly (D-41's
minimum credible demo plus Petra's working loop, D-35):

> compose (delivery page, job tag, store routing) → dispatch → Tier-2
> defer with auto-granted previews → accept ("download from sender") →
> pin → reply/thread → sweep to zero → day-40 resolution
> (recipient-side or Extend) — plus the guest delivery → claim →
> instant-`have` funnel end to end.

**One promotion into v1**: *minimal* **Save to Medialet** — a Save
compose preset (compose-to-self, protocol-free per D-79), the already-
designed *Saved* origin facet (D-157), and a PWA share target (a
static manifest beside the D-169 service worker). Nearly free, and it
completes the "central hub" idea at its smallest honest size. Browser
extension and web clipper stay post-v1. *(Igor marked this feature
optional; the promotion is his to veto.)*

## 2. Post-v1 roster (D-178)

Product roadmap, no spec dependency. Design constraints recorded now
bind later implementations:

| Item | From | Recorded constraint |
|---|---|---|
| Snooze | S3.1/D-120 | **Deadline-aware or not at all** — never snooze past an `available-until` without explicit warning |
| Full download scheduler (now / Wi-Fi / tonight) | S3.4/D-141 | The mobile-data guard ships in v1; scheduling layers on the same grant timing |
| On-the-fly download bundling (zip streaming) | S3.6/D-151 | Server CPU/bandwidth cost analysis first; author-attached archives remain the v1 answer |
| Template variables (`{client_name}`) | S3.3 | Must stay within the D-134 invariant — substitution happens pre-sign, never a client-side engine in the viewer |
| Video preview clips (beyond posters) | S3.3/S3.7 | Transcoding is a heavy server dependency — operator-optional feature, never a conformance assumption |
| Offline media ("keep offline") | S3.9/D-169 | Distinct from pin (retention ≠ device caching); vocabulary rule extends D-120's discipline |
| Delivery analytics | S3.5 | **Aggregate protocol facts only** (D-37); nothing per-recipient beyond what matrices already show |
| Bulk delivery operations (extend-all-expiring) | S3.5 | Rides D-122 mechanics; UI batching only |
| Correspondent import/export | S3.8 | — |
| Organization-level correspondents (domain as correspondent) | S3.8 | Interacts with tier definitions; design against D-19/D-162 |
| Content/EXIF search, smart albums | S3.7 | Home-server indexing (D-109 posture); privacy note: index never leaves the domain |
| Guest-page full localization | S3.6/S3.10 | Email language stays sender-selected (D-153); page language may add recipient choice |
| Save-to-Medialet extension/clipper | S3.1/D-177 | The v1 share target is the foundation |

## 3. MEP-gated (D-179)

Client features blocked on spec changes — each named with its MEP,
each shipping a v1 workaround meanwhile:

| Feature | MEP | V1 workaround |
|---|---|---|
| Custody-forward of window-expired Medialets (the day-40 forward path) | **MEP-001: fulfillment-window override** (`until` on `fulfillment_sources`, D-126) | Compose-new from library (D-124/D-125) — works today |
| Reliable preview pairing | **MEP-002: `preview_of` Manifest member** (D-158) | The figure heuristic + the preview marker + the pin-the-original nudge |
| Multi-parent threading | MEP candidate: `references` (D-110) | `in_reply_to` trees; dangling parents as local roots |
| Per-photo galleries at scale | MEP candidate: cap analysis / collection object (D-136) | The curated-delivery pattern |

**Filing recommendation**: MEP-001 and MEP-002 are small, additive,
and already analyzed — file both at Stage 4's start, in parallel with
implementation. **Implementation discipline stated**: the reference
implementation builds to the frozen spec (D-108); MEP features land
only after formal acceptance through the D-40 process — the process's
first two exercises, arriving on schedule. The `references` and
gallery-scale candidates wait for implementation evidence.

## 4. Non-feature carryovers (D-180)

- **Product brand name** (S3.1 §8): an editor decision required before
  launch, entangled with the D-39 trademark scope — flagged, not
  triaged.
- **The operator guide** — including the D-163 quarantine-disclosure
  option text, the D-139 policy knobs, and the D-159 GC classes — is
  registered as a Stage 4 documentation deliverable alongside the
  code.

## 5. Traceability (D-38)

Every v1 element traces through its origin session's table; this
document adds only the line itself. The triage criteria applied:
Petra's working loop (D-35), demo criticality (D-41), protocol
readiness (frozen spec vs MEP queue), and the scope-creep visibility
duty (D-38) — post-v1 items are deferred *with their constraints
recorded* so deferral never becomes silent redesign.

---

*Next: S3.12 — the Stage 3 Closing Document: the design record
D-109 through the close, the surface-by-surface artifact index, the
MEP queue in its final Stage 3 state, and the Stage 4 implementation
handoff — build order, the conformance anchors (TV-001–005 plus the
API draft), and the definition of "done" for the minimum credible
demo.*
