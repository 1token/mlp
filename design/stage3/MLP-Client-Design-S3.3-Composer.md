# MLP Flagship Client Design — S3.3: The Composer

> **Status.** Stage 3, session 3. Judgment calls D-133–D-138 pending
> editor confirmation. This is the surface where "the message is the
> product" (Stage 1, D-35) is realized or lost. §9 traces per D-38.

---

## 1. Two modes, one draft (D-133)

A "thank you" reply and a wedding delivery page are different acts; one
chrome cannot serve both. The composer has two **modes over a single
draft model**:

- **Quick compose** — default from Inbox and reply contexts: recipients,
  optional subject, body, drag-in attachments. Minimal chrome.
- **Delivery composer** — default from Deliveries/Studio: template
  picker, branded layout, gallery arrangement, cover, per-object
  captions, the Job tag and window controls.

"Turn into delivery page" escalates a quick draft losslessly. **Replies
default subject-less**: `subject` is optional (D-49) and the thread
supplies context — the protocol-native end of "Re: Re: Re:". Threading
rides `in_reply_to` (content address of the parent, D-49) set
automatically.

## 2. Profile-native authoring (D-134)

The editor's output *is* `mlp-html/1` — it offers only what §11 renders:
the allowlisted elements, the D-93 style subset (hex/named colors,
unquoted families, flex layout), URN-only embeds. Nothing the sanitizer
would strip can be authored, so WYSIWYG is honest by construction.

**The invariant, asserted:** before signing, the client runs the
reference sanitization over its own output and requires
`sanitize(body) == body` (tree equality, D-94) plus §3.2.3 Manifest
completeness. A failed assertion blocks dispatch and files a bug — no
editor defect ever becomes a signed artifact. (The TV-005 machinery,
promoted to a compose-time conscience.)

**Templates** are profile-conformant Body skeletons — header with logo,
intro, gallery grid, footer with contact and the availability line. No
variable engine in v1 (backlog). **Branding assets are ordinary Media
objects**: Petra's logo is a Manifest entry like any photo — which means
it rides the dedup economy and transfers to each recipient domain
exactly once, ever (`have` thereafter). Brand-heavy senders get faster
over time for free.

## 3. The media pipeline (D-135)

Per dropped file, in order:

1. **Hash first**: streaming BLAKE3 in WASM (D-116) computes the
   `urn:mlet:` address while reading — identity exists before any byte
   moves.
2. **Have-check** against the sender's own store: present → **attach by
   reference** instantly, zero upload ("already in your store" — the
   compose-time dedup moment).
3. Absent → **background upload** to the selected store — the D-105
   selector, defaulting from the sender's D-107 routing rules (job
   label → store; type → store), per-file override available. The
   reference implementation reuses the tus machinery intra-domain
   (D-79 freedom; one code path, Stage 4 note).
4. **Preview pairing**: images gain a scaled preview (~200 KB target);
   videos gain a poster frame — generated client-side, uploaded as
   *separate small Media objects*. The Body embeds the preview and
   links the master; the pair is one user gesture, two Manifest
   entries, managed invisibly. (Small-object auto-grant policy for the
   recipient side: parked to S3.4 per S3.1 §8.1.)

**Dispatch gates on possession**: sending is enabled only when every
Manifest object is verified `live` in the sender's own store — promises
you can keep, the D-84 custody principle applied to authorship. Until
then the draft sits in **"Send when uploads complete"**, an explicit,
cancelable state.

## 4. Cap-aware composition (D-136)

Live meters in the delivery composer chrome:
**`Envelope 118 KB / 256 KiB · Objects 42 / 256`** (D-20/D-52).

The meters exist because scale collides with frozen structure: a
214-photo shoot with preview pairing wants 428 Manifest entries —
**over the 256 cap** — and a 214-figure Body presses the envelope
limit. The caps are anti-abuse load-bearing (D-15/D-20); the composer
designs within them:

