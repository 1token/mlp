# User stories — the Medialet Protocol in five lives

Every story below is implemented and tested; citations point at the
decision register (D-nn), the specification (§n), and the test that
keeps the story true. The cast: **Petra** (author/sender), **Novák**
(reader/recipient), **Friend** (guest — a reader with no account),
**Olga** (operator of a domain), and **Ivan** (implementer of an
independent node).

## Petra — the author

Petra is an independent photographer. Her product is tens of
gigabytes per client, and her tools are email (which cannot carry
it), WeTransfer-style links (which expire, track, and speak nothing
of her identity), and cloud drives (which make her clients create
accounts and make her manage permissions forever).

**P1. Send a shoot like an email.** Petra writes a subject and a
note, attaches the files, and sends to `novak@target.example`. The
files are hashed *in her browser* before a byte moves (D-135,
D-244); anything her domain already holds is attached instantly
without re-upload ("already in your store, nothing uploaded"), and
uploads resume from the server's confirmed offset if her connection
dies mid-way (§8; `TestTwoDomainDemo`). Her authorship is
cryptographically hers: the Medialet is signed with her domain's
author key, and no relay can alter what she wrote (§3.3).

**P2. Promise, don't push.** Sending moves no payload. Novák's
domain answers with a signed verdict; large files wait for his
explicit accept, small previews flow at once (D-139). Petra's
delivery view shows protocol facts — dispatched, granted, deferred,
downloaded — never surveillance ("opens" do not exist as a concept;
D-147).

**P3. Reach anyone, account or not.** For a client with no MLP
address anywhere, Petra names a guest (D-151, D-237). The guest gets
a capability link; Petra gets a 6-digit PIN to convey through her
own second channel — a text message, a phone call (D-152, D-238).
The notification email carries the link and nothing else: no
tracker, no PIN, nothing phishable (D-153).

**P4. A job, not a message.** The delivery carries her job tag
("novak wedding"); her deliveries lens groups by it, and the
timeline is her paper trail per client (D-149).

## Novák — the reader

**N1. An inbox that respects strangers cautiously and friends
fully.** Petra's first delivery arrives as Tier 2 — a stranger. The
note and the small preview render immediately (auto-granted within
the D-139 budget); the 40 GB master sits as an offer describing
itself (name, size, type, deadline) until Novák accepts. Nothing
heavy lands uninvited. Once Novák has ever written to Petra, she is
a correspondent (D-162, D-246), and her resends of media he already
holds answer `have` — accepted instantly, zero bytes moved
(§7.5, D-241).

**N2. Accept is the only ceremony.** One tap accepts the master; the
transfer is his domain's business, resumable across failures, and
the object is verified by its content address before it is ever
"available" (§8). If the object is already local — a resend, a
same-domain delivery, a claimed guest package — the accept completes
on the spot (`instant: true`).

**N3. Read safely.** The Body renders through a sanitizer proven on
a shared 14-case corpus by *two independent implementations* and
re-sanitized at render time inside an isolated DOM (§11, D-31,
TV-005). Junk from strangers can be released (the sender becomes
allowed — outranking any classifier) or blocked, with the record
kept (D-165).

**N4. His storage, his rules.** The media library shows everything
he holds or was offered, foldable previews under masters (MEP-002).
Pinning retains absolutely (§10.5 invariant 1 — a MUST with a
test); deleting is his right even for pinned objects (D-88); what
policy auto-granted, policy may reclaim when nothing pins it
(D-139, D-251).

## Friend — the guest

**G1. Click, PIN, see.** The link opens a delivery page — same
sanitizer, same viewer as the inbox (one Body viewer, two hosts;
D-151). The PIN arrived separately from Petra herself. Five wrong
attempts lock the link before anything leaks (D-155, D-238); views
are recorded nowhere; downloads are honest facts on Petra's timeline
(D-147, D-239).

**G2. Keep it — become someone.** One field ("pick a name") turns
possession of link + PIN into a mailbox at the hosting domain
(D-154, D-240). The delivery re-dispatches through the real ingest —
Petra's signature intact — and the files are available the instant
they are accepted, because the bytes never had to move
(instant-have). The old link keeps working. A passkey ceremony
(D-161) follows immediately; a password remains only a fallback.

## Olga — the operator

**O1. One process, one domain.** `mlpd -domain example.org -data
/var/lib/mlpd` behind any TLS proxy is a complete node: federation,
storage, client (docs/OPERATOR.md). The domain key is generated on
first run; the Domain Document serves itself.

**O2. Sleep at night.** Discovery is hardened by specification —
HTTPS-only, size-capped, redirect-budgeted, private ranges refused
at dial time (§5.4, all failing-input tested). The demo relaxations
exist behind an explicit flag that logs `DEMO MODE`; production
composition cannot reach them (D-247). Backups are one directory;
key rotation is additive (§5.5).

**O3. Policy is hers.** The auto-grant knob, delegation budgets, and
standard-class reclamation under quota pressure are operator
territory by explicit spec latitude (§7.7, §10.5) — with the
invariants (pins retain; tombstones are honest) enforced in code
above her.

## Ivan — the implementer

**I1. Build from the spec, verify against the vectors.** Seven
frozen test vectors regenerate byte-identically from committed
generators; the sanitizer corpus is already passed by two
implementations; every MUST in the spec maps to a covering test or a
named gap (`conformance/MUST-AUDIT.md`, D-104). Ivan's node either
reproduces the bytes or it does not — no interop archaeology.

**I2. Extend without forking.** The MEP process has already run a
full cycle twice over: four proposals filed, decided, rolled into draft-02 and draft-03 with
new vectors, old vectors untouched, old receivers unharmed
(unknown-member tolerance is itself a tested MUST).
