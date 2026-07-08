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
    assert a==m; return a

# target.example SN key = RFC 8032 TEST 3
sk_t = Ed25519PrivateKey.from_private_bytes(bytes.fromhex(
    "c5aa8df43f9f837bedb7442f31dcb7b166d38535076f094b85ce3a2e0b4458f7"))
pub_t = sk_t.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
assert pub_t.hex() == "fc51cd8e6218a1a38da47ed00230f0580816ed13ba3303ac5deb911548908025", "not RFC8032 TEST3!"
kid_t, key_t = kid_of(pub_t), key_field(pub_t)

media_urn = "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y"
env_id = "019f2c92-2c88-7c16-a1fe-4548abf07edd"

def sign_verdict(payload, created):
    p={"kid":kid_t,"alg":"ed25519","created":created}
    i=jcs({"mlp_sig":"verdict/1","protected":p,"payload":payload})
    v=b64url(sk_t.sign(i))
    sk_t.public_key().verify(base64.urlsafe_b64decode(v+"=="), i)
    return {"payload":payload,"signature":{"mlp_sig":"verdict/1","protected":p,"value":v}}, i

# Verdict 1: synchronous reply — Tier 2 first contact
v1_payload = {"mlp":"0.1",
 "verdict_id": uuid7(datetime(2026,7,4,10,0,6,tzinfo=timezone.utc),"TV-002 verdict 1"),
 "created":"2026-07-04T10:00:06Z","issuer":"target.example",
 "envelope_origin":"origin.example","envelope_id":env_id,
 "message":"accepted",
 "recipients":[{"addr":"novak@target.example","verdict":"accepted"}],
 "media":[{"urn":media_urn,"verdict":"defer","reason":"pending-acceptance"}]}
v1, i1 = sign_verdict(v1_payload, "2026-07-04T10:00:06Z")

# Verdict 2: upgrade after recipient acceptance — grant with Reservation
token = b64url(blake3.blake3(b"TV-002 reservation token").digest()[:24])
ingest = "https://bs.target.example/ingest/" + blake3.blake3(b"TV-002 ingest path").hexdigest()[:16]
v2_payload = {"mlp":"0.1",
 "verdict_id": uuid7(datetime(2026,7,4,12,30,0,tzinfo=timezone.utc),"TV-002 verdict 2"),
 "created":"2026-07-04T12:30:00Z","issuer":"target.example",
 "envelope_origin":"origin.example","envelope_id":env_id,
 "message":"accepted",
 "recipients":[{"addr":"novak@target.example","verdict":"accepted"}],
 "media":[{"urn":media_urn,"verdict":"grant",
   "reservation":{"urn":media_urn,"max_size":36,"target_url":ingest,
                  "token":token,"expires":"2026-07-07T12:30:00Z"}}]}
v2, i2 = sign_verdict(v2_payload, "2026-07-04T12:30:00Z")

dd_target = {"domain":"target.example","mlp":["0.1"],"sn":"https://mlp.target.example/sn",
 "keys":[{"kid":kid_t,"alg":"ed25519","key":key_t,"roles":["sn","bs"]}]}

vector = {"vector":"TV-002",
 "description":"Negotiation transcript for the TV-001 dispatch: Tier-2 first-contact verdict (accepted, media deferred pending recipient acceptance), then the upgrade verdict carrying a Reservation. Issuer SN key = RFC 8032 Ed25519 TEST 3.",
 "issuer_key":{"seed_hex":"c5aa8df43f9f837bedb7442f31dcb7b166d38535076f094b85ce3a2e0b4458f7",
   "public_hex":pub_t.hex(),"key":key_t,"kid":kid_t},
 "target_domain_document":dd_target,
 "verdict_1_sig_input_jcs":i1.decode(),"signed_verdict_1":v1,
 "verdict_2_sig_input_jcs":i2.decode(),"signed_verdict_2":v2,
 "sizes":{"verdict_1_canonical":len(jcs(v1)),"verdict_2_canonical":len(jcs(v2))}}
open('../vectors/mlp-tv-002.json','w').write(json.dumps(vector,indent=2,ensure_ascii=False))
print("tv-002 regenerated")
