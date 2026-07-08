# MEP-002: `preview_of` Manifest member

| | |
|---|---|
| **Status** | Draft (filed) |
| **Type** | Additive (optional member; no wire-version bump per D-101) |
| **Filed** | 2026-07-06 |
| **Affects** | Spec §3.2.2 (Manifest Entries), §11.4 note; no registry changes |
| **Origin** | D-158 — the pairing gap found in Stage 3 media-library design |

## Motivation

The composer pairs masters with small previews as separate Manifest
entries (D-135), and recipient policy auto-grants the small ones
(D-139) so first-contact delivery pages render alive. But **the pairing
is authored knowledge with no structural representation**: recipient
clients currently infer it from Body markup (a figure containing the
preview image and the master link — the documented D-158 heuristic).
Consequences of inference failing: a recipient pins a 200 KB preview
believing the master is saved; libraries show two unrelated cards for
one logical asset.

## Specification change

§3.2.2, Manifest Entry gains one OPTIONAL member:

> `preview_of` — string, OPTIONAL. The `urn` of another entry **in the
> same Manifest** for which this entry is a reduced-fidelity preview.
> Constraints: the referenced `urn` MUST be present in the Manifest;
> an entry carrying `preview_of` MUST NOT itself be the target of any
> `preview_of` (no chains); self-reference is forbidden. A violating
> member is ignored at ingest validation (the entry otherwise stands).

## Semantics

Purely descriptive metadata — a display and organization hint, never a
policy input (the D-111/D-107 constraint inherited verbatim:
sender-declared members do not drive routing, acceptance, or store
selection; auto-grant policy continues to key on `size` alone, D-139).
Recipient clients MAY: fold preview and master into one library card
with the preview as its pre-accept face (D-156/D-158); mark rendered
previews as such; and redirect a pin gesture on a preview toward the
master with the D-158 nudge.

## Compatibility

Old receivers ignore the member and retain the figure heuristic — the
v1 behavior, unchanged. Old senders never emit it; new receivers keep
the heuristic as fallback for their sake. The heuristic remains
documented until 1.0 usage data says otherwise.

## Security & privacy considerations

None new: the member names a relationship between two objects already
declared in the same signed Manifest. Explicitly not a policy surface
(above).

## Conformance impact

Manifest-validation cases: valid pair; dangling target; chain;
self-reference (member-ignored outcomes); a TV-005-style client case
for card folding may accompany the reference client.

## Editor decision

*Pending.*
