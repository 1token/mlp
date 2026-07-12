package render

// The Go sanitizer against TV-005 under parsed-tree equality plus
// idempotence — the third implementation through the same corpus
// (Python generator, JS client, Go server), which is exactly the
// cross-language bridge D-94 designed the vector to be.

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type tv005 struct {
	Manifest       []string `json:"manifest"`
	ForeignURNUsed string   `json:"foreign_urn_used"`
	Cases          []struct {
		Name      string `json:"name"`
		Input     string `json:"input"`
		Sanitized string `json:"sanitized"`
	} `json:"cases"`
}

func loadTV005(t *testing.T) *tv005 {
	t.Helper()
	raw, err := os.ReadFile("../../conformance/vectors/mlp-tv-005.json")
	if err != nil {
		t.Fatal(err)
	}
	var v tv005
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return &v
}

func TestGoSanitizerReproducesTV005(t *testing.T) {
	v := loadTV005(t)
	set := map[string]bool{}
	for _, u := range v.Manifest {
		set[u] = true
	}
	if set[v.ForeignURNUsed] {
		t.Fatal("corpus sanity: foreign urn in manifest")
	}
	for _, c := range v.Cases {
		in, err := ParseFragment(c.Input)
		if err != nil {
			t.Fatalf("%s: parse: %v", c.Name, err)
		}
		got := SanitizeTree(in, set)
		wantNodes, err := ParseFragment(c.Sanitized)
		if err != nil {
			t.Fatalf("%s: parse expected: %v", c.Name, err)
		}
		if !TreesEqual(got, wantNodes) {
			t.Fatalf("%s: tree mismatch\n got: %s\nwant: %s", c.Name, SerializeTree(got), SerializeTree(wantNodes))
		}
		// Idempotence (§11.5 step 4) as trees.
		re, err := ParseFragment(SerializeTree(got))
		if err != nil {
			t.Fatalf("%s: reparse: %v", c.Name, err)
		}
		if again := SanitizeTree(re, set); !TreesEqual(got, again) {
			t.Fatalf("%s: idempotence violated", c.Name)
		}
		// The full derivation agrees and does not degrade the corpus.
		res := Derive(c.Input, v.Manifest)
		if res.Degraded {
			t.Fatalf("%s: Derive degraded", c.Name)
		}
		reparsed, _ := ParseFragment(res.RenderForm)
		if !TreesEqual(reparsed, wantNodes) {
			t.Fatalf("%s: render form disagrees", c.Name)
		}
	}
}

func TestCapsDegradeToDerivedText(t *testing.T) {
	v := loadTV005(t)
	flood := strings.Repeat("<p>x</p>", MaxNodes/2+64)
	res := Derive(flood, v.Manifest)
	if !res.Degraded || !strings.HasPrefix(res.DerivedText, "x") {
		t.Fatalf("cap violation must degrade to text: %+v", res.Degraded)
	}
}

func TestDerivedTextReference(t *testing.T) {
	v := loadTV005(t)
	content := `<h1>T</h1><ol start="3"><li>a</li><li>b</li></ol>` +
		`<p><a href="https://x.example/y">link</a></p>` +
		`<p><img src="` + v.Manifest[0] + `" alt="cat"></p>` +
		`<table><tr><td>1</td><td>2</td></tr></table>`
	res := Derive(content, v.Manifest)
	if res.Degraded {
		t.Fatal("must not degrade")
	}
	for _, needle := range []string{"3. a", "4. b", "link <https://x.example/y>", "[image: cat]", "1\t2"} {
		if !strings.Contains(res.DerivedText, needle) {
			t.Fatalf("derived text missing %q:\n%s", needle, res.DerivedText)
		}
	}
	if got := Snippet(res.DerivedText, 24); len(got) > 30 || !strings.HasSuffix(got, "…") {
		t.Fatalf("snippet: %q", got)
	}
}
