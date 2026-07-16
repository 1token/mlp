# MEP-004: The mailing-list profile (and the profile series)

| | |
|---|---|
| **Status** | Draft |
| **Type** | Additive (companion document; zero core-spec text changes) |
| **Filed** | 2026-07-16 · **Decided** | — |
| **Affects** | Establishes `spec/profiles/` (independently versioned); core §14 gains at most reserved metadata member names if the profile needs them |
| **Origin** | D-260 — S4.17's finding that the working-group exploder required *nothing* from the protocol |

## Motivation

S4.17 built an IETF-style working-group list across five domains
(`TestScenarioWorkingGroupExploder`) and its deepest finding was a
negative: the exploder needed **zero** core-spec changes. Roster and
re-dispatch live at the application layer; every action is pure
§3.4.2 re-dispatch with `automatic=true`; D-51 chain-member
semantics prevent loops (the boomerang provably arrives, its
re-explosion is refused); the tier system is the moderation queue;
§9.3 delegation keeps heavy media off the list server; D-110 threads
across re-dispatch; D-04 envelope privacy holds throughout.

What is missing is not protocol — it is *convention*. Two
independently written list servers today would interoperate at the
wire level and still diverge on everything a subscriber experiences:
how a list identifies itself, how unsubscribe works, what a
moderation refusal looks like. Email solved this exactly once and
correctly: SMTP's core stayed in RFC 5321/5322 while list behavior
became RFC 2919 (List-Id) and RFC 2369 (List-\* headers) — layered
documents, separately evolved. MLP should copy the shape, not just
the lesson.

## Specification change

None to core text. This MEP establishes:

1. **The profile series.** A new document class at `spec/profiles/`,
   licensed and versioned independently of the core (own draft
   numbers), each stating the core draft it was written against.
   Profiles are **normative for participants that claim them** and
   invisible to everyone else — a profile MUST NOT impose behavior
   on non-participating nodes.

2. **Its first document,** `spec/profiles/mailing-list-draft-01.md`,
   to be written on acceptance, covering: list identity (a stable
   list address plus a `list` metadata member naming the list and
   linking its policy page), subscription and unsubscribe
   conventions (plain Medialets to the list address; no silent
   administrative channels), re-dispatch requirements (always
   §3.4.2 with `automatic=true`; original author signature always
   preserved), loop conduct (D-51 refusal is terminal — a list MUST
   NOT retry a refused re-explosion), moderation surface (tier
   verdicts are the queue; refusals are visible to the sender as
   ordinary dispatch outcomes, never silent drops), and heavy-media
   conduct (delegated mode per §9.3; the list server SHOULD NOT take
   custody).

3. **Registry touch, if any.** If the `list` metadata member is
   adopted, its name is reserved in §14 as a one-line registry
   addition — the only core edit this MEP could ever produce.

## Semantics

The profile binds claimants only. A list server claiming the profile
MUST satisfy its requirements to describe itself as
MLP-mailing-list-conformant; ordinary nodes interact with it as with
any correspondent, no special handling anywhere.

## Compatibility

Trivially total: non-claimants see standard MLP traffic. A draft-02
core node and a hypothetical draft-05 core node can both claim the
same profile version. Unknown metadata members (the `list` member to
recipients that predate it) fall under existing D-43 tolerance.

## Security & privacy considerations

The profile codifies what S4.17 proved safe: envelope privacy is
preserved because every re-dispatch mints fresh envelopes (members
never see each other's delivery details unless the author displayed
them); the roster lives only on the list server; loop prevention is
cryptographically anchored in the hop chain, not in mutable headers.
The profile adds requirements, not capabilities — its security
section will consist of MUSTs that forbid the classic list-server
leaks (roster disclosure, silent drops, custody hoarding).

## Conformance impact

Acceptance requires the profile document itself plus its test
anchor: `TestScenarioWorkingGroupExploder` is cited as the reference
implementation and gains assertions for any convention the profile
adds beyond current behavior (the `list` member, refusal visibility).
No new byte vectors — profile conformance is behavioral, tested over
sockets in the scenario suite.

## Editor decision

*Pending.*
