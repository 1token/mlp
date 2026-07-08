# MLP Flagship Client Design — S3.2: The Inbox

> **Status.** Stage 3, session 2. Judgment calls D-127–D-132 pending
> editor confirmation. Builds on the S3.1 frame (D-118–D-123); §8 traces
> per D-38.

---

## 1. Bundling algorithm (D-127)

**Time sections**: Today · Yesterday · This week · Last week · Earlier,
by month; reverse chronological; empty sections do not render.

**Placement rules:**

1. A thread's section is determined by its **latest non-done activity**
   (`received_at` of the newest message not marked done). A thread
   appears exactly once in the Inbox, never duplicated across sections.
2. A bundle-promoted label (D-119) **materializes per section**: the
   "Novak Wedding" bundle may appear in *Today* and again in *Last
   week*, each instance containing only that section's threads.
3. A bundle instance containing a **single thread merges** bundle header
   and item into one row (topic prefix + full item anatomy) —
   predictability without tap-tax.
4. **Flagged threads are hoisted**: they render bare, above the bundles
   of their section, flag glyph visible. A flag swallowed by a collapsed
   bundle would be no flag at all.
5. Ordering within a section: by latest activity, descending; a bundle
   sorts by its newest thread.

**Collapse behavior:** bundles default collapsed; expansion state
persists per label in the store. A collapsed header shows: topic name +
color, thread count, top two sender names, up to three aggregated media
chips, the **most urgent deadline chip bubbled up** (a collapsed bundle
must never hide a ticking clock), and the sweep control.

## 2. The "Expiring offers" system bundle (D-131)

Auto-materializes pinned above *Today* whenever any thread not marked
done contains an `offered` reference whose `available-until` is under
**7 days**; aggregates across all time sections; each row shows the
countdown prominently; dissolves when empty. This is the deadline
economy of the protocol (D-19) surfaced as furniture — the
Medialet-native feature no mail client has because no mail protocol has
windows.

## 3. Row anatomy (D-128)

Two-line rows (~72 px desktop; equivalent mobile), thread-level rollup:

```
┌──────────────────────────────────────────────────────────────────┐
│ ● Petra Fotografka, You 3        Nováková wedding — final   12:30 │
│   Thank you so much! The photos are…   [▣][▣][▦+204] [⏳ 5d] ⚑    │
└──────────────────────────────────────────────────────────────────┘
```

Line 1: participants (display-safe per spec §4.4; "You" for self;
count for >2), subject, time. Unread = bold + accent bar (●).
Line 2: snippet from the **derived text** (§11.6, D-95) — the same
string the classifier sees, one honesty of representation; right
cluster: media chips + deadline chip + flag glyph.

**Media chips** aggregate the whole thread: up to three thumbnails,
`+N` overflow. Thumbnails render **only for locally `available`
objects** (previews come from the home BS; an `offered` object has no
local bytes to thumbnail — its chip is a type icon + declared size).
State badges map §10.3 exactly: *offered* = dashed outline + ↓;
*expected* = progress ring; *available* = plain; *pinned* = pin glyph;
*tombstone* = greyed + slash. **Deadline chips**: neutral ≥7 d, amber
<7 d, red <48 h.

## 4. Triage (D-129)

**Touch:** swipe right = **Done** (the Inbox-Zero gesture); swipe left =
**Flag**; long left-swipe = label sheet. **Desktop hover:** ✓ Done,
⚑ Flag, 🏷 Label. **Selection mode** (long-press / `x`): bulk done,
flag, label.

**Keyboard** (Gmail muscle memory where it maps):

| Key | Action | Key | Action |
|---|---|---|---|
| `j` / `k` | next / previous | `e` | Done (on bundle header: sweep) |
| `Enter` | open | `s` | flag / unflag |
| `u` | back to list | `l` | label sheet |
| `x` | select | `z` | undo |
| `c` | compose | `/` | search |
| `#` | delete (confirm) | `g` `i` | go to Inbox |

