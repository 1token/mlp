import json, re, base64
import blake3
from bs4 import BeautifulSoup, Comment, Tag, NavigableString

def b32l(b): return base64.b32encode(b).decode().lower().rstrip("=")
def urn_mlet(d): return "urn:mlet:b" + b32l(bytes([0x1E,0x20]) + blake3.blake3(d).digest())

URN_OK = "urn:mlet:bdyqbcgucl4thqvg5tqzv66l4q6myvy3j647jojvokzpw2c4ohcctu6y"  # TV-001 object
URN_FOREIGN = urn_mlet(b"TV-005 foreign object")  # syntactically valid, not in Manifest
MANIFEST = {URN_OK}

GLOBAL = {"title", "dir", "lang", "class", "style", "id"}
ALLOWED = {
 "p": set(), "br": set(), "hr": set(),
 "h1": set(), "h2": set(), "h3": set(), "h4": set(), "h5": set(), "h6": set(),
 "blockquote": set(), "pre": set(), "code": set(),
 "ul": set(), "ol": {"start"}, "li": set(), "dl": set(), "dt": set(), "dd": set(),
 "em": set(), "strong": set(), "b": set(), "i": set(), "u": set(), "s": set(),
 "sub": set(), "sup": set(), "mark": set(), "small": set(), "q": set(),
 "abbr": set(), "dfn": set(), "kbd": set(), "samp": set(), "var": set(),
 "del": set(), "ins": set(), "wbr": set(), "span": set(), "div": set(),
 "time": {"datetime"},
 "table": set(), "caption": set(), "thead": set(), "tbody": set(), "tfoot": set(),
 "tr": set(), "th": {"colspan", "rowspan", "scope"}, "td": {"colspan", "rowspan"},
 "a": {"href"}, "img": {"src", "alt", "width", "height"},
 "video": {"src", "poster", "width", "height"}, "audio": {"src"},
 "source": {"src", "type"}, "figure": set(), "figcaption": set(),
}
DROP = {"script", "style", "iframe", "frame", "frameset", "object", "embed",
 "applet", "form", "input", "button", "textarea", "select", "option",
 "optgroup", "label", "fieldset", "legend", "template", "svg", "math",
 "link", "meta", "base", "noscript", "slot", "canvas", "dialog", "map",
 "area", "marquee"}

CSS_PROPS = {"color","background-color","font-size","font-weight","font-style",
 "font-family","text-align","text-decoration","line-height","letter-spacing",
 "margin","margin-top","margin-right","margin-bottom","margin-left",
 "padding","padding-top","padding-right","padding-bottom","padding-left",
 "border","border-top","border-right","border-bottom","border-left",
 "border-width","border-style","border-color","border-radius",
 "width","max-width","height","max-height","display","flex-direction",
 "justify-content","align-items","gap","vertical-align","white-space",
 "overflow-wrap","word-break","list-style-type","object-fit"}
CSS_VAL = re.compile(r"^[A-Za-z0-9#%.,\s-]+$")
DISPLAY_OK = {"block","inline","inline-block","flex"}
ID_RE = re.compile(r"^[A-Za-z][A-Za-z0-9_-]*$")
LANG_RE = re.compile(r"^[A-Za-z0-9-]{1,35}$")
INT_ATTRS = {"width","height","colspan","rowspan","start"}

def clean_style(v):
    out = []
    for decl in str(v).split(";"):
        if ":" not in decl: continue
        name, val = decl.split(":", 1)
        name, val = name.strip().lower(), val.strip()
        if name not in CSS_PROPS or not val or not CSS_VAL.match(val): continue
        if name == "display" and val.lower() not in DISPLAY_OK: continue
        out.append(f"{name}:{val}")
    return ";".join(out)

def ok_href(v):
    v = str(v).strip()
    if v.startswith("#"): return bool(ID_RE.match(v[1:]))
    l = v.lower()
    if l.startswith("https://") or l.startswith("mailto:"): return True
    if l.startswith("urn:mlet:"): return v in MANIFEST
    return False

def ok_embed(v):
    v = str(v).strip()
    return v.lower().startswith("urn:mlet:") and v in MANIFEST

def attr_str(el, name):
    v = el.attrs.get(name)
    return " ".join(v) if isinstance(v, list) else v

