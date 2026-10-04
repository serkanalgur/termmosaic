package docsgen

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/widgets/basic"
)

// catalog is the widget list the documentation site promises.
//
// It is written out literally rather than derived from the registry, and that is
// the whole point of the test: a registry that silently lost a widget would still
// generate cleanly, still write a manifest, and still produce a site that quietly
// documents 21 widgets while claiming 22. Deriving the expectation from the thing
// under test would make the test unable to fail.
//
// This is the same 22 the README's category table resolves to: 4 core, 9 forms,
// 4 data, 5 visualisation. `buffer.Buffer` and `layout.Layout` are deliberately
// absent — the first is what widgets draw into, the second a solver — and
// `optionList` is unexported, so none of the three is a widget.
var catalog = []string{
	// Core.
	"Block", "Text", "Paragraph", "Split",
	// Forms.
	"TextInput", "TextArea", "Select", "Checkbox", "Radio",
	"Toggle", "Tabs", "Button", "KeyHint",
	// Data.
	"List", "Table", "Tree", "Pager",
	// Visualisation.
	"ProgressBar", "Gauge", "Meter", "Sparkline", "BarChart",
}

// TestManifestCoversEveryWidget is the drift gate: the manifest must name all 22
// widgets, each exactly once, with a file on disk for each.
//
// A widget added to the framework without a registry entry is invisible to this
// test in one direction and caught in the other by
// TestEveryConstructorIsRegistered; a widget REMOVED from the framework is caught
// here, because the literal catalog still names it.
func TestManifestCoversEveryWidget(t *testing.T) {
	dir := t.TempDir()
	if _, err := Generate(dir); err != nil {
		t.Fatalf("Generate: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("reading the manifest: %v", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("the manifest is not valid JSON: %v", err)
	}

	if m.Count != len(catalog) {
		t.Errorf("manifest reports %d widgets; the catalog is %d. The headline "+
			"count on the site is this number, and a site that says one thing and "+
			"shows another loses trust in every other number on it", m.Count, len(catalog))
	}

	got := make(map[string]bool, len(m.Widgets))
	for _, w := range m.Widgets {
		if got[w.Name] {
			t.Errorf("%s appears in the manifest twice", w.Name)
		}
		got[w.Name] = true
	}
	for _, want := range catalog {
		if !got[want] {
			t.Errorf("%s is in the catalog but not in the manifest; a widget the site "+
				"cannot link is the exact failure this tool exists to prevent", want)
		}
	}
	for name := range got {
		if !contains(catalog, name) {
			t.Errorf("the manifest has %s, which is not in the catalog", name)
		}
	}

	// Every record must point at files that exist and are not empty. A manifest
	// entry with a missing file is worse than no entry: the site links it and the
	// reader gets a 404.
	for _, w := range m.Widgets {
		for _, f := range []string{w.Text, w.HTML} {
			info, err := os.Stat(filepath.Join(dir, f))
			if err != nil {
				t.Errorf("%s: %v", w.Name, err)
				continue
			}
			if info.Size() == 0 {
				t.Errorf("%s: %s is empty", w.Name, f)
			}
		}
		if w.Constructor == "" {
			t.Errorf("%s: no constructor recorded; the site prints it on the widget page", w.Name)
		}
		if len(w.Widths) != 3 || len(w.Heights) != 3 {
			t.Errorf("%s: %d widths and %d heights; the site shows three", w.Name, len(w.Widths), len(w.Heights))
		}
	}

	// index.json is the catalog the site's index page reads, and it must agree
	// with the manifest rather than being a second hand-maintained list.
	rawIndex, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		t.Fatalf("reading the index: %v", err)
	}
	var ix Index
	if err := json.Unmarshal(rawIndex, &ix); err != nil {
		t.Fatalf("the index is not valid JSON: %v", err)
	}
	if ix.Count != m.Count {
		t.Errorf("index.json has %d widgets and manifest.json has %d", ix.Count, m.Count)
	}
	for i, e := range ix.Widgets {
		if i >= len(m.Widgets) {
			break
		}
		if e.Name != m.Widgets[i].Name {
			t.Errorf("index entry %d is %s but manifest entry %d is %s", i, e.Name, i, m.Widgets[i].Name)
		}
	}
}

