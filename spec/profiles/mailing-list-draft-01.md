# MLP Profile: Mailing Lists — draft-01

| | |
|---|---|
| **Series** | `spec/profiles/` — established by MEP-004 (D-260, D-270): independently versioned companion documents, **normative for participants that claim them**, invisible to everyone else |
| **Core relation** | Written against MLP/0.1 **draft-03**; uses the §3.4.1 `list` member and nothing else that is new — every mechanism below existed at draft-01 of the core |
| **Status** | draft-01, accepted with MEP-004 (2026-07-17) |
| **License** | CC-BY 4.0 |
| **Reference implementation** | `server/cmd/mlpd/scenarios_wg_test.go` — `TestScenarioWorkingGroupExploder` (S4.17), five domains over real sockets |

A **list** here is an exploder: an application that receives a
Medialet at a list Address and re-dispatches it to a roster. The
deepest fact about this profile is what it does *not* contain: S4.17
proved the exploder needs nothing from the protocol. The roster is
application data (as with mailman on SMTP); every action below is
ordinary MLP. This document only pins the conventions that make two
independently written lists feel the same to their subscribers —
email's RFC 2919/2369 move, copied deliberately.

Requirement language per core §2.1. "The list" means a server
claiming this profile; requirements bind claimants only (a profile
MUST NOT impose behavior on non-participating nodes — MEP-004).

## 1. Identity

1.1. A list has exactly one stable Address (core §4) — the list
Address. Posts, subscribe requests, and unsubscribe requests are all
plain Medialets to this Address; the list MUST NOT require any
side channel for membership actions.

1.2. Every re-dispatched Envelope MUST carry the §3.4.1 `list`
member, set to the list Address. This is the subscriber's stable
"via the list" signal — clients thread and badge on it — and it is
hop-signed like every Envelope member: the dispatch is the list's
own act, attested by the list domain's `sn` key.

1.3. The list SHOULD publish a policy page (membership rules,
moderation policy, archive policy) and SHOULD name it in the
Medialet body of its subscription confirmations.

## 2. Subscription and unsubscription

2.1. Subscribe and unsubscribe are Medialets from the member's own
Address to the list Address. The list MUST honor an unsubscribe
request from the subscribed Address without further ceremony, and
MUST confirm both actions with a Medialet to the requester.

2.2. The roster is list-private application data. The list MUST NOT
disclose the roster in any protocol-visible artifact: not in
`envelope_to` beyond each member's own domain-partitioned dispatch
(core D-03/D-04 already forces this shape), not in Medialet bodies
sent to third parties, not via any enumeration surface.

## 3. Re-dispatch

3.1. Every explosion is a §3.4.2 re-dispatch with `automatic=true` —
an exploder IS automation, and that honesty is what arms D-51 for
the whole federation (D-256). The list MUST NOT re-originate: the
author's Signed Medialet travels unchanged, author signature and
content address intact, so authorship and threading (D-110) survive
the list at every subscriber.

3.2. The list MUST set `forwarded_by` to the list Address (it is the
mailbox whose action caused the dispatch) alongside the `list`
member of 1.2.

## 4. Loops

4.1. A D-51 refusal is terminal: when the list's own domain already
appears in the hop chain, the automatic re-explosion MUST NOT
proceed and MUST NOT be retried for that Envelope. The boomerang's
*arrival* is legal and expected (delivery records may show it); its
re-explosion is what dies. One revolution, never two — the S4.17
scenario asserts exactly this.

## 5. Moderation

5.1. The tier system (core §7.7) is the moderation queue: a
quarantined post awaits a moderator's release; release is approval;
block is removal. The list MUST NOT silently drop a post — a sender
whose post is not exploded sees the ordinary dispatch outcome
(`quarantined`, `rejected:policy`), the same sender-visible honesty
the core already provides (D-163/D-165).

## 6. Heavy media

6.1. The list re-dispatches in Delegated mode (core §9.3) by
default: `fulfillment_sources` names the author's domain (and any
custody holders it knows), and the list server SHOULD NOT take
custody of media it merely explodes — heavy bytes flow
point-to-point from the author to accepting members. S4.17 asserts
`objectLive(lists) == false` while every accepting member holds the
draft.

6.2. A list MAY additionally operate an archive mailbox that accepts
custody and re-offers under its own MEP-001 `until` window — the
already-proven custody-forwarding composition; nothing new.

## 7. Security and privacy

The classic list-server leaks, forbidden above by requirement rather
than hope: roster disclosure (2.2), silent drops (5.1), custody
hoarding (6.1). Envelope privacy needs no help — every explosion
mints fresh per-domain Envelopes, so members never see each other's
delivery details unless the author displayed them (core D-04), and
Hop Attestations exclude recipient sets by construction (§3.4.2).
Loop safety is cryptographically anchored in the hop chain, not in
mutable headers (D-51).

## 8. Conformance

A claimant conforms when it satisfies every MUST above.
`TestScenarioWorkingGroupExploder` is the reference walk: post,
fan-out with envelope-privacy assertions, delegated heavy media,
threading across the exploder, tier moderation, and the
one-revolution loop. The `list`-member emission (1.2, 3.2) postdates
the S4.17 scenario and lands with the implementation substage that
wires it, alongside its scenario assertion (D-270 acceptance note).
