# MLP Flagship Client Design — S3.1: Product Frame & Information Architecture

> **Status.** Stage 3, session 1. Judgment calls D-118–D-123 pending editor
> confirmation. Inherits the Stage 2 handoff (Stage 2 Closing Document §6)
> and the technology frame D-113–D-117. Per D-38, §9 traces every feature
> to its persona need or frozen decision.

---

## 1. Purpose

This document fixes the flagship client's product frame and screen-level
information architecture. Later sessions detail each surface; nothing here
requires core-spec changes (D-109).

Working title: **Medialet App**, served at `app.medialet.org` (brand
separation per D-41); real product naming is deferred as a trademark
matter (§8).

## 2. The product frame: one store, two lenses

The client serves two paradigms that must not be forced into one frame
(D-112): the **recipient's** (triage, reading, accepting — the
Inbox-by-Google model) and the **sender's** (deliveries as jobs with
lifecycles — Petra's model). The resolution is **one underlying data
model, two lenses**: a delivery is a single object that appears in a
conversation thread (Inbox lens) *and* on the status board (Deliveries
lens). Nothing is stored twice; nothing is filed in only one world.

## 3. Screen map

Global shell navigation, in order:

| Surface | One-line charter | Detailed in |
|---|---|---|
| **Inbox** | Default surface. Time bundles → topic bundles → threads; triage to zero. | S3.2 |
| **Deliveries** | The Sender Studio home: every outgoing delivery as a status object. | S3.5 |
| **Media** | The library: all objects across the domain's stores, galleries, labels, pins, quota. | S3.7 |
| **Correspondents** | People and organizations: history, trust tier, per-correspondent routing (D-107), block. | S3.8 |
| **Search** | Global: derived text, subjects, correspondents, labels, media names/types. Server-indexed (D-109). | S3.2/S3.7 |
| **Settings** | Account, stores (D-105), quotas, junk policy, guest-link policy (Annex A). | per topic |

**Compose** is a persistent global action (button, keyboard shortcut,
share targets), not a navigation destination; it opens the composer
(S3.3) from anywhere, pre-scoped to context (composing from a thread =
reply; from a correspondent = addressed; from Media = attach-first).

## 4. Inbox information architecture

### 4.1 Bundling (feature 1)

Two nesting levels, both collapsible:

- **Time bundles**: Today · Yesterday · This week · Last week · Earlier,
  by month. Pure presentation over `received_at` (Delivery Record, D-53).
- **Topic bundles** within time bundles: see §4.2.

Thread items sit inside bundles. **Sweep** — mark an entire bundle Done
in one gesture — is the signature Inbox-Zero affordance and appears on
every bundle header.

### 4.2 Topics are bundled Labels (D-119)

There is exactly **one recipient-side taxonomy: Labels** (feature 3).
A label carries a "bundle in inbox" switch; switched on, it renders as a
Topic bundle (Trips, Healthcare, Novák Wedding). This is the Inbox-by-
Google model adopted knowingly — it prevents two parallel filing systems
from diverging.

Label assignment sources, in descending trust order, all user-teachable:

1. **Subaddress tag** (D-55) — the recipient issued `igor+health@…`, so
   the tag is authoritative filing intent; delivered tags map to labels
   automatically.
2. **Correspondent rules** — "everything from hospital.example →
   Healthcare."
3. **Classifier** over derived text (§11.6) and metadata — the same
   receiver-side machinery species as the D-21 hooks; server-side per
   D-109 (the home server already sees everything, D-34 — preprocessing
   adds no exposure).
4. **Manual** assignment, which teaches rules 2–3.

Labels also apply to individual media objects (feature 4) as
reference-level metadata (§10.3) — detailed with the Media surface
(S3.7). Author-side traveling tags remain future MEPs under the D-111
constraint (advisory only, never routing inputs).

### 4.3 Threads

Per D-110: `in_reply_to` content-address trees; dangling parents render
as local thread roots; forward-stability of content addresses means
replies to forwarded Medialets thread correctly everywhere. A thread
item's face rolls up: participants, latest snippet (derived text,
§11.6), unread state, and the aggregate **media chips** of the whole
thread.

### 4.4 Item anatomy

Each thread item shows: sender (display-safe per §4.4 of the spec),
subject, snippet, **media chips** (thumbnail or type icon + size + a
state badge from the §10.3 reference states: *offered / expected /
available / pinned / tombstone*), and **deadline chips** where an
`available-until` clock is running ("accept within 5 days"). The chips
are the product's visual signature: unlike mail, these items carry heavy
objects with lifecycles, and the lifecycle is always visible.

### 4.5 Triage (feature 2; D-120)

**Done** removes from Inbox (findable under All — the Inbox-by-Google
model; no separate "archive" concept). Done is triage, never retention:
it MUST NOT unpin, GC, or alter any §10 state (D-109 guard). **Flag**
keeps an item prominent in Inbox despite sweeps. Terminology rule,
project-wide: **"pin" refers exclusively to D-21 media retention**; the
triage marker is always "flag." Snooze is deferred to backlog (S3.11)
and, if ever adopted, must be deadline-aware — snoozing past an
`available-until` without warning is data loss by UI.

## 5. Deliveries (Sender Studio) information architecture

The Deliveries list shows every outgoing delivery as a status object:
recipients, headline state, expiry countdowns, unread-reply indicator.
The detail view is two matrices (D-122):

- **Recipient × status**: per-recipient message verdicts (§7.4), guest
  vs. federated, acceptance events with their times (shown because they
  are shown to us — D-98 transparency works both directions).
