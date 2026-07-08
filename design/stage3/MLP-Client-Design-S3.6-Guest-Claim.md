# MLP Flagship Client Design — S3.6: Guest Delivery & Claim Conversion

> **Status.** Stage 3, session 6. Judgment calls D-151–D-155 pending
> editor confirmation. Renders Annex A (D-102) as product; every guest
> delivery is an adoption funnel with a satisfied recipient at the
> moment of maximum goodwill (D-36). §7 traces per D-38.

---

## 1. The guest page (D-151)

Served by the sender's own node — the same Go binary, `go:embed`, no
build step (D-113) — as a minimal standalone page: no app shell, no
login. Composition, top to bottom:

1. **Sender header**: display name + Medialet address (display-safety
   per spec §4.4), "delivered via Medialet."
2. **The delivery page**: rendered by **the same Body-viewer component
   the app uses** (D-115 realized — one shadow-DOM component, two
   hosts), consuming the sanitized render form under the full §11.7
   floor. Guests get the identical, identically-safe rendering; there
   is no second, lesser renderer to diverge or to exploit.
3. **The Files panel** (shared component family): per-object rows —
   name, size, type, thumbnail where a preview object exists — each a
   direct HTTPS download from the sender's BS with Range support
   (resumable by the browser). No accept economy exists here: Annex A
   federates nothing, so objects are simply *downloadable while the
   link lives*. "Download all" in v1 means the author-attached archive
   objects (the D-136 curated pattern); on-the-fly server bundling is
   backlogged (S3.11).
4. **The claim banner** (§4).
5. **Footer**: the expiry, plainly — "This link is available until
   August 3."

After expiry or revocation: an honest minimal page — "This delivery
link has expired. Contact the sender for a new one" — never a broken
page, never a silent 404 for a once-valid link.

## 2. PIN and expiry (D-152)

**Sender side**: Settings hold guest defaults (PIN on/off, link-expiry
default, email language); the composer and delivery detail override
per delivery. PINs are auto-generated 6-digit codes with the
two-channel nudge at creation: *"Share the PIN by phone or text — not
in the same email as the link."* Link expiry is bounded by the
delivery's `available-until` (Annex A alignment); extending a link
beyond it routes through Extend (D-122). Re-issuing means a new token;
revocation is immediate (D-147 actions).

**Guest side**: the PIN gate is rate-limited with lockout and
`Retry-After`; wrong-PIN messaging is plain ("incorrect PIN") — the
capability holder already possesses the link, so link-validity is not
the secret being defended; the PIN is.

## 3. The notification email (D-153)

The one message Medialet sends over SMTP — composed so it does not
train phishing victims (D-102) and does not betray the product's own
ethos:

- **Context-rich**: sender display name *and* Medialet address; the
  subject; the file list (top entries, names and sizes) and total; the
  expiry date. Enough substance that a recipient can verify with the
  sender out of band — and the email says so: "Expecting this? You can
  confirm with Petra directly."
- **The link, twice**: a button *and* the full URL visible in text.
  Visible URLs train recipients to check domains; button-only emails
  train them to click.
- **PIN discipline**: when a PIN is set, the email says only "the
  sender will share a PIN with you separately" — teaching the
  two-channel pattern by example.
- **Zero tracking**: no external images, no pixels, no per-recipient
  decoration beyond the capability URL itself; `multipart/alternative`
  with a complete plain-text part. Having killed the tracking pixel
  structurally (D-31), we do not smuggle one back in our sole SMTP
  artifact.
- Language: sender-selected per delivery (default: sender's locale) —
  the sender knows the recipient; the node does not.
- Honesty note carried from Annex A: this email travels outside MLP's
  privacy envelope; SMTP metadata semantics apply to it.

## 4. The claim flow (D-154)

The banner: *"Get your permanent Medialet address — keep these files
and receive future deliveries directly."* Two paths from the tap:

- **New address**: the flagship signup (passkey-first; identity design
  detailed in S3.8) with a "claiming a delivery" context ribbon.
- **Existing address**: enter it (double-entry confirmation, §5);
  Discovery resolves it.

Either way the mechanism is the same, and it is the whole point of
D-102's design: **the sender's node re-dispatches the same Signed
Medialet** — immutable, author-signed, federation-ready all along — to
the claimed address as a genuine MLP dispatch.

- **Same-domain claim** (signup at the sender's node — the funnel's
  common case): the objects are already `live` at the domain, so the
  claimant's accepts answer `have` and **the files appear instantly,
  zero bytes transferred** — the protocol performing a small magic
  trick for free at the exact moment a new user forms their first
  impression. The UI leans into it: "Your files are already here."
- **Cross-provider claim**: ordinary federation; the claimant's domain
  applies its own tier policy (first contact → Tier 2 defer; their
  accept follows). No special-case machinery exists anywhere in the
  claim path.
- **The guest link survives the claim** — alive until its expiry or
  sender revocation. Auto-revoking on claim would strand a recipient
  mid-transition to enforce tidiness; instead the sender sees the
  *claimed* status (D-147) and revokes at will.

## 5. Claim anti-abuse (D-155)

Entering a third party's address redirects the delivery to them — a
power equivalent to what the capability holder already has
(download-and-resend), so it is documented rather than feared:
double-entry address confirmation, per-link and per-IP rate limits,
and the sender-visible *claimed* event as the audit trail. The
capability URL remains the security boundary it was designed to be
(≥128-bit, D-102); the claim flow adds no new one.

## 6. Open questions parked

1. On-the-fly download bundling (zip streaming of selected objects) —
   S3.11 backlog.
2. Guest-page localization beyond the email language — S3.10 (i18n).
3. Signup detail (passkeys, recovery) — S3.8.

## 7. Traceability (D-38)

| Element | Traces to |
|---|---|
| One Body viewer, two hosts | D-115; §11.5–11.7; D-151 |
| No accept economy for guests | Annex A "no bytes federate"; D-102/D-151 |
| Same-binary serving | D-113; D-41 single-binary story; D-151 |
| PIN two-channel nudge, rate-limited gate | D-102; D-152 |
| Link expiry ≤ available-until | Annex A; D-19; D-152 |
| Anti-phishing email, zero tracking | D-102; D-31 ethos; D-153 |
| Re-dispatch of the same artifact | D-02 immutability; D-102; D-154 |
| Instant-`have` claim delight | D-16 `have`; D-79; D-154 |
| Link survives claim | D-147; D-154 |
| Redirect residual documented | D-102 capability model; D-155 |

---

*Next: S3.7 — media handling: the Media surface as library (galleries
across partitioned stores, D-105/D-107 facets), the lightbox, media
labels (D-111), pin management at scale, quota surfaces and cleanup
flows, and the preview/derivative story from the recipient's side.*
