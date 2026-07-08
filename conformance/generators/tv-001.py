import json, base64
from datetime import datetime, timezone
import blake3, rfc8785
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization

def b32l(b: bytes) -> str:
    return base64.b32encode(b).decode().lower().rstrip("=")

def multihash_blake3(data: bytes) -> bytes:
    return bytes([0x1E, 0x20]) + blake3.blake3(data).digest()

def urn_mlet(data: bytes) -> str:
    return "urn:mlet:b" + b32l(multihash_blake3(data))

def kid_of(pub: bytes) -> str:  # provisional construction, pending S2.4
    return "b" + b32l(multihash_blake3(pub))

def b64url(b: bytes) -> str:
    return base64.urlsafe_b64encode(b).decode().rstrip("=")

def uuid7(dt: datetime, label: str) -> str:
    ts = int(dt.timestamp() * 1000)
    rnd = blake3.blake3(label.encode()).digest()
    b = bytearray(16)
    b[0:6] = ts.to_bytes(6, "big")
    b[6] = 0x70 | (rnd[0] & 0x0F)          # version 7
    b[7] = rnd[1]
    b[8] = 0x80 | (rnd[2] & 0x3F)          # variant 10
    b[9:16] = rnd[3:10]
    h = b.hex()
    return f"{h[0:8]}-{h[8:12]}-{h[12:16]}-{h[16:20]}-{h[20:32]}"

def jcs(obj) -> bytes:
    a = rfc8785.dumps(obj)
    m = json.dumps(obj, sort_keys=True, separators=(",", ":"), ensure_ascii=False).encode()
    assert a == m, "JCS implementations disagree"
    return a

# --- keys: RFC 8032 section 7.1 TEST 1 (author role) and TEST 2 (sn role)
sk_author = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(
    "9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60"))
sk_sn = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(
    "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"))
pub_author = sk_author.public_key().public_bytes(
    serialization.Encoding.Raw, serialization.PublicFormat.Raw)
pub_sn = sk_sn.public_key().public_bytes(
    serialization.Encoding.Raw, serialization.PublicFormat.Raw)
kid_author, kid_sn = kid_of(pub_author), kid_of(pub_sn)

# --- media object
media = b"MLP test vector 001: media object A\n"
media_urn = urn_mlet(media)

t_created = datetime(2026, 7, 4, 10, 0, 0, tzinfo=timezone.utc)
t_dispatch = datetime(2026, 7, 4, 10, 0, 5, tzinfo=timezone.utc)

medialet = {
    "mlp": "0.1",
    "id": uuid7(t_created, "TV-001 medialet id"),
    "author": "petra@origin.example",
    "subject": "TV-001 sample delivery",
    "created": "2026-07-04T10:00:00Z",
    "displayed_to": [{"addr": "novak@target.example", "name": "Nov\u00e1k Family"}],
    "body": {
        "profile": "mlp-html/1",
        "content": "<p>Hello from TV-001. File: <a href=\"" + media_urn + "\">sample.txt</a></p>"
    },
    "manifest": [{
        "urn": media_urn, "size": len(media), "type": "text/plain",
        "name": "sample.txt", "available_until": "2026-07-11T10:00:00Z"
    }]
}

prot_a = {"kid": kid_author, "alg": "ed25519", "created": "2026-07-04T10:00:00Z"}
sig_input_a = jcs({"mlp_sig": "author/1", "protected": prot_a, "payload": medialet})
sig_a = b64url(sk_author.sign(sig_input_a))

signed_medialet = {"medialet": medialet,
                   "signature": {"mlp_sig": "author/1", "protected": prot_a, "value": sig_a}}
medialet_ca = urn_mlet(jcs(signed_medialet))

envelope = {
    "mlp": "0.1",
    "envelope_id": uuid7(t_dispatch, "TV-001 envelope id"),
    "created": "2026-07-04T10:00:05Z",
    "origin": "origin.example",
    "envelope_to": ["novak@target.example"],
    "medialet": signed_medialet
}
prot_h = {"kid": kid_sn, "alg": "ed25519", "created": "2026-07-04T10:00:05Z"}
sig_input_h = jcs({"mlp_sig": "hop/1", "protected": prot_h, "payload": envelope})
sig_h = b64url(sk_sn.sign(sig_input_h))
signed_envelope = {"envelope": envelope,
                   "signature": {"mlp_sig": "hop/1", "protected": prot_h, "value": sig_h}}

# sanity: verify both signatures
sk_author.public_key().verify(base64.urlsafe_b64decode(sig_a + "=="), sig_input_a)
sk_sn.public_key().verify(base64.urlsafe_b64decode(sig_h + "=="), sig_input_h)

out = {
    "media_bytes_repr": media.decode(),
    "media_size": len(media),
    "media_urn": media_urn,
    "pub_author_hex": pub_author.hex(),
    "pub_sn_hex": pub_sn.hex(),
    "kid_author": kid_author,
    "kid_sn": kid_sn,
    "medialet_id": medialet["id"],
    "envelope_id": envelope["envelope_id"],
    "author_sig": sig_a,
    "hop_sig": sig_h,
    "medialet_content_address": medialet_ca,
    "canonical_signed_medialet": jcs(signed_medialet).decode(),
    "canonical_signed_envelope_size": len(jcs(signed_envelope)),
    "author_sig_input": sig_input_a.decode(),
}
print(json.dumps(out, indent=1, ensure_ascii=False))
