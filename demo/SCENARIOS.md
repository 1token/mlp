# The scenario suite — programmatic demos over real TCP sockets

Thirteen self-contained stories in the `TestTwoDomainDemo` mold: each
boots real `mlpd` nodes on real sockets (the same `buildNode`
production composition, demo-mode peering only), walks one scenario
through the actual client API and federation endpoints, and asserts
the protocol facts that make the story true. Run any one alone:

```sh
cd server
go test ./cmd/mlpd/ -run TestScenarioCustodySurvivesOriginDeath -v
```

or the whole suite:

```sh
go test ./cmd/mlpd/ -run TestScenario -v
```

## The harness (`scenario_harness_test.go`)

`newWorld(t, map[domain]firstUser)` boots N peered domains sharing
one **controllable clock** (`config.Clock`, wired through SN, BS,
Client API, and the pusher). `w.login(addr)`, `w.send(...)`,
`w.accept(...)`, `w.advance(d)`, and `w.pushAll()` are the verbs.
Two postures worth knowing:

- **Time is world-owned.** Every protocol-visible operation advances
  the clock one second. A fully frozen clock makes causally ordered
  verdicts share one RFC 3339 `created`, pushing §7.6 supersession
  onto the verdict_id tiebreak — which real deployments resolve via
  UUIDv7's millisecond ordering, meaningless when the milliseconds
  are frozen too. One second per operation restores the ordering
  reality provides. (Found the honest way: the suite flaked 1-in-4
  until the mechanism was understood.)
- **Forwarding drives `SN.Forward` directly** and dispatches the
  returned envelope over the real socket to the receiving domain's
  `/dispatch`. There is no client API forwarding endpoint yet (client
  backlog); the wire behavior is identical to what one will produce.

## Simple scenarios (`scenarios_basic_test.go`)

| Scenario | Domains | What it proves | Anchors |
|---|---|---|---|
| `TestScenarioSameDomainDelivery` | 1 | Two mailboxes, one roof: dispatch loops through the node's own SN (the real ingest path), and accept completes instantly — the bytes never move | D-241, D-240 |
| `TestScenarioConversationLifecycle` | 2 | A 3-message back-and-forth threads into ONE topic per side; read/flag/done verbs hold | D-110 |
| `TestScenarioAttachByReference` | 2 | Declaring held content answers `have: true` — no second upload resource, no bytes re-sent | D-135 |
| `TestScenarioTierLifecycle` | 2 | Block: the sender's record sweeps to junk, the next delivery arrives `quarantined` with media never granted. Release: the allow override outranks the stranger posture and the inbox reopens | D-162, D-163, D-165 |
| `TestScenarioMultiRecipientFanout` | 3 | One draft, two domains: one envelope PER DOMAIN, and neither canonical envelope names the other's recipients — Bcc-by-construction, proven on the origin's dispatch records | D-04 |
| `TestScenarioIdempotentSend` | 2 | The same `Idempotency-Key` replayed lands ONE envelope at the target | D-169 |

## Advanced scenarios (`scenarios_advanced_test.go`)

| Scenario | Domains | What it proves | Anchors |
|---|---|---|---|
| `TestScenarioDelegatedForwarding` | 3 | petra → novak → (forward) → carol: the Medialet arrives at the third domain byte-identical under petra's authorship; carol's accept runs §9.3 delegation, the chain answers will-push, and the bytes arrive from the origin | §9.2–9.3, D-84 |
| `TestScenarioCustodySurvivesOriginDeath` | 3 | novak takes the bytes, custody-forwards with an `until` past the author's window, then **petra's domain is killed**. Carol's effective deadline is the custodian's window (MEP-001); her accept fulfills from the custodian honoring the promise he himself hop-signed (§9.5); byte equality verified with the origin a closed socket | MEP-001, §9.5, D-84 |
| `TestScenarioForwardLoopPrevention` | 2 | The envelope walks home (petra → novak → archive@origin); an AUTOMATIC onward dispatch at the chain-member origin is refused; the same forward done deliberately passes — the human may mean it | D-51 |
| `TestScenarioResendAfterDeletion` | 2 | Delete leaves the honest tombstone (`unavailable/deleted`); the resend's verdict is `grant`, not `have` — possession claims stay true even for a correspondent — and the bytes travel a second life | §10.4, §7.5, D-139 |
| `TestScenarioGuestLockAndExpiry` | 1 | Five wrong PINs lock the link; the lock outranks even the CORRECT PIN (checked before evaluation); a fresh link answers 200 today and 410 on day 31 under the moving clock | D-152, D-155, D-238 |
| `TestScenarioEphemeralGC` | 2 | The auto-granted preview (ephemeral class) is collected the moment nothing needs it, tombstoned `expired-local`; the pinned master is untouchable | D-139, D-251, §10.5 |

## The working-group exploder (`scenarios_wg_test.go`)

`TestScenarioWorkingGroupExploder` — an IETF-style WG mailing list
built on MLP primitives, five domains: the list (`medialet-wg@
lists.demo`, whose mailbox owner is the moderator), three members,
and a cross-subscribed archive mirror. The exploder is an
APPLICATION, as mailman is an application on SMTP — MLP defines no
list membership; the roster is the exploder's own data. Everything
the exploder does is pure protocol: §3.4.2 re-dispatch with
`automatic=true`, because an exploder IS automation and must say so.

Seven phases, each an assertion set:

1. **Posting** — a member's post reaches the list like any delivery.
2. **Fan-out** — the exploder re-dispatches per subscriber domain
   (D-04 preserved); authorship and content address arrive
   byte-identical at every subscriber.
3. **Heavy media never touches the list** — subscribers accept
   through §9.3 delegation and the 5 MiB draft flows point-to-point
   from the author's domain; `objectLive(lists.demo) == false` is
   asserted. The structural difference from SMTP exploders and
   their attachment-size limits: the list explodes envelopes, never
   payloads.
4. **Threading across the exploder** — a reply re-distributed by the
   list joins the SAME topic at every subscriber, because the
   content address is the message identity (D-110 over re-dispatch).
5. **Moderation is the tier system** — a blocked troll's next post
   arrives `quarantined`, never enters the inbox, and is therefore
   never exploded; `release` is moderator approval, after which the
   post flows to the group.
6. **The loop dies at one revolution** — the classic
   cross-subscribed-lists misconfiguration: the mirror explodes the
   announcement back at the list (legal — the mirror is not yet in
   the chain), the boomerang PROVABLY arrives (two Delivery Records
   for one Medialet at the list), and the list's automatic
   re-explosion is refused with `ErrForwardLoop` — lists.demo is
   already in the chain (D-51). Every member holds exactly one copy.

Anchors: §3.4.2, §9.3, D-04, D-51, D-110, D-163, D-165.

## What the suite has already earned

Scenario-building surfaced real semantics that narrower tests had
not pinned down over sockets: the §7.6 same-second supersession
corner above; that `block` sweeps the sender's existing record out
of the inbox, not just future deliveries; that a blocked sender's
dispatch outcome reads `quarantined` — policy visible to the sender,
not a bounce; and that D-51's loop signature is *the forwarding
domain already being in the chain*, not the destination.

## Writing the next scenario

Copy the shape: `newWorld` with the domains, tell the story through
`w.send` / `w.accept` / `forwardOverSocket` / `w.advance` /
`w.pushAll`, and assert protocol facts (refs states, verdicts,
stored envelopes) rather than UI shapes. Prefer objects **above**
the D-139 auto-grant line (> 4 MiB) when the story is about accept
ceremonies, and below it when the story is about auto-grant flow —
the line changes which path runs. One scenario, one property.
