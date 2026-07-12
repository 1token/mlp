# Operating an MLP domain (D-180)

One `mlpd` process is one domain: the federation server (dispatch,
fulfillment, verdicts), the Body Store, the Client API, and the web
client. This guide covers running it for real. For the two-domain
local demo, see `demo/DEMO.md` — and note that everything the demo
relaxes is exactly what this guide tells you never to relax.

## What a domain needs

1. **A DNS name with at least one dot**, serving HTTPS on 443. MLP
   discovery is hardened by specification (§5.4): peers fetch your
   Domain Document at
   `https://<domain>/.well-known/medialet.json` over TLS, port 443,
   GET only, 64 KiB cap, ≤3 same-origin HTTPS redirects, private
   address ranges refused at dial time. There is no plain-HTTP
   federation. Terminate TLS in front of mlpd (any reverse proxy);
   mlpd itself speaks HTTP behind it.
2. **A data directory** on a filesystem you back up. It holds
   `mlp.db` (SQLite — the mailboxes, threads, references, dispatch
   records, keys) and `objects/` (content-addressed payload files).
   The two are one logical unit: back them up together. SQLite's
   `.backup` or a filesystem snapshot while mlpd is stopped both
   work; objects are immutable once verified, so file-level rsync of
   `objects/` is safe at any time.
3. **The client directory** (`-client`) if you serve the web client;
   omit it for a headless federation node.

## First run

    mlpd -domain example.org -listen 127.0.0.1:8441 \
         -data /var/lib/mlpd -client /usr/share/mlp/client \
         -init alice -password '<initial password>'

On an empty data directory mlpd generates the domain key (Ed25519,
roles `sn`,`bs`,`author`) and logs its kid; the Domain Document is
served from it automatically. `-init` provisions a first mailbox
with password fallback — passkeys (WebAuthn) can be added from the
client afterwards and are the intended primary credential (D-161).

Put the reverse proxy in front:

- `/.well-known/medialet.json`, `/dispatch`, `/fulfill`, `/verdict`,
  `/ingest/` — the federation surface; forward verbatim. Do NOT
  rewrite paths on `/ingest/`: transfer requests carry RFC 9421
  signatures over the exact request URI (§6.6), and a path rewrite
  breaks them (the demo learned this the hard way).
- `/api/v1/`, `/g/`, and `/` — the client surface; standard
  proxying, cookies pass through.

## Keys

Keys live in the `own_keys` table (kid, 32-byte seed, roles, and an
optional validity window). Rotation is additive: insert the new key,
serve both (the Domain Document lists every entry), let caches turn
over (23-hour ceiling, §5.5), then window the old key out with
`not_after`. Never delete a key that signed material still in
flight. The seed column is the crown jewel of the deployment —
database backups are key backups; treat them accordingly.

## Retention and garbage collection

The §10.5 invariants are enforced in code and tested: a pinned
reference retains its object absolutely; collection atomically
tombstones the references it strands. mlpd's built-in sweep collects
only the **ephemeral** class — small media that Tier-2 auto-grant
policy admitted (D-139) and nobody pinned. Standard-class objects
are never collected automatically; reclaiming them under quota
pressure is your call, and the protocol demands only honesty about
the outcome: a source that no longer holds an object answers
`not-available` and recipients degrade to resend (§9.5, D-88).

Sender-side: outbound promises bind you through each object's latest
outstanding `available_until` (which MEP-001 forwarding can extend
by YOUR OWN declaration only). Size your storage for what you
promise.

## The policy knobs

- `sn.AutoGrant` — the D-139 Tier-2 small-media auto-grant. mlpd
  ships it on (≤4 MiB per entry, ≤32 MiB per envelope). The spec
  default is defer-everything; disabling the knob returns to it.
- Delegation budget — per (envelope, urn), default 10 (D-83);
  per-delivery override via the client.

## What never to relax

`-peer` exists for local demonstrations and switches the named
domains onto explicit base URLs with §5.4 hardening off (loopback,
plain HTTP, non-443) plus plain-http reservation targets (§7.5).
mlpd logs `DEMO MODE` when any `-peer` is set. Production
deployments pass no `-peer` flags: discovery is the protocol's, and
every relaxation above is an SSRF or downgrade vector by design.

## Observability

Timeline events (`timeline_events`) are the protocol-fact feed per
delivery (D-149): dispatches, verdicts, guest links, downloads,
claims. `reservations_out` shows transfer state including resumable
checkpoints; a row parked at `pushing` after a crash resumes on the
next loop pass from the receiver's confirmed offset — never
re-sending bytes it already holds (§8.7).
