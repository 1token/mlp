# MLP Flagship Client Design — S3.5: Status & Tracking

> **Status.** Stage 3, session 5. Judgment calls D-145–D-150 pending
> editor confirmation. The author's chair: everything here renders
> protocol facts (D-37) and nothing else. §8 traces per D-38.

---

## 1. The Deliveries list (D-145)

Each row: subject, job-tag chip (D-123), recipient summary, **headline
state**, expiry countdown, unread-reply badge, latest activity. The
headline derives from the matrices by a priority ladder — worst news
first:

1. **Attention** — any rejection, failed transfer, or expiring window
   with unaccepted offers;
2. **Transferring** — pushes in flight ("3 of 5 objects");
3. **Awaiting** — delivered, offers pending acceptance;
4. **Complete** — all recipients settled;
5. **Expired** — window closed with offers unaccepted (a resting state,
   not an error: Extend is one click away).

Filter chips: *Needs attention · Awaiting · Expiring soon · Complete*.
Default sort: latest activity.

**The Studio dashboard strip** above the list carries counts for the
first three states plus the **outbound-promises meter** — the sum of
live `promised` references (§10.5) made visible: "Your store is
promising 214 GB through Aug 3." The sender-side retention duty (D-88),
surfaced where the sender lives.

## 2. The delivery detail — two matrices and a timeline

### 2.1 Recipient matrix, grouped by domain (D-146)

Rows are **domain groups** containing their recipients; guest
recipients appear as their own rows alongside (§2.3). Per recipient:
message verdict with reason (accepted / rejected: `unknown-recipient` /
quarantined — §7.4). Per **domain group**: the media acceptance state
and its timestamp.

The grouping is the honesty: per-URN verdicts and acceptance upgrades
are domain-level facts (D-70) — when three recipients share
`target.example`, the author learns *the domain* fetched the masters,
never which person clicked. Single-recipient domains collapse to
per-person display naturally; multi-recipient domains render the D-70
truth truthfully rather than inventing precision.

### 2.2 Object matrix

Rows: objects (thumb, name, size). Columns per domain group: the
pipeline `verdict → transfer → verified → accepted-at`. `have` renders
as the dedup feature moment when disclosed — "already present at
target.example, no transfer needed" (Tier-1 disclosure per D-29;
masked cases simply look like ordinary transfers to the author, which
is the masking working). Per-object **delegation budget meter** when
delegation has occurred (§2.4).

### 2.3 Guest rows (D-147)

Guest state vocabulary: *link created → email sent → downloads
(per object, with counts) → claimed → expired/revoked*. Actions: copy
link, set/reset PIN, extend link expiry, revoke (Annex A). **Claimed**
is a celebrated fact — "carol@… claimed a permanent address" is the
adoption funnel (D-36) reporting success.

**The reading-privacy line, held** (D-147): guest **downloads** are
shown — transfers are visible acts (D-98). Guest page **opens are never
surfaced**, although the sender's own server serves the page and
trivially could. D-37's "no read status exists or will be inferred"
holds exactly where it is cheapest to break; the flagship treats that
as identity: *Medialet doesn't watch reading — not even on pages it
serves itself.*

### 2.4 Delegation events (D-148)

Rendered as neutral facts, never alarms: "Your delivery was forwarded;
the copy was fetched by final.example · budget 1/10" — domain only
(all the wire carries, §9.6), object, timestamp, budget meter. The
author-side control surfaces here and in the composer: **per-delivery
delegation budget** — default 10 / off / window-bound — origin-side
policy entirely within D-23 ("off" answers `delegation-budget`,
steering forwarders to custody mode, §9.7).

### 2.5 The timeline (D-149)

A chronological feed of protocol facts from the origin's own records:
dispatched (per domain) → verdicts → transfer start/finish (per
object) → acceptance upgrades (D-98 events) → delegation requests →
replies. Every entry carries its timestamp and source document
reference. It is simultaneously the human story of the delivery and
the audit/debug surface — one rendering, two audiences.

## 3. Extend, Resend, and the closing of the loop (D-150)

**Author-side expiry alerts**: when a window approaches with offers
unaccepted, the delivery raises Attention and notifies (default 3 days
ahead): "Novák hasn't downloaded the masters — window closes Friday.
Extend?" **Extend** shows its diff before acting ("2 pending objects
get 30 more days; 12 transferred objects re-offer at zero cost —
dedup") and re-dispatches (D-122).

**The resend-request convention** (closing the D-143 loop): the
recipient-side template carries the object's *name and URN as plain
text* — deliberately never a Body link, because §3.2.3/D-92 would
demand a Manifest entry, and a Manifest entry carries an
`available_until` promise the requester cannot honestly make for an
object they do not hold. Clients recognize the pattern in the derived
text of thread replies; the author's client raises the action chip in
both thread and delivery detail: "Novák requested *final-masters.zip*
→ Extend availability." Convention over wire; the profile rules stay
unviolated.

**Notification policy** (author side): rejections and failures —
instant; acceptance events — instant or digest (sender's choice; these
are the D-98 facts Petra runs her business on); replies — per-label
policy (D-130); expiry alerts — the advance default above. All of it
protocol facts; nothing else exists to notify about.

## 4. Open questions parked

1. Delivery analytics over time (jobs per month, acceptance latency
   medians) — S3.11 backlog; must remain aggregate protocol facts.
2. Multi-delivery bulk operations (extend all expiring) — S3.11.

## 5. Traceability (D-38)

| Element | Traces to |
|---|---|
| Headline ladder, filters | D-122; D-145 |
| Outbound-promises meter | §10.5; D-88; D-145 |
| Domain-grouped attribution | D-70; D-146 |
| `have` as feature moment | D-16/D-29; D-146 |
| Guest states, claim celebration | D-36/Annex A; D-147 |
| Downloads-not-opens line | D-31/D-37/D-98; D-147 |
| Delegation facts + budget control | D-23; §9.5–9.6; D-148 |
| Timeline as audit surface | D-37; D-53 records; D-149 |
| Expiry alerts, Extend diff | D-122; D-145; D-150 |
| Plain-text resend convention | §3.2.3/D-92; D-143; D-150 |

---

*Next: S3.6 — guest delivery and claim conversion: the Annex A pattern
as product — the guest page (sharing the Body viewer per D-115), PIN
and expiry UX, the notification email that must not train phishing
victims, the claim flow from link to permanent address, and the
re-dispatch that turns a guest delivery into a federated one.*
