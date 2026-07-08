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

# --- E2: target.example auto-forwards to carol@final.example at 10:00:07Z
root_attestation={"origin":"origin.example","envelope_id":e1["envelope_id"],
 "created":"2026-07-04T10:00:05Z","kid":kid_o,"sig":sh["signature"]["value"]}
e2={"mlp":"0.1","envelope_id":uuid7(datetime(2026,7,4,10,0,7,tzinfo=timezone.utc),"TV-004 envelope id"),
 "created":"2026-07-04T10:00:07Z","origin":"target.example",
 "envelope_to":["carol@final.example"],"forwarded_by":"novak@target.example",
 "fulfillment_sources":[{"domain":"origin.example"}],
 "hops":[root_attestation],"medialet":sm}
sh2=sign_doc(sk_t,"hop/1",e2,"2026-07-04T10:00:07Z",kid_t)
signed_e2={"envelope":e2,"signature":sh2["signature"]}

# --- delegation request: final.example -> origin.example at 11:00:00Z
token=b64url(blake3.blake3(b"TV-004 reservation token").digest()[:24])
ingest="https://bs.final.example/ingest/"+blake3.blake3(b"TV-004 ingest path").hexdigest()[:16]
dreq={"mlp":"0.1","request_id":uuid7(datetime(2026,7,4,11,0,0,tzinfo=timezone.utc),"TV-004 delegation request"),
 "created":"2026-07-04T11:00:00Z","requester":"final.example",
 "root":root_attestation,"medialet_ca":ca,
 "media":[{"urn":media_urn,"reservation":{"urn":media_urn,"max_size":36,
   "target_url":ingest,"token":token,"expires":"2026-07-07T11:00:00Z"}}]}
sd=sign_doc(sk_f,"delegation/1",dreq,"2026-07-04T11:00:00Z",kid_f)
signed_dreq={"payload":dreq,"signature":sd["signature"]}

dd_final={"domain":"final.example","mlp":["0.1"],"sn":"https://mlp.final.example/sn",
 "keys":[{"kid":kid_f,"alg":"ed25519","key":key_field(P(sk_f)),"roles":["sn","bs"]}]}

vector={"vector":"TV-004",
 "description":"Forwarding + delegated fulfillment: target.example auto-forwards the TV-001 envelope (Signed Medialet byte-identical, root attestation = TV-001 hop signature) to carol@final.example before holding custody; final.example presents the root attestation directly to origin.example via delegation/1 with a Reservation for its own BS; origin validates against its dispatch records and answers will-push. final.example key = RFC 8032 Ed25519 TEST 1024.",
 "final_key":{"seed_hex":"f5e5767cf153319517630f226876b86c8160cc583bc013744c6bf255f5cc0ee5",
  "public_hex":P(sk_f).hex(),"key":key_field(P(sk_f)),"kid":kid_f},
 "final_domain_document":dd_final,
 "signed_forwarded_envelope":signed_e2,
 "forwarded_envelope_canonical_size":len(jcs(signed_e2)),
 "delegation_sig_input_jcs":sd["input"].decode(),
 "signed_delegation_request":signed_dreq,
 "delegation_request_canonical_size":len(jcs(signed_dreq)),
 "fulfill_response_unsigned":{"media":[{"urn":media_urn,"status":"will-push"}]},
 "continuity_assertions":{"medialet_content_address":ca,
  "root_attestation_sig_equals_tv001_hop_sig":True,
  "push_then_proceeds_as":"TV-003 shape against bs.final.example"}}
open('../vectors/mlp-tv-004.json','w').write(json.dumps(vector,indent=2,ensure_ascii=False))
print("tv-004 regenerated")
