import json, base64
from datetime import datetime, timezone
import blake3, rfc8785
from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
from cryptography.hazmat.primitives import serialization

def b32l(b): return base64.b32encode(b).decode().lower().rstrip("=")
def mh_b3(d): return bytes([0x1E,0x20]) + blake3.blake3(d).digest()
def urn_mlet(d): return "urn:mlet:b" + b32l(mh_b3(d))
def mc(p): return bytes([0xED,0x01]) + p
def key_field(p): return "b" + b32l(mc(p))
def kid_of(p): return "b" + b32l(mh_b3(mc(p)))
def b64url(b): return base64.urlsafe_b64encode(b).decode().rstrip("=")
def uuid7(dt,label):
    ts=int(dt.timestamp()*1000); r=blake3.blake3(label.encode()).digest()
    b=bytearray(16); b[0:6]=ts.to_bytes(6,"big"); b[6]=0x70|(r[0]&15); b[7]=r[1]; b[8]=0x80|(r[2]&63); b[9:16]=r[3:10]
    h=b.hex(); return f"{h[0:8]}-{h[8:12]}-{h[12:16]}-{h[16:20]}-{h[20:32]}"
def jcs(o):
    a=rfc8785.dumps(o); m=json.dumps(o,sort_keys=True,separators=(",",":"),ensure_ascii=False).encode()
    assert a==m; return a
def sign_doc(sk,label,payload,created,kid):
    p={"kid":kid,"alg":"ed25519","created":created}
    i=jcs({"mlp_sig":label,"protected":p,"payload":payload}); v=b64url(sk.sign(i))
    sk.public_key().verify(base64.urlsafe_b64decode(v+"=="), i)
    return {"payload_wrapped":None,"signature":{"mlp_sig":label,"protected":p,"value":v},"input":i}

K = lambda h: Ed25519PrivateKey.from_private_bytes(bytes.fromhex(h))
P = lambda sk: sk.public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
sk_a  = K("9d61b19deffd5a60ba844af492ec2cc44449c5697b326919703bac031cae7f60")  # origin author (T1)
sk_o  = K("4ccd089b28ff96da9db6c346ec114e0f5b8a319f35aba624da8cf6ed4fb8a6fb")  # origin sn/bs (T2)
sk_t  = K("c5aa8df43f9f837bedb7442f31dcb7b166d38535076f094b85ce3a2e0b4458f7")  # target sn/bs (T3)
sk_f  = K("f5e5767cf153319517630f226876b86c8160cc583bc013744c6bf255f5cc0ee5")  # final sn/bs (T1024)
assert P(sk_f).hex() == "278117fc144c72340f67d0f2316e8386ceffbf2b2428c9c51fef7c597f1d426e", "not RFC8032 TEST1024"
kid_a,kid_o,kid_t,kid_f = map(lambda s: kid_of(P(s)), (sk_a,sk_o,sk_t,sk_f))

# --- rebuild TV-001 deterministically and assert continuity
media = b"MLP test vector 001: media object A\n"; media_urn = urn_mlet(media)
t0=datetime(2026,7,4,10,0,0,tzinfo=timezone.utc); t1=datetime(2026,7,4,10,0,5,tzinfo=timezone.utc)
medialet={"mlp":"0.1","id":uuid7(t0,"TV-001 medialet id"),"author":"petra@origin.example",
 "subject":"TV-001 sample delivery","created":"2026-07-04T10:00:00Z",
 "displayed_to":[{"addr":"novak@target.example","name":"Nov\u00e1k Family"}],
 "body":{"profile":"mlp-html/1","content":"<p>Hello from TV-001. File: <a href=\""+media_urn+"\">sample.txt</a></p>"},
 "manifest":[{"urn":media_urn,"size":36,"type":"text/plain","name":"sample.txt","available_until":"2026-07-11T10:00:00Z"}]}
sa=sign_doc(sk_a,"author/1",medialet,"2026-07-04T10:00:00Z",kid_a)
sm={"medialet":medialet,"signature":sa["signature"]}
assert sa["signature"]["value"].startswith("kJ5A09wU5Tc")
ca=urn_mlet(jcs(sm)); assert ca=="urn:mlet:bdyqhmtxg343efvdn34cvh4xacxbfa7keroljucjvcpvg63rtkvhmlqa"
e1={"mlp":"0.1","envelope_id":uuid7(t1,"TV-001 envelope id"),"created":"2026-07-04T10:00:05Z",
 "origin":"origin.example","envelope_to":["novak@target.example"],"medialet":sm}
sh=sign_doc(sk_o,"hop/1",e1,"2026-07-04T10:00:05Z",kid_o)
assert sh["signature"]["value"].startswith("TiQzJ3TU")

# --- TV-006 (MEP-001): custody forward AFTER the Manifest window,
# carrying the forwarder's own `until`. The Manifest still says
# 2026-07-11T10:00:00Z — the author's untouched promise; the custody
# holder separately promises 2026-09-01T00:00:00Z under its own hop
# signature. Effective offer deadline at the recipient: the later.
root_attestation={"origin":"origin.example","envelope_id":e1["envelope_id"],
 "created":"2026-07-04T10:00:05Z","kid":kid_o,"sig":sh["signature"]["value"]}
t6 = datetime(2026,7,12,9,0,0,tzinfo=timezone.utc)
e3={"mlp":"0.1","envelope_id":uuid7(t6,"TV-006 envelope id"),
 "created":"2026-07-12T09:00:00Z","origin":"target.example",
 "envelope_to":["carol@final.example"],"forwarded_by":"novak@target.example",
 "fulfillment_sources":[{"domain":"target.example","until":"2026-09-01T00:00:00Z"},
                        {"domain":"origin.example"}],
 "hops":[root_attestation],"medialet":sm}
sh3=sign_doc(sk_t,"hop/1",e3,"2026-07-12T09:00:00Z",kid_t)
signed_e3={"envelope":e3,"signature":sh3["signature"]}

vector={
 "vector":"TV-006","mep":"MEP-001",
 "description":"Custody forward carrying the declarant's own offer window (until) past the Manifest available_until; the effective offer deadline at the recipient is the later of the two; the custody holder is bound by exactly what it declared (spec 9.5).",
 "signed_custody_envelope":signed_e3,
 "expectations":{
   "manifest_available_until":"2026-07-11T10:00:00Z",
   "declared_until":"2026-09-01T00:00:00Z",
   "declarant":"target.example",
   "effective_deadline":"2026-09-01T00:00:00Z",
   "offered_ref_survives_at":"2026-07-13T00:00:00Z",
   "offered_ref_expires_at":"2026-09-02T00:00:00Z",
   "media_urn":media_urn,
   "medialet_ca":ca
 }
}
open('../vectors/mlp-tv-006.json','w').write(json.dumps(vector,indent=2,ensure_ascii=False))
print("TV-006:", len(jcs(signed_e3)), "canonical bytes,", e3["envelope_id"])
