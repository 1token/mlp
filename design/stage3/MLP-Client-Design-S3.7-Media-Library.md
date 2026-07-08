# MLP Flagship Client Design — S3.7: Media Handling & the Library

> **Status.** Stage 3, session 7. Judgment calls D-156–D-160 pending
> editor confirmation. §7 traces per D-38.

---

## 1. Object-level library over reference-level storage (D-156)

The spec's retention model is per-mailbox **references** (§10.3);
users think in **files**. The library therefore renders **one card per
URN**, aggregating that object's references in the mailbox:

- **Provenance**: "appears in 3 deliveries" — each linked to its
  thread; the domain's dedup economics surfaced as a feature rather
  than hidden as plumbing.
- **State rollup**: best state wins (any reference `pinned` → pinned;
  else any `available` → available; else the liveliest of
  offered/expected; tombstone only when all references are).
- **Labels** (D-111): displayed as the union across references;
  applying or removing a label from the card writes through to all the
  mailbox's references of that object — object-level UX, reference-
  level storage, no spec bending.

## 2. Views and facets (D-157)

Default view: a responsive **grid**, time-ordered by newest reference,
infinite scroll. Tiles: thumbnails for locally available images and
video posters; type-icon tiles for documents, audio, archives; the
D-128 state-badge vocabulary reused verbatim.

Facet rail:

| Facet | Values / notes |
|---|---|
| Type | Photos · Videos · Audio · Documents · Archives |
| State | Available · Pinned · Offered · **Expiring** (offered, <7 d — the D-131 twin) · Tombstones |
| **Store** | The D-105/D-107 partition facet — Health · Business · Default — each with a mini quota meter inline |
| Label | The D-111 taxonomy |
| From | Correspondent |
| Delivery / Job | The Medialet it arrived in; job tags for senders (D-123) |
| Origin | Received · Sent · **Saved** (the Save-to-Medialet slot, D-109 #5 — facet exists, feature backlogged) |

The library is **unified across sent and received**: a sender's
uploaded objects are store objects like any other, and their cards
show the outbound side — "promised to 2 deliveries through Aug 3"
(§10.5/D-88, the D-145 meter at object grain). Search covers name,
label, correspondent, type; content/EXIF search is backlogged.

## 3. The lightbox and the preview honesty (D-158)

Full-bleed viewer; swipe/arrow navigation within the current filtered
set; on every item: the **pin glyph** (D-144's placement — attachment
peaks here), label shortcut, and an info panel (name, size, type,
truncated URN with copy, provenance, state, store, deadline when
offered). Available video streams ranged from the home BS (§10.8).

**Pixels versus promises.** An offered master with an auto-granted
preview (D-135/D-139) renders the preview **with a visible "preview"
marker** and the accept CTA overlaid — "Preview · Download original
from sender (2.1 GB)". An offered object with no preview renders a
type placeholder + metadata + CTA. The marker exists because the
failure mode is real: pinning a 200 KB preview while believing the
master is saved.

**The pairing gap, handled honestly**: nothing structural tells the
recipient that object A previews object B — D-135's pairing is
authored knowledge living only in the Body's figure markup. V1 infers
it from the figure pattern (`img[urn-A]` + `a[urn-B]` in one figure),
documented as a heuristic; a small additive **`preview_of` Manifest
member joins the MEP queue** (batched naturally with the
fulfillment-window MEP, D-126). Either way, the UX guard ships now:
pinning a preview whose known master is undownloaded prompts —
*"Pin the original instead? It hasn't been downloaded yet."*

## 4. Pins at scale and quota surfaces (D-159)

Bulk pin/unpin via grid selection; the **Pinned** facet is "my
keepers," its header showing pinned bytes against quota. Unpin states
its consequence once: "unpinned files may be removed when space is
needed" (D-88).

**The segmented meter** — per store, in Settings and inline in the
Store facet — explains every byte:

```
[■■■■■■■■ pinned 41 GB │ ■■■■ unpinned 18 GB │ ▨▨ auto-cleaning 2 GB │ ▤▤ promised 14 GB ]  75 / 100 GB
```

*Pinned* (D-21), *unpinned available*, *auto-cleaning* (the D-139
ephemeral class, named for what it does), *promised* (outbound
retention duties, §10.5 — the sender's own promises sharing the disk,
visible).

**The cleanup flow** (from quota pressure or the D-141 accept-time
pre-check) suggests candidates **in exactly the order GC would collect
them**: ephemeral class first, then largest unpinned (D-139/D-88) —
the UI's advice and the server's behavior are one algorithm, so
freeing space never produces later surprises. Candidates group by
delivery ("unaccepted previews from 8 old deliveries · 3 GB; unpinned
masters from 'Hike 2025' · 9 GB"), and the consequence is stated:
freed objects tombstone, re-requestable where sender windows live
(D-143), otherwise gone. Pinned objects never appear in the flow —
the D-88 invariant restated as absence.

## 5. Store management (D-160)

Settings → Stores: each instance with name, backend note, quota, and
its segmented meter. Below: the **routing rules editor** — the D-107
recipient-controlled signals as literal rule rows:

```
tag  health        → Health store
from *.hospital.example → Health store
type video/*      → Media NAS
(default)         → Default store
```

Plus the accept-time selector (D-141) as the manual override these
rules pre-answer. **Move between stores** is a first-class object and
bulk action — free repointing per D-28's location independence
(D-107's migration), with the card's store facet updating in place.
Per-store dedup scope (D-106) surfaces as a read-only operator note
where applicable.

**Derivatives**: grid thumbnails for accepted masters are generated
home-server-side (the D-109 preprocessing posture — the server holds
the bytes), with client-side on-the-fly fallback; a thumbnail endpoint
(`GET {bs}/o/{urn}/thumb?w=…`) is registered as an S3.9 client-API
requirement at the same informative level as §10.8.

## 6. Open questions parked

1. Content/EXIF search and smart albums — S3.11 backlog.
2. `preview_of` MEP drafting — the MEP queue (with D-126).
3. Save-to-Medialet capture UX — S3.11 (the Origin facet slot awaits
   it).

## 7. Traceability (D-38)

| Element | Traces to |
|---|---|
| One card per URN, provenance, rollup | §10.3/§10.5; D-21; D-156 |
| Label write-through | D-111; §10.3; D-156 |
| Facets incl. Store meters, Expiring | D-105–107; D-131; D-157 |
| Unified library, promised-at-object | §10.5; D-88/D-145; D-157 |
| Lightbox pin placement | D-144; D-158 |
| Preview marker, pairing heuristic, nudge | D-135; D-139; D-158 |
| `preview_of` MEP candidate | D-126 queue discipline; D-158 |
| Segmented meters | D-21/D-88/D-139; §10.5–10.6; D-159 |
| Cleanup = GC order | D-88/D-139 consistency; D-159 |
| Rules editor, free moves | D-107; D-28; D-160 |
| Server-side thumbnails | D-109; S3.9 registry; D-160 |

---

*Next: S3.8 — identity, correspondents, and junk: passkey-first signup
and recovery (the claim flow's dependency, D-154), the Correspondents
surface (tiers made legible, allowlisting, per-correspondent routing),
display-safety in practice, the Junk surface with its structural
cheapness and rescue flow, and blocking.*