// TestEntriesAreWellFormed catches the registry-level mistakes that would
// otherwise show up as a subtly wrong picture: a duplicated name, a missing
// closure, and parallel slices that are not parallel.
func TestEntriesAreWellFormed(t *testing.T) {
	seenName := map[string]bool{}
	seenSlug := map[string]bool{}
	for _, e := range Entries() {
		if e.Name == "" || e.Package == "" || e.Constructor == "" || e.Summary == "" {
			t.Errorf("an entry is missing a field: %+v", e)
		}
		if seenName[e.Name] {
			t.Errorf("%s is registered twice", e.Name)
		}
		seenName[e.Name] = true
		if seenSlug[e.Slug()] {
			t.Errorf("%s and another entry share the slug %q, so one overwrites the other", e.Name, e.Slug())
		}
		seenSlug[e.Slug()] = true
		if e.Construct == nil {
			t.Errorf("%s has no Construct closure", e.Name)
		}
		if len(e.Widths) != len(e.Heights) || len(e.Widths) == 0 {
			t.Errorf("%s has %d widths and %d heights", e.Name, len(e.Widths), len(e.Heights))
		}
		for i, h := range e.Heights {
			if h < 3 {
				t.Errorf("%s is %d rows tall at %d columns; under three rows there is "+
					"no room for a border and content, so the capture cannot be honest",
					e.Name, h, e.Widths[i])
			}
		}
	}
}

