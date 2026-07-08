# MLP Flagship Client Design — S3.4: Receiving & Accepting

> **Status.** Stage 3, session 4. Judgment calls D-139–D-144 pending
> editor confirmation. Decides the auto-grant question parked at S3.1
> §8.1. §8 traces per D-38.

---

## 1. The auto-grant policy (D-139) — parked question, decided

Flagship default acceptance policy for **Tier 2** (verified first
contact, D-19): message `accepted`, Media `defer` — **except** objects
auto-granted under both limits:

| Limit | Value | Rationale |
|---|---|---|
| Per-object | ≤ 4 MiB | Covers composer previews (~200 KB target, D-135), posters, small docs |
| Per-envelope cumulative | ≤ 32 MiB | ~160 previews of headroom; bounds the worst case regardless of the 256-entry cap |
| Allocation | First-fit in Manifest order | The author curates which objects deserve the budget; composer rule added: **previews listed first** (extends D-135) |

**Tier 3 / quarantined: no auto-grant whatsoever** — junk stays
kilobytes (D-15), structurally.

**The quota honesty**: auto-granted stranger objects charge the
recipient's quota (§10.6) — the exhaustion vector in miniature. Answer:
auto-granted Tier-2 objects enter an **ephemeral retention class** —
GC-first under pressure, collected after ~30 days without interaction
(opening the Medialet, pinning, accepting more). Entirely within the
frozen D-88 invariants: which unpinned objects GC takes, and when, is
operator policy; this names a class and an order. Interaction promotes
the objects to ordinary standing. All numbers operator-configurable per
D-19.

## 2. Reading anatomy (D-140)

The thread view renders the Medialet through the **Body viewer** — the
one shadow-DOM component family (D-115), consuming the render form
(§11.5) under the §11.7 floor.

- **Inline previews**: auto-granted objects resolve instantly through
  the home BS (§10.7) — the page is alive on first open.
- **Inline object chips**: Body anchors targeting Manifest URNs
  (`download original`) are upgraded by the viewer into stateful chips:
  `⬇ final-master.mp4 · 2.1 GB · 28 d left · Accept`. States track
  §10.3 live (offered / progress ring / open / tombstone).
- **The Files panel** below the Body: the canonical operational list of
  *every* Manifest object — including entries the Body never referenced
  (legal per D-02) — with state, size, deadline, and per-object
  actions. The Body is presentation; the Files panel is operations.
- **First-contact framing**: a calm banner on Tier-2 threads — "First
  message from petra@origin.example · signature verified" (D-32
  surfaced as trust vocabulary, §4.4 display-safety applied) — with
  junk/block in the overflow.
- **Page-level expiry banner** when any live offer < 7 days ("These
  files expire in 5 days — Accept all"), the in-thread twin of the
  D-131 bundle.

## 3. The accept affordance (D-141)

The button reads **"Download from sender"** — the D-98 disclosure
carried in the affordance itself: accepting is a visible act on the
sender's side, and the wording makes that inherent rather than
fine-print. One-time explainer on first use: "The sender's server
transfers these files to your storage; the sender can see that you
accepted. Reading and viewing are never visible." Never repeated
per-click.

- **Accept all** for the masters+archives gesture; per-object accept on
  chips and panel rows.
- **Quota pre-check** before the grant: "Needs 48 GB · 12 GB free" —
  with the **accept-time store selector** (the D-107 decision point:
  Health / Business / Default) and cleanup suggestions (largest
  unpinned first).
- **Mobile-data guard** setting ("accept large files on Wi-Fi only");
  a full download scheduler is backlogged (S3.11).
- Progress renders on the chips (expected → ring), transfers run in
  background, completion notifies per the label's notification policy
  (D-130).

## 4. Decline, wrapped safely (D-142)

Protocol `deny` is terminal (D-71). The UI therefore distinguishes:

- **Dismiss** (default swipe/action): local only — the reference shows
  `declined` locally, restorable from the Files panel while the window
  lives; no wire message.
- **Decline permanently** (overflow, confirm) and **junk-marking**:
  these issue the protocol transition. Terminality is a spec fact; the
  client never hands it out as a casual gesture.

## 5. Tombstones and resend (D-143)

Cause-specific rendering per the §10.4 record, each with its action:

| Cause | Copy | Action |
|---|---|---|
| `expired-remote` | "No longer offered by the sender" | **Request a resend** |
| `expired-local` | "Removed to free up space" | Request again |
| `declined` | "You dismissed this" | Restore (window live) / Request |
| `failed` | "Transfer failed" | Retry |
| `deleted` | "You deleted this" | — |

**Request a resend** is a templated human reply Medialet ("Could you
resend *final-masters.zip*?") — deliberately not a protocol primitive;
the author's client recognizes it and offers Extend/Resend (D-122).
Recipient-side resolution (D-124) is checked first: if the objects are
held locally or by a forwarding path, the client routes there before
bothering the author.

## 6. Pin UX (D-144)

The moment of maximum attachment is right after the download finishes
and the browsing begins — pin lives there:

- Pin on object cards, on Files-panel rows (bulk), and **in the
  lightbox** — a pin glyph on each photo while the couple browses.
- One-time explainer: "Pinned files never expire from your storage and
  count toward your quota" — D-21, D-88, and D-89 in one sentence.
- One gentle nudge per delivery, after a large accept completes:
  "Pin your favorites to keep them permanently." Once; never nagging.
- Copy discipline enforced project-wide: **pin** is retention only;
  the inbox marker is always **flag** (D-120).

## 7. Open questions parked

1. Download scheduler (now/Wi-Fi/tonight) — S3.11 backlog.
2. Lightbox/gallery interaction detail — S3.7 (media handling).
3. Guest-recipient variant of this page — S3.6 (shares the Body viewer
   per D-115).

## 8. Traceability (D-38)

| Element | Traces to |
|---|---|
| Auto-grant thresholds & budget | S3.1 §8.1; D-19 configurability; D-139 |
| Ephemeral GC-first class | D-88 invariants; §10.5; D-139 |
| Junk gets nothing | D-15/D-19; §7.7 |
| Previews-first Manifest order | D-135 extension; D-139 |
| Body viewer, inline chips, Files panel | D-115; §11.5–11.7; D-02; D-140 |
| Verification banner | D-32; §4.4; D-140 |
| "Download from sender" | D-98; D-37; D-141 |
| Accept-time store selector | D-105/D-107; D-141 |
| Dismiss vs deny | D-71 terminality; D-142 |
| Tombstone causes & actions | §10.3–10.4; D-87; D-143 |
| Resend as human reply | D-122/D-124; D-143 |
| Pin surfaces & explainer | D-21/D-88/D-89; D-120; D-144 |

---

*Next: S3.5 — status and tracking: the Deliveries detail view built
from the two matrices (D-122), verdict/transfer/acceptance timelines
rendered as protocol facts (D-37), guest-link status alongside
federated recipients, delegation events shown honestly, and the
Extend/Resend flows from the author's chair.*
