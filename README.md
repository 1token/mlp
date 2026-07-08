# Medialet (MLP) — email for heavy media

An open, federated protocol for asynchronous point-to-point delivery of
heavy media. Signaling is cheap and optimistic; storage is expensive and
pessimistic: no payload byte moves without an explicit, scoped, expiring
reservation.

| Area | Status |
|---|---|
| `spec/` | **MLP/0.1 draft-01, frozen** (Stage 2, D-108); changes via `spec/meps/` |
| `conformance/` | Vectors TV-001–005, **byte-reproducible** from `generators/` (CI-enforced) |
| `design/stage3/` | Flagship client design, **frozen** (Stage 3, D-181) + Client API draft-01 |
| `docs/closing/` | The decision record: 181 decisions across three closing documents |
| `server/` `client/` | Stage 4 — implementation in progress |

Licensing: see `LICENSING.md`. Governance: sole editor through 1.0;
all changes via MEP (Stage 1 Closing Document, D-40).
