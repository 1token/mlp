// Package render implements the server-side half of the §11
// sanitization duty (D-94): deriving the render form and derived
// text at ingest and at send. Parsing is golang.org/x/net/html —
// spec-compliant HTML5 tree construction in body-fragment context,
// exactly what §11.5 step 1 demands. The pipeline mirrors
// client/lib/sanitizer.js over the same tree model; TV-005 under
// parsed-tree equality is the cross-language bridge holding the two
// (and the Python vector generator) together.
package render

import (
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Node is the language-neutral tree (mirrors the JS model).
type Node struct {
	Kind  byte // 'e' element, 't' text, 'c' comment
	Tag   string
	Attrs map[string]string
	Kids  []*Node
	Text  string
}

// --- §11.2 tables (kept in lockstep with client/lib/sanitizer.js) ------

var permitted = map[string][]string{
	"p": {}, "br": {}, "hr": {}, "h1": {}, "h2": {}, "h3": {}, "h4": {}, "h5": {}, "h6": {},
	"blockquote": {}, "pre": {}, "code": {}, "div": {},
	"ul": {}, "ol": {"start"}, "li": {}, "dl": {}, "dt": {}, "dd": {},
	"em": {}, "strong": {}, "b": {}, "i": {}, "u": {}, "s": {}, "sub": {}, "sup": {},
	"mark": {}, "small": {}, "q": {}, "abbr": {}, "dfn": {}, "kbd": {}, "samp": {},
	"var": {}, "del": {}, "ins": {}, "wbr": {}, "span": {}, "time": {"datetime"},
	"table": {}, "caption": {}, "thead": {}, "tbody": {}, "tfoot": {}, "tr": {},
	"th": {"colspan", "rowspan", "scope"}, "td": {"colspan", "rowspan"},
	"a":     {"href"},
	"img":   {"src", "alt", "width", "height"},
	"video": {"src", "poster", "width", "height"},
	"audio": {"src"}, "source": {"src", "type"},
	"figure": {}, "figcaption": {},
}

var dropSet = map[string]bool{
	"script": true, "style": true, "iframe": true, "frame": true, "frameset": true,
	"object": true, "embed": true, "applet": true, "form": true, "input": true,
	"button": true, "textarea": true, "select": true, "option": true, "optgroup": true,
	"label": true, "fieldset": true, "legend": true, "template": true, "svg": true,
	"math": true, "link": true, "meta": true, "base": true, "noscript": true,
	"slot": true, "canvas": true, "dialog": true, "map": true, "area": true,
	"marquee": true,
}

var embeds = map[string]bool{"img": true, "video": true, "audio": true, "source": true}

var voidSet = map[string]bool{
	"area": true, "base": true, "br": true, "col": true, "embed": true, "hr": true,
	"img": true, "input": true, "link": true, "meta": true, "source": true,
	"track": true, "wbr": true,
}

var intAttrs = map[string]bool{"width": true, "height": true, "colspan": true, "rowspan": true, "start": true}

var (
	reInt      = regexp.MustCompile(`^[0-9]{1,6}$`)
	reID       = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	reClass    = regexp.MustCompile(`^[\w\s-]{1,256}$`)
	reLang     = regexp.MustCompile(`^[A-Za-z0-9-]{1,35}$`)
	reFragment = regexp.MustCompile(`^#[A-Za-z][A-Za-z0-9_-]*$`)
	reStyleVal = regexp.MustCompile(`^[A-Za-z0-9#%.,\s-]+$`)
	reHTTPS    = regexp.MustCompile(`^(?i)https://`)
	reMailto   = regexp.MustCompile(`^(?i)mailto:`)
)

var styleProps = map[string]bool{
	"color": true, "background-color": true, "font-size": true, "font-weight": true,
	"font-style": true, "font-family": true, "text-align": true, "text-decoration": true,
	"line-height": true, "letter-spacing": true,
	"margin": true, "margin-top": true, "margin-right": true, "margin-bottom": true, "margin-left": true,
	"padding": true, "padding-top": true, "padding-right": true, "padding-bottom": true, "padding-left": true,
	"border": true, "border-top": true, "border-right": true, "border-bottom": true, "border-left": true,
	"border-width": true, "border-style": true, "border-color": true, "border-radius": true,
	"width": true, "max-width": true, "height": true, "max-height": true,
	"display": true, "flex-direction": true, "justify-content": true, "align-items": true, "gap": true,
	"vertical-align": true, "white-space": true, "overflow-wrap": true, "word-break": true,
	"list-style-type": true, "object-fit": true,
}

var displayValues = map[string]bool{"block": true, "inline": true, "inline-block": true, "flex": true}

const (
	styleMax = 2048
	// MaxDepth and MaxNodes are the §11.5 complexity caps.
	MaxDepth = 64
	MaxNodes = 16384
)

// --- parsing (§11.5 step 1: the spec-compliant HTML5 parser) -----------

// ParseFragment parses Body content in body-fragment context.
func ParseFragment(content string) ([]*Node, error) {
	ctx := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(content), ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		if t := fromHTML(n); t != nil {
			out = append(out, t)
		}
	}
	return out, nil
}

