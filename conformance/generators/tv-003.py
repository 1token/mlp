import json, base64, hashlib
from datetime import datetime, timezone
import blake3, rfc8785
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization

def b32l(b): return base64.b32encode(b).decode().lower().rstrip("=")
def mh_b3(d): return bytes([0x1E,0x20]) + blake3.blake3(d).digest()
def urn_mlet(d): return "urn:mlet:b" + b32l(mh_b3(d))
def mc(p): return bytes([0xED,0x01]) + p
def kid_of(p): return "b" + b32l(mh_b3(mc(p)))

# pusher: origin.example bs-role key = RFC 8032 TEST 2
sk = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(
    "4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb"))
pub = sk.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
kid = kid_of(pub)
assert kid == "bdyqnbqeil3qzkhtocxe77a7j5qactmknmkncicd6k2glhgyrbvnzs5a"

media = b"MLP test vector 001: media object A\n"
urn = urn_mlet(media)
assert urn == "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y"
part1, part2 = media[:20], media[20:]
target = "https://bs.target.example/ingest/24c372e9a5a3c559"
token = "Yam_b2ATZeRdhaofjfPPEasHKNDlGBAB"

def cdigest(body): return "sha-256=:" + base64.b64encode(hashlib.sha256(body).digest()).decode() + ":"
def epoch(s): return int(datetime.fromisoformat(s.replace("Z","+00:00")).timestamp())

def sig(method, created, body=None, offset=None):
    if body is not None:
        comps = [("@method", method), ("@target-uri", target),
                 ("content-digest", cdigest(body)), ("upload-offset", str(offset)),
                 ("mlp-reservation", token)]
    else:
        comps = [("@method", method), ("@target-uri", target), ("mlp-reservation", token)]
    inner = "(" + " ".join(f'"{n}"' for n,_ in comps) + f');created={created};keyid="{kid}";alg="ed25519"'
    base = "\n".join(f'"{n}": {v}' for n,v in comps) + f'\n"@signature-params": {inner}'
    s = sk.sign(base.encode())
    sk.public_key().verify(s, base.encode())
    return {"signature_base": base,
            "Signature-Input": f"mlp={inner}",
            "Signature": "mlp=:" + base64.b64encode(s).decode() + ":"}

patch1 = sig("PATCH", epoch("2026-07-04T12:31:00Z"), body=part1, offset=0)
head1  = sig("HEAD",  epoch("2026-07-04T12:31:05Z"))
patch2 = sig("PATCH", epoch("2026-07-04T12:31:10Z"), body=part2, offset=20)

vector = {"vector":"TV-003",
 "description":"Push transcript for the TV-002 Reservation: PATCH bytes 0-19 (accepted, reply lost), HEAD resume discovery (offset 20), PATCH bytes 20-35 completing; final BLAKE3 equals the TV-001 URN. Pusher bs-role key = RFC 8032 Ed25519 TEST 2.",
 "pusher_kid": kid, "reservation": {"target_url": target, "token": token, "max_size": 36,
   "urn": urn, "expires": "2026-07-07T12:30:00Z"},
 "object": {"bytes_utf8": media.decode(), "size": 36, "urn": urn},
 "requests": [
   {"step":1,"request":"PATCH","upload_offset":0,"body_utf8":part1.decode(),
    "headers":{"Tus-Resumable":"1.0.0","Content-Type":"application/offset+octet-stream",
      "Upload-Offset":"0","Content-Digest":cdigest(part1),"MLP-Reservation":token,
      "Signature-Input":patch1["Signature-Input"],"Signature":patch1["Signature"]},
    "signature_base":patch1["signature_base"],
    "response":{"status":204,"Upload-Offset":"20","note":"reply lost in transit (simulated)"}},
   {"step":2,"request":"HEAD",
    "headers":{"Tus-Resumable":"1.0.0","MLP-Reservation":token,
      "Signature-Input":head1["Signature-Input"],"Signature":head1["Signature"]},
    "signature_base":head1["signature_base"],
    "response":{"status":200,"Upload-Offset":"20","Upload-Length":"36",
      "Upload-Expires":"Tue, 07 Jul 2026 12:30:00 GMT","Cache-Control":"no-store"}},
   {"step":3,"request":"PATCH","upload_offset":20,"body_utf8":part2.decode(),
    "headers":{"Tus-Resumable":"1.0.0","Content-Type":"application/offset+octet-stream",
      "Upload-Offset":"20","Content-Digest":cdigest(part2),"MLP-Reservation":token,
      "Signature-Input":patch2["Signature-Input"],"Signature":patch2["Signature"]},
    "signature_base":patch2["signature_base"],
    "response":{"status":204,"Upload-Offset":"36","MLP-Object-State":"verified",
      "note":"offset == Upload-Length; final BLAKE3 matches URN; object live; token consumed"}}]}
open('../vectors/mlp-tv-003.json','w').write(json.dumps(vector,indent=2,ensure_ascii=False))
print("tv-003 regenerated")
