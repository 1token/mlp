# The MLP documentation map

**Normative** (the protocol's truth):
- `spec/MLP-Core-Specification-0.1-draft-03.md` — the wire, 17
  sections; every MUST audited (`conformance/MUST-AUDIT.md`, D-104)
- `spec/meps/` — the change-control record (MEP-001/002: accepted)
- `conformance/` — vectors TV-001–TV-007 with committed generators,
  the sanitizer corpus, the MUST audit instrument

**Product** (what is built and why):
- `docs/product/USER-STORIES.md` — five personas, every story tested
- `docs/product/PRD.md` — functional & non-functional requirements,
  decision-traceable, status-honest

**Architecture** (how the code is shaped):
- `docs/architecture/ARCHITECTURE.md` — system context, modules,
  deployment, the three background loops (PlantUML)
- `docs/architecture/DOMAIN-MODEL.md` — the entities and their
  loyalties (class diagram)
- `docs/architecture/DATA-MODEL.md` — the schema by subsystem
  (generated: `gen-er.py`; regenerate after migrations)
- `docs/architecture/SEQUENCES.md` — the five signature flows, each
  bound to its certifying test

**Interfaces**:
- `api/openapi.yaml` — OpenAPI 3.0 for the Client API (/api/v1,
  45 operations; validated in CI). The federation surface is
  signature-driven and specified by the core spec, not OpenAPI.
- `design/stage3/MLP-Client-API-draft-01.md` — the companion’s prose
  rationale (the OpenAPI is the machine-readable projection)

**Operations & program**:
- `docs/OPERATOR.md` — running a domain (D-180)
- `demo/DEMO.md` — the minimum credible demo, on camera
- `docs/NLNET-APPLICATION.md` — the funding package (D-42)
- `docs/S4-CONTINUITY.md` — the decision register and session state
- `docs/closing/` — Stages 1–3, frozen

Diagrams are PlantUML in fenced blocks; render with the VS Code
PlantUML extension or plantuml.jar. Generated documents
(`DATA-MODEL.md`, `MUST-AUDIT.md`, `must-corpus.txt`) are edited
only through their generators — CI diffs them against the sources.