func fromHTML(n *html.Node) *Node {
	switch n.Type {
	case html.TextNode:
		return &Node{Kind: 't', Text: n.Data}
	case html.CommentNode:
		return &Node{Kind: 'c', Text: n.Data}
	case html.ElementNode:
		el := &Node{Kind: 'e', Tag: strings.ToLower(n.Data), Attrs: map[string]string{}}
		for _, a := range n.Attr {
			if a.Namespace != "" {
				continue
			}
			name := strings.ToLower(a.Key)
			if _, dup := el.Attrs[name]; !dup { // first occurrence wins (HTML5)
				el.Attrs[name] = a.Val
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if t := fromHTML(c); t != nil {
				el.Kids = append(el.Kids, t)
			}
		}
		return el
	default:
		return nil
	}
}

// --- the pipeline --------------------------------------------------------

// Measure counts nodes and depth.
func Measure(nodes []*Node) (count, depth int) {
	var walk func(ns []*Node, d int)
	walk = func(ns []*Node, d int) {
		if d > depth {
			depth = d
		}
		for _, n := range ns {
			count++
			if n.Kind == 'e' {
				walk(n.Kids, d+1)
			}
		}
	}
	walk(nodes, 1)
	return
}

// SanitizeTree is the §11.5 step-2 filter (pure; never errors).
func SanitizeTree(nodes []*Node, manifest map[string]bool) []*Node {
	out := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		switch n.Kind {
		case 't':
			out = append(out, &Node{Kind: 't', Text: n.Text})
		case 'e':
			tag := n.Tag
			if dropSet[tag] {
				continue
			}
			kids := SanitizeTree(n.Kids, manifest)
			specific, ok := permitted[tag]
			if !ok {
				out = append(out, kids...) // unwrap
				continue
			}
			attrs, keep := filterAttrs(tag, specific, n.Attrs, manifest)
			if !keep {
				continue // embed with invalid or absent src (§11.4)
			}
			out = append(out, &Node{Kind: 'e', Tag: tag, Attrs: attrs, Kids: kids})
		}
	}
	return out
}

func filterAttrs(tag string, specific []string, attrs map[string]string, manifest map[string]bool) (map[string]string, bool) {
	out := map[string]string{}
	isSpecific := func(name string) bool {
		for _, s := range specific {
			if s == name {
				return true
			}
		}
		return false
	}
	for name, value := range attrs {
		switch name {
		case "name":
			// permitted nowhere (DOM clobbering)
		case "title":
			if value != "" {
				out["title"] = value
			}
		case "dir":
			if value == "ltr" || value == "rtl" || value == "auto" {
				out["dir"] = value
			}
		case "lang":
			if reLang.MatchString(value) {
				out["lang"] = value
			}
		case "class":
			if reClass.MatchString(value) {
				out["class"] = value
			}
		case "id":
			if reID.MatchString(value) {
				out["id"] = value
			}
		case "style":
			if s, ok := FilterStyle(value); ok {
				out["style"] = s
			}
		case "href":
			if isSpecific("href") && hrefAllowed(value, manifest) {
				out["href"] = value
			}
		case "src", "poster":
			if isSpecific(name) && strings.HasPrefix(value, "urn:mlet:") && manifest[value] {
				out[name] = value
			}
		default:
			if !isSpecific(name) {
				continue
			}
			if intAttrs[name] {
				if reInt.MatchString(value) {
					out[name] = value
				}
			} else {
				out[name] = value // alt, type, scope, datetime
			}
		}
	}
	if embeds[tag] {
		if _, ok := out["src"]; !ok {
			return nil, false
		}
	}
	if tag == "img" {
		if _, ok := out["alt"]; !ok {
			out["alt"] = ""
		}
	}
	return out, true
}