- **Object × state**: per-URN verdict → transfer progress → verified →
  accepted-when; delegation events rendered honestly ("forwarded copy
  fetched by final.example", the D-23 visibility).

First-class actions: **Resend** (the day-9 case, D-35);
**Extend availability** — implemented as re-dispatch with a fresh
Manifest window, since the signed original is immutable; `have`-dedup
makes extension near-free for already-transferred objects (zero bytes
move) and correct for untransferred ones (fresh window, D-122). Extend
is the *author-side* remedy; when recipients themselves hold the
objects, recipient-side resolution (§7 step 6, D-124) needs no author
at all;
**Guest-link management** per delivery (revoke, PIN, expiry — Annex A).
Delivery-page templates are composer material (S3.3).

## 6. The tagged-author pattern (D-123)

The compose flow exposes a **Job tag** field: Petra authors as
`petra+novak-wedding@origin.example` (valid per D-55; correspondent
matching ignores tags, delivery routes to her base mailbox). Every reply
from the Nováks carries the tag back, where it is the strongest topic
signal (§4.2 rule 1). One field bridges sender-side job organization,
recipient-side reply filing, and inbox bundling — no new protocol
machinery, no new taxonomy.

## 7. Walkthrough: Petra and the Nováks

1. **Compose** (Studio): Petra picks her delivery template, sets Job tag
   `novak-wedding`, drags in 214 files. Background upload to her chosen
   store (the D-105 selector; masters → NAS store, previews → fast
   store per her D-107 routing defaults). Compose-time BLAKE3 (D-116)
   flags "3 files already in your store" before a byte moves.
2. **Send**: dispatch to `novak@target.example`. Deliveries shows:
   *delivered · media deferred (first contact)*.
3. **Novák side**: the Medialet renders fully (Body + metadata); small
   previews may be auto-granted under the operator's threshold policy
   (open question §8.1); the masters show *offered · 30 days*. At home,
   "Accept — download from sender" (the D-98 wording made honest UI)
   starts transfer; progress on chips; done → *available*; they **pin**
   the twelve favorites.
4. **Reply**: "Thank you!" — a text-only Medialet (D-49) threading via
   `in_reply_to`.
5. **Petra's Inbox**: the reply lands in the **Novak Wedding** topic
   bundle (tag → label, §4.2), threaded under the delivery. Deliveries
   shows *accepted 2026-07-04 12:30*. She reads, smiles, **sweeps** the
   bundle. Inbox zero.
6. **Day 40**: the couple's cousin wants the files. The Nováks hold
   the objects (accepted at step 3) — so this resolves **recipient-
   side, without Petra** (D-124): either **compose a new Medialet**
   attaching the same objects from their library (manifest-from-
   library, zero re-upload, fresh windows, their own authorship —
   works today, D-125), or **forward the original** with custody
   fulfillment from their own store, preserving Petra's authorship —
   which works within the original window today, and past it awaits
   the fulfillment-window MEP (§8.2). Petra's server learns nothing
   in either path (§9.6 privacy). **Extend availability** (§5)
   remains the fallback for objects the recipients never accepted or
   locally GC'd — the case where only the author can help.

## 8. Open questions parked

0. **MEP queue** (spec-layer items, to be filed formally per D-40, never
   silently patched): **(a) fulfillment-window override** — an optional
   `until` member on `fulfillment_sources` entries expressing the
   custody holder's own offer window, governing the §10.3
   `expired-remote` transition when that source is active; unblocks
   custody-forwarding of window-expired Medialets (D-126) — now the
   leading MEP-001 candidate. **(b) `references` threading chains**
   (D-110) — remains speculative, behind (a).
1. **Preview auto-grant policy** (→ S3.4): auto-granting small objects
   for Tier 2 makes first-contact deliveries feel alive, but naïvely
   allows 256 entries × threshold bytes per envelope. Proposal to
   evaluate: size threshold (e.g., ≤ 4 MiB) **plus a cumulative
   per-envelope auto-grant budget** (e.g., ≤ 32 MiB) — bounded exposure,
   preserved UX. Operator-configurable per D-19.
2. **Snooze** (→ S3.11): deadline-aware or not at all.
3. **Product brand name**: deferred; trademark scope per D-39 applies.
4. **Save to Medialet** (→ S3.11 backlog per D-109): compose-to-self is
   already free protocol-wise (D-79); the feature is capture UX.

## 9. Traceability (D-38)

| Feature | Traces to |
|---|---|
| Time bundles, sweep, Done | Igor F1/F2; `received_at` D-53; D-120/121 |
| Topics = bundled labels | Igor F1/F3; D-119; tags D-55; hooks D-21 |
| Threads | Igor F1; D-110; `in_reply_to` D-49 |
| Media chips & deadline chips | §10.3 states; D-19 `available-until`; D-87 |
| Labels on media | Igor F4; D-111; §10.3 reference metadata |
| Deliveries matrices | Petra D-35; verdicts D-70; D-37 protocol facts; D-98 |
| Extend-as-redispatch | Immutability D-02/D-28; dedup D-16 `have` |
| Job tag authoring | D-55/D-123; Petra's filing D-35 |
| Store selector & routing | D-105–D-107 |
| Guest links | D-36; Annex A |
| Server-side preprocessing | Igor F1; D-109; D-79 intra-domain freedom |

---

*Next: S3.2 — the Inbox in full detail: bundle algorithms and ordering,
thread and item layouts, the triage gesture set, label management UX,
unread semantics, and the empty-state ("inbox zero") design.*
