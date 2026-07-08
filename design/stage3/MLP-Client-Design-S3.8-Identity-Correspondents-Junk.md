# MLP Flagship Client Design — S3.8: Identity, Correspondents & Junk

> **Status.** Stage 3, session 8. Judgment calls D-161–D-165 pending
> editor confirmation. §7 traces per D-38.

---

## 1. Identity: passkey-first (D-161)

The v1 key model is domain-attested (D-13): the server signs on the
user's behalf, so user identity is **account authentication**, not key
custody — which makes WebAuthn the natural primary. Passkeys are a
platform API (no library, no build step — the D-113 ethos extends to
auth), phishing-resistant, and leave no password database to breach.

**Signup**: choose the local part (live D-55 grammar validation,
availability, suggestions) → create a passkey. A **password fallback**
exists but is de-emphasized ("other options"), with settings nudging
fallback users toward passkeys later. Adding a **second passkey**
(another device) is encouraged immediately after signup — the cheapest
recovery insurance there is.

**Recovery**, layered:

1. **Recovery codes** — mandatory generation at signup, one-time,
   printable; the user is walked through saving them.
2. **Recovery email** — optional; and here the funnel pays out: a
   **claim-originated signup (D-154) begins from a notification email
   the recipient demonstrably received** — verified by receipt. With a
   consent checkbox, that address seeds recovery at creation. The
   common onboarding path arrives with its recovery story pre-solved.
3. **Multiple passkeys** as the everyday layer.

No SMS (SIM-swap exposure, and infrastructure an indie EU project
should not run). Operator-mediated manual recovery is operator policy,
outside client scope, noted as existing. **Sessions**: a device list
in Settings with per-session revoke and "sign out everywhere." The
last authentication method cannot be removed.

## 2. The Correspondents surface (D-162)

The protocol's tiers (D-19) rendered as legible relationships. A
correspondent page shows:

- **Header**: display name + address under full §4.4 display-safety.
- **Relationship, with its reason**: "Trusted correspondent — because
  you sent them a Medialet on June 3" (the D-19 correspondent
  definition, mailbox-key comparison per D-55, made inspectable);
  or "First contact"; or "Blocked."
- **The allowlist as human language**: *"Always accept files from
  Petra"* — Tier 1 membership, auto-grant within quota. Its inverse,
  *"Ask me first,"* demotes a correspondent to defer-by-default —
  a per-correspondent override squarely inside D-19's declared
  configurability.
- **Store routing**: "Files from Dr. Nováková → Health store" — a rule
  row rendered here, writing to the same D-160 rules table (one rules
  system, two doors).
- **History**: threads and media exchanged as pre-filtered Inbox and
  Library views (the D-157 From facet).
- **Block** (§3).

## 3. Blocking and the quarantine-disclosure trade (D-163)

**Block = synchronous `rejected` with reason `policy`** — the
email-550 posture: honest, immediate, unambiguous. No void that
pretends to be a mailbox.

**The trade this session surfaces**: the frozen vocabulary returns
`quarantined` to origins (D-16/D-70). For legitimate senders that is
useful signal (Petra learns to say "check your junk folder"); for
spammers it is classifier feedback — the loop email deliberately
denies. The flagship's posture: **default disclosure** (ecosystem
honesty, and MLP's economics already deny spammers the payload
bandwidth that makes probing profitable), with an **operator option to
answer `accepted` for junk-delivered mail** — semantically defensible
(quarantine *is* delivery, to the junk folder) and within the
operator's verdict-policy latitude. The trade is documented in the
operator guide rather than discovered by incident.

## 4. Display-safety as daily practice (D-164)

The spec's §4.4 rules applied at every surface an address appears:
inbox rows, thread headers, recipient chips (D-137), correspondent
pages. Non-Tier-1 addresses get confusable/mixed-script detection with
punycode reveal and a warning chip; display names matching an
address-pattern that differs from the real `addr` render the real
address prominently with caution UI.

**Copy discipline**: the quiet ✓ reads **"signature verified"** (D-32
surfaced) — never "safe," never "trusted sender." Transport
authenticity and content trustworthiness are different claims, and the
product's vocabulary keeps them apart everywhere, including in the
first-contact banner (D-140).

## 5. The Junk surface (D-165)

- **The cheapness banner**, front and center: "Junk holds no files —
  these messages weigh kilobytes" (D-15/D-19 as visible product truth;
  the terabyte junk folder is structurally impossible, and the UI says
  so).
- **Maximum restraint rendering**: junk items display their **derived
  text (§11.6) by default**, with "show formatted" as opt-in — an
  extra layer of caution atop sanitization, and faster to triage
  besides.
- **Rescue** ("Not junk"): moves to Inbox and triggers the
  deferred-upgrade path exactly as a Tier-2 accept (D-130 wiring),
  with an allowlist prompt ("Always accept files from this sender?")
  closing the loop into §2.
- **Mark as junk** (from Inbox): quarantines, feeds the operator's
  classifier hook (D-21), and offers Block.
- **Auto-purge at 30 days** — they were only kilobytes.

## 6. Open questions parked

1. Correspondent import/export (vCard-ish) — S3.11 backlog.
2. Organization-level correspondents (a domain as a correspondent) —
   S3.11.
3. Operator guide text for the §3 disclosure option — Stage 4
   documentation.

## 7. Traceability (D-38)

| Element | Traces to |
|---|---|
| Account-auth not key custody | D-13; D-161 |
| Passkeys as platform API | D-113 ethos; D-161 |
| Claim-seeded recovery email | D-154 funnel; D-161 |
| Tier legibility with reasons | D-19; D-55; D-162 |
| Allowlist / ask-me-first overrides | D-19 configurability; D-162 |
| Routing rule rows shared | D-107; D-160; D-162 |
| Block as rejected:policy | D-16/D-70 vocabulary; D-163 |
| Quarantine-disclosure option | D-16/D-70; operator latitude; D-163 |
| Address safety everywhere | spec §4.4; D-14; D-164 |
| "Verified" ≠ "safe" copy rule | D-32; D-164 |
| Junk cheapness, restraint, rescue | D-15/D-19; §7.7; D-130; D-165 |
| Classifier feedback hook | D-21; D-165 |

---

*Next: S3.9 — client architecture: the Web Components structure over
the D-114 store, the render-form pipeline through the D-115 Body
viewer, offline posture, SSE wiring (D-132), and the session's main
deliverable — the client↔home-server API companion draft, gathering
every requirement registered along the way (rollups, mutations with
undo tokens, uploads, resolution, thumbnails, drafts, rules) into the
document D-68 anticipated.*