// FilterStyle applies §11.3; ok=false drops the attribute.
func FilterStyle(value string) (string, bool) {
	if len(value) > styleMax {
		return "", false
	}
	var out []string
	for _, decl := range strings.Split(value, ";") {
		colon := strings.IndexByte(decl, ':')
		if colon < 0 {
			continue
		}
		prop := strings.ToLower(strings.TrimSpace(decl[:colon]))
		val := strings.TrimSpace(decl[colon+1:])
		if !styleProps[prop] || val == "" || !reStyleVal.MatchString(val) {
			continue
		}
		if prop == "display" && !displayValues[val] {
			continue
		}
		out = append(out, prop+":"+val)
	}
	if len(out) == 0 {
		return "", false
	}
	return strings.Join(out, ";"), true
}

func hrefAllowed(href string, manifest map[string]bool) bool {
	if reHTTPS.MatchString(href) || reMailto.MatchString(href) {
		return true
	}
	if strings.HasPrefix(href, "urn:mlet:") {
		return manifest[href]
	}
	return reFragment.MatchString(href)
}

// --- serialization and equality -----------------------------------------

var textEsc = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
var attrEsc = strings.NewReplacer("&", "&amp;", `"`, "&quot;")

// SerializeTree emits HTML5 fragment serialization (attributes in
// sorted order — order is presentation-neutral; D-94 comparison is
// tree equality, and deterministic bytes make the stored render form
// reproducible).
func SerializeTree(nodes []*Node) string {
	var b strings.Builder
	var walk func(ns []*Node)
	walk = func(ns []*Node) {
		for _, n := range ns {
			switch n.Kind {
			case 't':
				b.WriteString(textEsc.Replace(n.Text))
			case 'e':
				b.WriteByte('<')
				b.WriteString(n.Tag)
				names := make([]string, 0, len(n.Attrs))
				for k := range n.Attrs {
					names = append(names, k)
				}
				sort.Strings(names)
				for _, k := range names {
					b.WriteByte(' ')
					b.WriteString(k)
					b.WriteString(`="`)
					b.WriteString(attrEsc.Replace(n.Attrs[k]))
					b.WriteByte('"')
				}
				b.WriteByte('>')
				if voidSet[n.Tag] {
					continue
				}
				walk(n.Kids)
				b.WriteString("</")
				b.WriteString(n.Tag)
				b.WriteByte('>')
			}
		}
	}
	walk(nodes)
	return b.String()
}

// TreesEqual is the D-94 comparator: tags/text exact, attribute maps
// unordered, children ordered.
func TreesEqual(a, b []*Node) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Kind != y.Kind {
			return false
		}
		switch x.Kind {
		case 't', 'c':
			if x.Text != y.Text {
				return false
			}
		case 'e':
			if x.Tag != y.Tag || len(x.Attrs) != len(y.Attrs) {
				return false
			}
			for k, v := range x.Attrs {
				if y.Attrs[k] != v {
					return false
				}
			}
			if !TreesEqual(x.Kids, y.Kids) {
				return false
			}
		}
	}
	return true
}

// --- the checked derivation (§11.5 steps 2–6) ----------------------------

// Result is one derivation: the render form (or the degraded text)
// plus the §11.6 derived text, which every path yields.
type Result struct {
	RenderForm  string // "" when Degraded
	DerivedText string
	Degraded    bool
}

// Derive runs the full pipeline over Body content: parse (step 1),
// caps (step 5), filter (step 2), the REQUIRED idempotence fixpoint
// (step 4), degradation to derived text (step 6).
func Derive(content string, manifest []string) Result {
	set := make(map[string]bool, len(manifest))
	for _, u := range manifest {
		set[u] = true
	}
	nodes, err := ParseFragment(content)
	if err != nil {
		return Result{Degraded: true, DerivedText: content}
	}
	if count, depth := Measure(nodes); count > MaxNodes || depth > MaxDepth {
		return Result{Degraded: true, DerivedText: DerivedText(nodes)}
	}
	clean := SanitizeTree(nodes, set)
	reparsed, err := ParseFragment(SerializeTree(clean))
	if err != nil {
		return Result{Degraded: true, DerivedText: DerivedText(nodes)}
	}
	again := SanitizeTree(reparsed, set)
	if !TreesEqual(clean, again) || !TreesEqual(clean, reparsed) {
		// The mutation seam: retreat, not hope (D-94).
		return Result{Degraded: true, DerivedText: DerivedText(nodes)}
	}
	return Result{RenderForm: SerializeTree(clean), DerivedText: DerivedText(clean)}
}
