import json, base64
import blake3

# TV-007 (MEP-002): `preview_of` Manifest-validation outcomes. A
# violating member is IGNORED at ingest — the entry stands, the
# member goes. Four cases: the valid pair; a dangling target; a
# chain (a preview's target itself carries preview_of); and
# self-reference. Expected manifests are the inputs with violating
# members stripped. Purely deterministic: URNs derive from fixed
# content strings through the real address construction.

def b32l(b): return base64.b32encode(b).decode().lower().rstrip("=")
def mh_b3(d): return bytes([0x1E,0x20]) + blake3.blake3(d).digest()
def urn_mlet(d): return "urn:mlet:b" + b32l(mh_b3(d))

MASTER  = urn_mlet(b"TV-007 master object")
PREVIEW = urn_mlet(b"TV-007 preview object")
THIRD   = urn_mlet(b"TV-007 third object")
ABSENT  = urn_mlet(b"TV-007 never-declared object")

def entry(urn, size, preview_of=None, name=None):
    e = {"urn": urn, "size": size, "type": "image/jpeg",
         "available_until": "2026-08-01T00:00:00Z"}
    if name: e["name"] = name
    if preview_of: e["preview_of"] = preview_of
    return e

def strip(e):
    out = dict(e); out.pop("preview_of", None); return out

cases = []

# 1. The valid pair: kept verbatim.
m = [entry(MASTER, 5_000_000, name="master.jpg"),
     entry(PREVIEW, 180_000, preview_of=MASTER, name="master-preview.jpg")]
cases.append({"name": "valid_pair", "manifest": m, "expected": m})

# 2. Dangling target: the member names a urn absent from the
#    Manifest — ignored, entry stands.
m = [entry(MASTER, 5_000_000),
     entry(PREVIEW, 180_000, preview_of=ABSENT)]
cases.append({"name": "dangling_target",
              "manifest": m, "expected": [m[0], strip(m[1])]})

# 3. Chain: PREVIEW previews MASTER, THIRD previews PREVIEW — the
#    chain-forming member (THIRD's, whose target is itself a
#    preview_of carrier) is ignored; the base pair stands.
m = [entry(MASTER, 5_000_000),
     entry(PREVIEW, 180_000, preview_of=MASTER),
     entry(THIRD, 20_000, preview_of=PREVIEW)]
cases.append({"name": "chain",
              "manifest": m, "expected": [m[0], m[1], strip(m[2])]})

# 4. Self-reference: ignored, entry stands.
m = [entry(MASTER, 5_000_000, preview_of=MASTER)]
cases.append({"name": "self_reference",
              "manifest": m, "expected": [strip(m[0])]})

vector = {
    "vector": "TV-007", "mep": "MEP-002",
    "description": "preview_of Manifest-validation outcomes (spec 3.2.2): violating members are ignored at ingest; entries otherwise stand.",
    "cases": cases,
}
open("../vectors/mlp-tv-007.json", "w").write(json.dumps(vector, indent=2, ensure_ascii=False))
print("TV-007:", len(cases), "cases")
