# TV-001 generator — final form (S2.4: D-61/D-62 kid and key encodings).
# Rewritten in S4.3: the previous copy of this file was the pre-S2.4
# provisional generator (raw-key kid, stdout-only), so the CI
# reproducibility gate never actually covered TV-001 (D-197).
# This generator writes ../vectors/mlp-tv-001.json byte-identically.
import json, base64
from datetime import datetime, timezone
import blake3, rfc8785
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization

def b32l(b): return base64.b32encode(b).decode().lower().rstrip("=")
def mh_b3(d): return bytes([0x1E,0x20]) + blake3.blake3(d).digest()
def urn_mlet(d): return "urn:mlet:b" + b32l(mh_b3(d))
def mc(pub): return bytes([0xED,0x01]) + pub
def key_field(pub): return "b" + b32l(mc(pub))
def kid_of(pub): return "b" + b32l(mh_b3(mc(pub)))
def b64url(b): return base64.urlsafe_b64encode(b).decode().rstrip("=")
def uuid7(dt,label):
    ts=int(dt.timestamp()*1000); r=blake3.blake3(label.encode()).digest()
    b=bytearray(16); b[0:6]=ts.to_bytes(6,"big"); b[6]=0x70|(r[0]&15); b[7]=r[1]; b[8]=0x80|(r[2]&63); b[9:16]=r[3:10]
    h=b.hex(); return f"{h[0:8]}-{h[8:12]}-{h[12:16]}-{h[16:20]}-{h[20:32]}"
def jcs(o):
    a=rfc8785.dumps(o); m=json.dumps(o,sort_keys=True,separators=(",",":"),ensure_ascii=False).encode()
    assert a==m, "JCS implementations disagree"
    return a

# --- keys: RFC 8032 section 7.1 TEST 1 (author) and TEST 2 (sn/bs)
seed_a = "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"
seed_s = "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"
sk_a = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(seed_a))
sk_s = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(seed_s))
raw = lambda sk: sk.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
pub_a, pub_s = raw(sk_a), raw(sk_s)
assert pub_a.hex() == "d75a980182b10ab7d54bfed3c964073a0ee172f3daa62325af021a68f707511a", "not RFC8032 TEST1!"
assert pub_s.hex() == "3d4017c3e843895a92b70aa74d1b7ebc9c982ccf2ec4968cc0cd55f12af4660c", "not RFC8032 TEST2!"
kid_a, key_a = kid_of(pub_a), key_field(pub_a)
kid_s, key_s = kid_of(pub_s), key_field(pub_s)

# --- media object
media = b"MLP test vector 001: media object A\n"
media_urn = urn_mlet(media)

t_created  = datetime(2026, 7, 4, 10, 0, 0, tzinfo=timezone.utc)
t_dispatch = datetime(2026, 7, 4, 10, 0, 5, tzinfo=timezone.utc)

# --- Domain Document fixture (spec §5.2 example; S4.3 parsing anchor)
domain_document = {
    "domain": "origin.example",
    "mlp": ["0.1"],
    "sn": "https://mlp.origin.example/sn",
    "contact": "hostmaster@origin.example",
    "keys": [
        {"kid": kid_a, "alg": "ed25519", "key": key_a, "roles": ["author"]},
        {"kid": kid_s, "alg": "ed25519", "key": key_s, "roles": ["sn", "bs"]},
    ],
}

# --- Medialet and author signature (spec §3.3.2)
medialet = {
    "mlp": "0.1",
    "id": uuid7(t_created, "TV-001 medialet id"),
    "author": "petra@origin.example",
    "subject": "TV-001 sample delivery",
    "created": "2026-07-04T10:00:00Z",
    "displayed_to": [{"addr": "novak@target.example", "name": "Novák Family"}],
    "body": {
        "profile": "mlp-html/1",
        "content": "<p>Hello from TV-001. File: <a href=\"" + media_urn + "\">sample.txt</a></p>",
    },
    "manifest": [
        {"urn": media_urn, "size": len(media), "type": "text/plain",
         "name": "sample.txt", "available_until": "2026-07-11T10:00:00Z"}
    ],
}
prot_a = {"kid": kid_a, "alg": "ed25519", "created": "2026-07-04T10:00:00Z"}
sig_input_a = jcs({"mlp_sig": "author/1", "protected": prot_a, "payload": medialet})
sig_a = b64url(sk_a.sign(sig_input_a))
signed_medialet = {"medialet": medialet,
                   "signature": {"mlp_sig": "author/1", "protected": prot_a, "value": sig_a}}
medialet_ca = urn_mlet(jcs(signed_medialet))

# --- Envelope and hop signature (spec §3.4)
envelope = {
    "mlp": "0.1",
    "envelope_id": uuid7(t_dispatch, "TV-001 envelope id"),
    "created": "2026-07-04T10:00:05Z",
    "origin": "origin.example",
    "envelope_to": ["novak@target.example"],
    "medialet": signed_medialet,
}
prot_h = {"kid": kid_s, "alg": "ed25519", "created": "2026-07-04T10:00:05Z"}
sig_input_h = jcs({"mlp_sig": "hop/1", "protected": prot_h, "payload": envelope})
sig_h = b64url(sk_s.sign(sig_input_h))
signed_envelope = {"envelope": envelope,
                   "signature": {"mlp_sig": "hop/1", "protected": prot_h, "value": sig_h}}

# --- sanity: verify both signatures before emitting anything
sk_a.public_key().verify(base64.urlsafe_b64decode(sig_a + "=="), sig_input_a)
sk_s.public_key().verify(base64.urlsafe_b64decode(sig_h + "=="), sig_input_h)

vector = {
    "vector": "TV-001",
    "revision": "final (S2.4: D-61/D-62 kid and key encodings)",
    "description": "Direct dispatch, one recipient, one Media object. Author key = RFC 8032 TEST 1; SN/BS key = RFC 8032 TEST 2.",
    "keys": {
        "author": {"seed_hex": seed_a, "public_hex": pub_a.hex(), "key": key_a, "kid": kid_a},
        "sn":     {"seed_hex": seed_s, "public_hex": pub_s.hex(), "key": key_s, "kid": kid_s},
    },
    "media": {"bytes_utf8": media.decode(), "size": len(media), "urn": media_urn},
    "domain_document": domain_document,
    "author_sig_input_jcs": sig_input_a.decode(),
    "hop_sig_input_jcs": sig_input_h.decode(),
    "signed_medialet": signed_medialet,
    "signed_medialet_content_address": medialet_ca,
    "signed_envelope": signed_envelope,
    "signed_envelope_canonical_size": len(jcs(signed_envelope)),
}
open('../vectors/mlp-tv-001.json','w').write(json.dumps(vector,indent=2,ensure_ascii=False))
