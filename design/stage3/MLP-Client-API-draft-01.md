# MLP Client API — draft-01 (informative companion)

> **Status.** Informative companion per D-68/D-86: the client↔home-
> server interface is deployment freedom in the core spec; this draft
> is the reference implementation's API, the interop suggestion for
> independent clients, and Stage 4's schema requirements list.
> Gathered from requirements registered in S3.2–S3.8. Adopted by
> D-170/D-171; refined field-by-field in Stage 4.

---

## 1. Conventions (D-170)

- Base `/{app-origin}/api/v1/`; JSON in the **D-43 dialect** —
  integers only, RFC 3339 UTC, snake_case. One JSON dialect
  project-wide: federation and client code never switch conventions.
- Errors: `application/problem+json`, `type` = `urn:mlp:err:<code>`,
  reusing and extending the §14 registry (client-side additions e.g.
  `quota-exceeded`, `draft-conflict`, `auth-required`).
- Auth: HttpOnly session cookie (SameSite=Lax) + a required
  `X-MLP-Client` header on all mutations (CSRF posture).
- Pagination: opaque `?cursor=`; responses carry `next_cursor`.
- **Idempotency**: mutating POSTs accept a client-generated
  `Idempotency-Key` (offline-queue safety, D-169).
- **Undo**: triage mutations return `undo_token` (TTL ≈ 30 s);
  `POST /undo {token}` reverses transactionally (D-129 sweep
  semantics).
- Live: `GET /events` — SSE, typed events, `Last-Event-ID` resume
  (D-132).

## 2. Endpoint inventory

### Auth & identity (S3.8 / D-161)

| Method & path | Purpose |
|---|---|
| POST `/auth/register-options` · `/auth/register` | WebAuthn ceremony (signup / add passkey) |
| POST `/auth/login-options` · `/auth/login` | WebAuthn assertion |
| POST `/auth/password` | Fallback login |
| POST `/auth/recovery-codes` | Generate/regenerate one-time codes |
| POST `/auth/recover` | Code- or email-based recovery entry |
| GET/DELETE `/sessions` | Device list; per-session revoke; sign-out-everywhere |

### Inbox & threads (S3.2 / D-132)

| Method & path | Purpose |
|---|---|
| GET `/threads?view=inbox&cursor=` | Rolled-up thread list: section key, label ids, participants, derived-text snippet, media aggregate (counts/states/preview refs), most-urgent deadline, unread |
| GET `/threads/{id}` | Full thread: messages (render-form refs), Files-panel data |
| POST `/threads/{id}/done` · `/flag` · `/read` | Triage (undo tokens) |
| POST `/threads/{id}/labels` | Apply/remove |
| POST `/sweep` | Batch done+read for a bundle instance — one transactional unit (D-129) |
| GET/POST/PATCH/DELETE `/labels` | Taxonomy CRUD incl. bundle switch, notification policy (D-130) |
| POST `/threads/{id}/junk` · `/rescue` | Classifier feedback; rescue triggers deferred-upgrade (D-165) |

### Compose & dispatch (S3.3 / D-135, D-138)

| Method & path | Purpose |
|---|---|
| GET/POST/PATCH/DELETE `/drafts` | Autosaving draft CRUD (unsigned medialet JSON + upload state) |
| GET `/objects/have?urn=` | Compose-time have-check (attach-by-reference) |
| POST `/uploads` (tus) | Intra-domain resumable upload to a chosen store (D-105 `store` param) |
| POST `/drafts/{id}/send` | Pre-flight (server side of D-138) → sign → dispatch; returns delivery id |
| POST `/resolve?resource=acct:` | SN-mediated compose-time resolution (§5.6, D-60) |

### Receive & objects (S3.4, S3.7 / D-141–144, D-156–160)

| Method & path | Purpose |
|---|---|
| GET `/o/{urn}` · `/o/{urn}/thumb?w=` | Resolution + server-side derivatives (§10.8; D-160); ranged |
| POST `/o/{urn}/accept` | defer→grant / delegation trigger; `store` param (accept-time selector, D-141) |
| POST `/o/{urn}/dismiss` · `/decline` | Local reversible vs terminal deny (D-142) |
| POST `/o/{urn}/pin` · `/unpin` · `/move` | Retention; store migration (D-160) |
| GET `/library?facets…&cursor=` | Object-level aggregation (one card per URN, provenance, state rollup — D-156) with facet filters (D-157) |
| POST `/objects/labels` | Media labels, write-through (D-156) |
| GET `/quota` | Per-store segmented meters (D-159) |
| GET `/cleanup-candidates` | GC-ordered suggestions (ephemeral first, largest unpinned — D-159) |
| GET/POST/PATCH/DELETE `/stores` · `/stores/rules` | Store list; the D-160 routing-rules table |

### Deliveries (S3.5 / D-145–150)

| Method & path | Purpose |
|---|---|
| GET `/deliveries?filter=&cursor=` | List with headline states (D-145) |
| GET `/deliveries/{id}` | The two matrices (domain-grouped, D-146) + guest rows (D-147) |
| GET `/deliveries/{id}/timeline` | Chronological protocol-fact feed (D-149) |
| POST `/deliveries/{id}/extend` | Re-dispatch with fresh windows (D-122/D-150); returns dedup diff |
| PATCH `/deliveries/{id}/delegation` | Budget setting: default/off/window (D-148) |
| POST/PATCH/DELETE `/deliveries/{id}/guest-links` | Create/PIN/expiry/revoke (D-152) |

### Correspondents & junk (S3.8 / D-162–165)

| Method & path | Purpose |
|---|---|
| GET `/correspondents` · `/{addr}` | Tier with reason (D-162), history refs |
| POST `/correspondents/{addr}/allow` · `/ask-first` · `/block` | Tier overrides; block = rejected:policy (D-163) |
| GET `/junk?cursor=` | Quarantine list (derived-text-first payloads, D-165) |

### Settings & live

| Method & path | Purpose |
|---|---|
| GET/PATCH `/settings` | Defaults: windows (D-137), guest (D-152), notifications, mobile-data guard (D-141) |
| GET `/events` | The SSE feed (§1) |

*(Guest pages live outside this API — the capability-URL space of
Annex A/D-151, unauthenticated by design.)*

## 3. Stage 4 handoff note

This inventory is the requirements list for the reference
implementation's SQLite schema and Go handlers: every endpoint names
the tables it implies (threads/rollups, references, labels, rules,
drafts, deliveries/timeline, guest links, sessions/credentials,
undo/idempotency journals). Field-level schemas are Stage 4 work,
JSDoc-mirrored per D-117.

---

*Companion to: MLP Flagship Client Design S3.9. Core-spec relation:
informative (D-68/D-86); nothing here is required for federation
conformance.*
