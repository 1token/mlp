# MEP-003: Bao verified streaming

| | |
|---|---|
| **Status** | Draft |
| **Type** | Additive (negotiated optional capability; no wire-version bump per D-101) |
| **Filed** | 2026-07-16 · **Decided** | — |
| **Affects** | Spec §5 (Domain Document capability advertisement), §8 (transfer), §9 (fulfillment/fetch); one §14 registry token for the capability name |
| **Origin** | D-259 — the S4.19 editor session on the post-draft-02 roadmap |

## Motivation

MLP identifies heavy media by BLAKE3 root hash (`urn:mlet:`, D-25),
and BLAKE3 is already a Merkle tree: the root every URN carries
commits to the entire chunk tree, not merely to the whole-object
digest. Today the protocol uses that commitment only at the §8.4
checkpoint — a recipient verifies after holding all the bytes. For
Petra's clients that means: a corrupt or hostile 2 GB transfer is
discovered at byte 2 G, not at the first bad chunk; a video cannot be
honestly previewed while it downloads; and a ranged fetch (seeking)
cannot be verified at all.

Bao (the BLAKE3 verified-streaming encoding) closes all three gaps
using the tree the URN already commits to. **No identifier changes:**
every `urn:mlet:` ever minted can be bao-verified as-is.

## Specification change

1. **Capability.** A Domain Document (§5) MAY advertise the
   capability token `bao-stream/1` per role. The token is added to
   the §14 capability registry.

2. **Verified fetch.** A fetch request (§9) to a bao-advertising
   endpoint MAY carry `Accept: application/mlp-bao` and an OPTIONAL
   byte range expressed in **chunk-group units**. The response body
   is the bao *combined slice encoding* of the requested range:
   interleaved tree nodes and content chunks sufficient to verify the
   slice against the URN's root hash.

3. **Verified push.** A push (§8) to a bao-advertising BS MAY use
   the same encoding per PATCH; the §8.4 whole-object checkpoint
   remains REQUIRED and unchanged (bao verification is an additional,
   earlier rejection point, not a replacement).

4. **Chunk-group size.** Fixed spec-wide at **16 KiB** (16 × the
   BLAKE3 1 KiB leaf) — the interoperability pin that makes slice
   boundaries identical across implementations. (Value to be
   confirmed against reference-implementation benchmarks before
   acceptance; whatever is chosen is frozen in the spec, never
   negotiated.)

5. **Outboard trees are private.** The bao tree is deterministically
   derivable from content by any holder. It never appears in a
   Manifest, envelope, or registry; whether a node precomputes and
   stores outboard trees or streams them on demand is an
   implementation choice.

## Semantics

Verification per slice: the receiver checks each chunk group against
its parent path up to the URN root as it arrives, rejecting the
transfer at the first non-verifying group with the existing §7.8
problem machinery (one new registry token, `bao-verify-failed`).
Whole-object semantics (custody, §8.4 `verified_at`, refs
transitions) are untouched: a bao-fetched slice confers no custody;
a bao push still goes live only at the checkpoint.

## Compatibility

Fully negotiated: a domain that does not advertise `bao-stream/1` is
never sent bao requests; a client that does not request
`application/mlp-bao` receives plain bytes exactly as today. Old
receiver + new sender and new receiver + old sender both degrade to
draft-02 behavior with zero loss. D-43 unknown-member tolerance is
not even exercised — nothing new appears in signed documents.

## Security & privacy considerations

Strictly narrows the attack surface: hostile bytes are rejected at
the first bad 16 KiB instead of after a full transfer (resource-
exhaustion mitigation), and ranged reads gain integrity they
currently lack. No new metadata crosses domains: the capability
token is public routing information of the same sensitivity as the
§5 endpoints themselves. Slice requests reveal access patterns
(which ranges a client reads) to the fulfilling node — the same
information plain HTTP range requests already reveal.

## Conformance impact

Acceptance requires: **TV-008** — a fixed small object, its bao
combined encoding, one valid slice (offset, length, encoded bytes,
expected verification success) and one corrupted slice (expected
`bao-verify-failed`), byte-identical from a Python generator like
TV-001–007; plus a MUST-audit row for the checkpoint-remains-REQUIRED
clause. Implementation note (informative): the reference server's
current BLAKE3 library (`zeebo/blake3`) does not expose bao
encode/decode; `lukechampine.com/blake3` does — the implementation
substage will add or swap that dependency.

## Editor decision

*Pending.*
