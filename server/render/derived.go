package render

import (
	"regexp"
	"strconv"
	"strings"
)

// DerivedText is the §11.6 reference algorithm (D-95) over the tree —
// the snippet source (D-132), the D-165 junk payload, the D-21
// classifier input. Kept in lockstep with client/lib/derived-text.js.

var blocks = map[string]bool{
	"p": true, "div": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "blockquote": true, "pre": true, "ul": true,
	"ol": true, "dl": true, "dt": true, "dd": true, "table": true,
	"caption": true, "thead": true, "tbody": true, "tfoot": true,
	"figure": true, "figcaption": true, "hr": true,
}

var reManyNewlines = regexp.MustCompile(`\n{3,}`)

// DerivedText renders the tree to deterministic plain text.
func DerivedText(nodes []*Node) string {
	out := renderText(nodes, nil)
	out = reManyNewlines.ReplaceAllString(out, "\n\n")
	return strings.Trim(out, "\n")
}

// renderText walks with an ordered-list counter context (nil = ul).
func renderText(nodes []*Node, ol *int) string {
	var b strings.Builder
	for _, n := range nodes {
		if n.Kind == 't' {
			b.WriteString(n.Text)
			continue
		}
		if n.Kind != 'e' {
			continue
		}
		switch n.Tag {
		case "br":
			b.WriteByte('\n')
		case "img":
			b.WriteString("[image: " + n.Attrs["alt"] + "]")
		case "a":
			text := renderText(n.Kids, ol)
			href := n.Attrs["href"]
			if href != "" && !strings.HasPrefix(href, "urn:mlet:") && !strings.HasPrefix(href, "#") {
				b.WriteString(text + " <" + href + ">")
			} else {
				b.WriteString(text)
			}
		case "ul":
			b.WriteString("\n" + renderText(n.Kids, nil) + "\n")
		case "ol":
			start := 1
			if reInt.MatchString(n.Attrs["start"]) {
				start, _ = strconv.Atoi(n.Attrs["start"])
			}
			counter := start
			b.WriteString("\n" + renderText(n.Kids, &counter) + "\n")
		case "li":
			prefix := "- "
			if ol != nil {
				prefix = strconv.Itoa(*ol) + ". "
				*ol++
			}
			b.WriteString(prefix + strings.TrimSpace(renderText(n.Kids, nil)) + "\n")
		case "tr":
			var cells []string
			for _, k := range n.Kids {
				if k.Kind == 'e' && (k.Tag == "td" || k.Tag == "th") {
					cells = append(cells, strings.TrimSpace(renderText(k.Kids, nil)))
				}
			}
			b.WriteString(strings.Join(cells, "\t") + "\n")
		default:
			inner := renderText(n.Kids, ol)
			if blocks[n.Tag] {
				b.WriteString("\n" + inner + "\n")
			} else {
				b.WriteString(inner)
			}
		}
	}
	return b.String()
}

// Snippet is the D-132 rollup projection: the first line-ish run of
// the derived text, capped.
func Snippet(derived string, max int) string {
	s := strings.Join(strings.Fields(derived), " ")
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	if i := strings.LastIndexByte(cut, ' '); i > max/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