def filter_attrs(el):
    name = el.name.lower()
    allowed = GLOBAL | ALLOWED[name]
    for a in list(el.attrs):
        if a.lower() not in allowed:
            del el.attrs[a]
    # value validation
    if "style" in el.attrs:
        cs = clean_style(attr_str(el, "style"))
        if cs: el.attrs["style"] = cs
        else: del el.attrs["style"]
    if "id" in el.attrs and not ID_RE.match(str(el.attrs["id"])):
        del el.attrs["id"]
    if "lang" in el.attrs and not LANG_RE.match(str(el.attrs["lang"])):
        del el.attrs["lang"]
    if "dir" in el.attrs and str(el.attrs["dir"]).lower() not in {"ltr","rtl","auto"}:
        del el.attrs["dir"]
    if "class" in el.attrs:
        c = attr_str(el, "class")
        if len(c) > 256 or not re.match(r"^[\w\s-]*$", c): del el.attrs["class"]
        else: el.attrs["class"] = c
    for a in INT_ATTRS & set(el.attrs):
        if not re.match(r"^[0-9]{1,6}$", str(el.attrs[a])): del el.attrs[a]
    # URL rules
    if name == "a" and "href" in el.attrs and not ok_href(el.attrs["href"]):
        del el.attrs["href"]
    if name in ("img", "video", "audio", "source"):
        if not ok_embed(el.attrs.get("src", "")):
            el.decompose(); return
        if name == "video" and "poster" in el.attrs and not ok_embed(el.attrs["poster"]):
            del el.attrs["poster"]
        if name == "img":
            el.attrs["alt"] = el.attrs.get("alt", "")

def walk(node):
    for child in list(node.children):
        if isinstance(child, Comment):
            child.extract(); continue
        if not isinstance(child, Tag):
            continue
        name = child.name.lower()
        if name in DROP:
            child.decompose(); continue
        walk(child)
        if name not in ALLOWED:
            child.unwrap(); continue
        filter_attrs(child)

def sanitize(html):
    soup = BeautifulSoup(html, "html5lib")
    body = soup.body
    walk(body)
    return "".join(str(c) for c in body.contents)

CASES = [
 ("benign_rich",
  '<h1 style="color:#663399">Delivery</h1><p>Hi <strong>Novák</strong> family,</p>'
  '<ul><li>36 photos</li></ul>'
  '<table><thead><tr><th scope="col">File</th></tr></thead>'
  '<tbody><tr><td>sample.txt</td></tr></tbody></table>'
  f'<p><a href="{URN_OK}">sample.txt</a></p>'
  f'<img src="{URN_OK}" alt="preview" width="320">',
  "Fully conformant body passes essentially unchanged."),
 ("script_drop", '<p>before</p><script>alert(1)</script><p>after</p>',
  "script is on the drop-subtree list."),
 ("tracking_pixel", '<p>hello</p><img src="https://tracker.example/p.gif" alt="">',
  "External src: embed elements with non-Manifest src are removed entirely."),
 ("js_href", '<a href="javascript:alert(1)">click</a>',
  "Disallowed scheme: href stripped, text preserved."),
 ("event_attr", '<p onclick="steal()">text</p>',
  "Event attributes are not in any allowlist."),
 ("urn_not_in_manifest", f'<p>see</p><img src="{URN_FOREIGN}" alt="x">',
  "Syntactically valid urn:mlet absent from the Manifest: removed (3.2.3)."),
 ("css_url_smuggle",
  '<div style="color:#333;background-color:url(https://x/y.png);margin-top:4px">t</div>',
  "No-functional-notation grammar drops the url() declaration; others survive."),
 ("svg_mxss", '<p>a</p><svg><circle r="4"></circle></svg><p>b</p>',
  "Foreign content (svg/math) is dropped with its subtree."),
 ("fragment_nav", '<p><a href="#s2">jump</a></p><h2 id="s2">Two</h2>',
  "Fragment navigation within the Medialet is preserved."),
 ("position_overlay", '<div style="position:fixed;top:0;color:#000">overlay</div>',
  "position/top are not allowlisted properties; color survives."),
 ("display_none_hiding", '<span style="display:none">hidden words</span>',
  "display value restricted to block|inline|inline-block|flex; none dropped."),
 ("unwrap_unknown", '<article><p>content</p></article>',
  "Unknown-but-harmless elements are unwrapped, content preserved."),
 ("data_uri", '<img src="data:image/png;base64,AAAA" alt="x">',
  "data: is not urn:mlet:; element removed."),
 ("comment_strip", '<p>keep</p><!-- secret note -->',
  "Comments are removed."),
]

results = []
for name, src, note in CASES:
    out = sanitize(src)
    out2 = sanitize(out)
    assert out == out2, f"idempotence failed: {name}"
    results.append({"name": name, "input": src, "sanitized": out, "notes": note})
    print(f"--- {name}\n IN : {src}\n OUT: {out}\n")

vector = {"vector": "TV-005",
 "description": "mlp-html/1 sanitization corpus. Outputs generated by the prototype reference sanitizer; conformance comparison is parsed-tree equality, not byte equality. Manifest for all cases contains exactly the TV-001 object URN.",
 "manifest": [URN_OK],
 "foreign_urn_used": URN_FOREIGN,
 "idempotence": "verified for every case (sanitize∘sanitize = sanitize)",
 "cases": results}
open("../vectors/mlp-tv-005.json","w").write(json.dumps(vector, indent=2, ensure_ascii=False))
print("foreign urn:", URN_FOREIGN)