**Sweep** (bundle header ✓): marks every thread in the bundle instance
**done and read as one transactional unit**; the universal **5-second
undo snackbar** restores both states atomically. Every triage action is
undoable; nothing in triage ever touches §10 retention state (the D-120
guard, restated at the gesture layer).

## 5. Labels (D-130)

One taxonomy (D-119). A label carries: name, color, the **bundle
switch**, attached rules, and a **notification policy** — *instant /
daily digest / silent* (the Inbox-by-Google insight that a Newsletters
bundle should never buzz; a Healthcare bundle should).

**Assignment & teaching:** delivered subaddress tags auto-map
(strongest signal, §4.2 of S3.1); moving a thread to a label prompts
rule creation ("Always label messages from petra@…?"); recurring
unmapped tags prompt label creation ("You receive messages tagged
`novak-wedding` — create a bundled label?").

**System labels** (fixed): **Inbox**, **All** (everything incl. Done),
**Flagged**, **Junk**, **Trash** (deleted Medialets, 30-day
convention, then mailbox-policy purge), plus **Deliveries** as a
cross-link to the Studio lens (one store, two lenses — D-118). **Junk**
renders quarantined items (§7.7) with their structural cheapness made
visible ("junk holds no files — these messages weigh kilobytes",
D-15/D-19); the **rescue** action (not-junk) triggers the deferred-
upgrade path exactly as a Tier-2 accept does.

## 6. Unread semantics and Inbox Zero (D-132)

Thread unread = any message unread; opening a thread marks messages
read as they are exposed. Bundle headers badge **unread thread counts**;
time sections carry no counts (noise); the app badge counts Inbox
unread threads. Sweep marks read (§4).

**The zero state**: when a section empties it vanishes; when the Inbox
empties, the sun rises — a calm illustration (visual language in
S3.10), one rotating line ("All clear."), and the useful residue:
*Flagged (3)* quick link, a deadline glance ("2 offers expire this
week"), and the storage meter. No confetti; zero is a resting state,
not a slot machine.

## 7. Server contract registered for S3.9 (D-109, D-132)

The Inbox requires from the client↔home-server API: a threads view with
**precomputed rollups** (section key, label ids, participants, derived-
text snippet, media chip aggregate — counts, states, preview refs —
most-urgent deadline, unread counts); cursor pagination per
section/bundle; mutation endpoints (done, flag, label, sweep-batch with
transactional undo tokens); and a **live change feed — SSE** (fits the
no-build, plain-HTTP ethos of D-113 better than WebSocket ceremony).
Carried as requirements into the S3.9 client-API draft.

## 8. Traceability (D-38)

| Element | Traces to |
|---|---|
| Latest-activity sections, one appearance | Igor F1; D-121/D-127 |
| Per-section bundle instances, merge rule | Inbox-by-Google model; D-119/D-127 |
| Flag hoisting | D-120 (flag ≠ pin); D-127 |
| Deadline bubbling, Expiring-offers bundle | D-19 windows; §10.3 states; D-131 |
| Chip state vocabulary | §10.3; D-87/D-128 |
| Derived-text snippets | §11.6; D-95/D-128 |
| Sweep as transactional unit | Igor F2; D-120/D-129 |
| Label system, notification policy | Igor F1/F3; D-119/D-130 |
| Junk cheapness surfaced, rescue path | D-15/D-19; §7.7; D-130 |
| Server rollups + SSE | Igor F1 (server-side preprocessing); D-109/D-132 |

---

*Next: S3.3 — the composer: delivery-page authoring (templates,
branding, drag-in media with background upload), the Job-tag field
(D-123), the D-105/D-107 store selector, attach-from-library (D-125),
compose-time dedup via BLAKE3 (D-116), recipients with display-safety,
and the draft lifecycle from first keystroke to signed dispatch.*