- **The curated-delivery pattern** (in-cap default, suggested
  automatically as meters climb): top-N highlight pairs inline
  (N ≈ 20–40) + the full set as archive objects ("All 214 edited JPEGs
  — 2.1 GB", "RAW masters — 48 GB"). This matches professional
  delivery practice and keeps envelopes small.
- **Per-photo browsable galleries at scale** are registered in the MEP
  queue (cap analysis / a collection-object concept) — a design that
  *assumed* a cap raise would be scope creep against frozen decisions;
  a design that documents the pressure is D-38 working.

## 5. Recipients and windows (D-137)

**Recipient chips**: as addresses are entered, SN-mediated resolve
(§5.6) returns a checkmark and display name; display-safety rules
(spec §4.4) applied inside chips. An unresolvable address does not
error — the chip shows the **guest indicator** ("via secure link +
email", Annex A/D-36) and the delivery proceeds seamlessly down the
guest path. **Bcc honesty**: the field carries its mechanism as a
tooltip — "each Bcc recipient receives a private copy; recipients never
see this list" (D-03 made legible). The composer derives
`displayed_to`/`displayed_cc` from To/Cc; Bcc appears nowhere in the
signed artifact, automatically.

**Availability windows**: a per-delivery default (client setting; 30
days initial) with per-object override (masters 30 d, previews 90 d).
The default footer template surfaces it recipient-visibly — "Files
available until August 3" — the D-35 insight that the retention promise
is a *feature*, stated where the client sees it.

**Job tag** (D-123): prominent in delivery mode, autocompleting from
existing labels; sets the tagged author address and pre-labels the sent
thread.

## 6. Draft lifecycle (D-138)

```
composing ──▶ uploads pending ──▶ ready ──▶ (undo hold, 10 s) ──▶
signing ──▶ dispatching (per domain) ──▶ negotiated  →  Deliveries lens
```

Drafts autosave to the home server (a draft is unsigned medialet JSON +
upload state); composing offline queues locally and syncs (posture
detailed in S3.9). **Undo send**: a 10-second client-side hold between
"Send" and dispatch — cheap, expected, and the last moment anything can
be unsaid, since what follows is a signature (D-02). **Pre-flight**, run
silently and surfaced as a single ready state rather than a checklist:
profile conformance (§2 invariant), Manifest completeness (§3.2.3),
caps (§4), recipients resolved-or-guest, all objects verified in store.
Signing is the SN's act (D-13) and instant; dispatch fans out per
target domain (§3.4 of the spec); verdicts stream back into the
Deliveries lens (S3.5).

## 7. Attach-from-library (D-125, realized)

The composer's second ingestion door: picking objects from the Media
surface (or "Compose with these" from any gallery) builds Manifest
entries from reference metadata (name/size/type retained per D-53/D-87)
with zero re-upload — the mechanism behind the Nováks' day-40
new-compose path (D-124) and behind every re-delivery a professional
ever makes.

## 8. Open questions parked

1. Template variable engine ({client_name}) — backlog, S3.11.
2. Video preview clips (beyond poster frames) — S3.7 with media
   handling.
3. Per-photo gallery scaling — MEP queue (§4).

## 9. Traceability (D-38)

| Element | Traces to |
|---|---|
| Two modes, subject-less replies | D-133; D-49 optional subject |
| Profile-native editor + assert | D-31/D-91–94; TV-005; D-134 |
| Logo-as-Media dedup | D-16 `have`; D-35 branding; D-134 |
| Hash-first, have-check, store selector | D-116; D-105–107; D-135 |
| Preview pairing | Tier-2 UX (D-19); S3.1 §8.1; D-135 |
| Dispatch gates on possession | D-84 principle; D-135 |
| Cap meters, curated delivery | D-15/D-20/D-52; D-136 |
| Resolve chips, guest fallback | §5.6; D-36/Annex A; D-137 |
| Bcc honesty | D-03/D-04; D-137 |
| Window surfacing | D-19/D-35; D-137 |
| Undo hold, pre-flight | D-02 immutability; D-138 |
| Attach-from-library | D-124/D-125 |

---

*Next: S3.4 — the receive and accept flow: the delivery page as the
recipient sees it, the accept affordance with D-98 transparency
("download from sender"), deadline and tombstone rendering, pin UX,
and the parked policy question decided — the small-object auto-grant
threshold with its cumulative per-envelope budget.*