// TestEveryConstructorIsRegistered is the other half of the drift gate, and the
// half that catches a NEW widget.
//
// It reads the AST of every non-test file under widgets/ and collects the types
// returned by exported New* constructors. Every one of those types must have a
// registry entry. The alternative — asserting only the manifest matches a
// literal list — passes happily when someone adds a twenty-third widget and
// forgets the docs entirely, which is the failure the site is most exposed to.
//
// Pointer results only, because that is what a widget constructor returns;
// form.NewBinding returns a value and is not a widget.
func TestEveryConstructorIsRegistered(t *testing.T) {
	registered := map[string]bool{}
	for _, e := range Entries() {
		registered[e.Name] = true
	}

	fset := token.NewFileSet()
	root := "../../widgets"
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == "widgettest" || info.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			if !strings.HasPrefix(fn.Name.Name, "New") || fn.Type.Results == nil {
				continue
			}
			name, ok := pointerTypeName(fn.Type.Results)
			if !ok {
				continue
			}
			if !registered[name] {
				t.Errorf("%s: %s returns a *%s, which has no docsgen registry entry; "+
					"a widget the site cannot show is a widget the README counts and the "+
					"catalog does not have", path, fn.Name.Name, name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
}

// TestNoCaptureIsBlankOrTrivial is the honesty check.
//
// A capture of an empty widget is worse than no capture: it makes a widget look
// broken, or worse, makes a widget that needs configuration look like it needs
// none. So each capture must paint a meaningful number of cells with something
// other than a space, and must not be almost entirely its own border.
func TestNoCaptureIsBlankOrTrivial(t *testing.T) {
	for _, e := range Entries() {
		w := e.Widths[len(e.Widths)-1]
		h := e.Heights[len(e.Heights)-1]
		sink, _, _, err := renderOne(e, w, h)
		if err != nil {
			t.Errorf("%s: %v", e.Name, err)
			continue
		}

		painted, glyphs := countPainted(sink)
		// Block draws nothing but chrome, so it is the one entry whose content is
		// legitimately all border; the threshold is set for the other 21.
		floor := 12
		if e.Name == "Block" {
			floor = 0
		}
		if glyphs < floor {
			t.Errorf("%s at %dx%d painted %d cells with a visible glyph and %d in all; "+
				"a capture this empty misrepresents the widget. Check that Construct "+
				"sets the content, and that any container it builds gets SetBounds",
				e.Name, w, h, glyphs, painted)
		}
	}
}

// TestGenerateIsDeterministic is the property a docs build depends on.
//
// Two runs of the same input must produce byte-identical output, because a
// capture that differs on every run cannot be reviewed: every commit that touched
// the docs would rewrite every file, and the one capture that actually changed
// would be lost in the noise.
func TestGenerateIsDeterministic(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()

	resA, err := Generate(a)
	if err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	resB, err := Generate(b)
	if err != nil {
		t.Fatalf("second Generate: %v", err)
	}

	if resA.Captures != resB.Captures || resA.Bytes != resB.Bytes {
		t.Fatalf("two runs disagreed in size: %d captures/%d bytes then %d captures/%d bytes",
			resA.Captures, resA.Bytes, resB.Captures, resB.Bytes)
	}
	if len(resA.Files) != len(resB.Files) {
		t.Fatalf("two runs wrote %d and %d files", len(resA.Files), len(resB.Files))
	}
	for i, name := range resA.Files {
		if name != resB.Files[i] {
			t.Fatalf("file %d is %q in the first run and %q in the second", i, name, resB.Files[i])
		}
		x, err := os.ReadFile(filepath.Join(a, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		y, err := os.ReadFile(filepath.Join(b, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if string(x) != string(y) {
			t.Errorf("%s differs between two runs of the same input; the output is "+
				"not a function of the code and cannot be diffed", name)
		}
	}
}

// TestHTMLIsEscaped checks the one thing the HTML emitter could get wrong in a
// way no assertion on cells would catch: a widget whose content contains a
// character that means something to a browser.
//
// The capture renderer builds markup from application text, so an unescaped
// ampersand or angle bracket in a label would break the page — and a docs site
// is exactly the place where that would be noticed and blamed on the framework.
func TestHTMLIsEscaped(t *testing.T) {
	e := Entry{
		Name:        "Escape",
		Package:     "widgets/basic",
		Constructor: "basic.NewTextString(r, s, st) *basic.Text",
		Summary:     "renders <script>alert(1)</script> & an ampersand",
		Widths:      []int{40},
		Heights:     []int{3},
		Construct: func(r buffer.Rect) termmosaic.Widget {
			return basic.NewTextString(r, `a < b && c > d "quoted"`, styBody)
		},
	}
	sink, _, _, err := renderOne(e, 40, 3)
	if err != nil {
		t.Fatalf("capturing: %v", err)
	}
	out := renderHTML(e, []capture{{entry: e, width: 40, height: 3, sink: sink}})
	if strings.Contains(out, "<script>") {
		t.Error("the page contains a literal <script>; text from a widget is not escaped")
	}
	if !strings.Contains(out, "&lt;") || !strings.Contains(out, "&amp;") {
		t.Error("angle brackets and ampersands from widget text were not escaped")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// contains reports whether list holds want.
func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// pointerTypeName returns the type name of a single pointer result.
func pointerTypeName(fields *ast.FieldList) (string, bool) {
	if fields == nil || len(fields.List) != 1 {
		return "", false
	}
	star, ok := fields.List[0].Type.(*ast.StarExpr)
	if !ok {
		return "", false
	}
	ident, ok := star.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// countPainted returns the number of cells in the grid and the number of those
// holding a visible glyph.
//
// "Painted" counts every cell, including blanks, and "glyphs" counts only the
// ones with something other than a space, so a widget that fills its rectangle
// with background colour scores high on the first and low on the second — which
// is the distinction between a real capture and a coloured rectangle.
func countPainted(sink *headless.MemorySink) (painted, glyphs int) {
	w, h := sink.Size()
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := sink.CellAt(x, y)
			if c.IsContinuation() {
				continue
			}
			painted++
			if r := cellRune(c); r != ' ' {
				glyphs++
			}
		}
	}
	return painted, glyphs
}
