package termmosaic

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// repoRoot is where the module-wide source scan starts. This package IS the
// module root — termmosaic.go sits at the top level — and a test runs with its
// working directory set to its own package directory, so "." is the root.
//
// It must not be "..": that is Projects/, which is not ours to walk and does not
// terminate promptly.
const repoRoot = "."

// forbiddenDecls are the names ADR 0007 §1 rule 4, ADR 0008 and ADR 0009
// reserve for the framework. A widget that defines one of them has re-invented
// shared vocabulary, which is the exact failure all three exist to prevent:
// three authors, three definitions, and a fix that is a signature change on
// every widget rather than an edit inside one.
var forbiddenDecls = []string{
	// ADR 0007 §1: responsive helpers.
	"clamp", "fit", "minRows", "Priority", "Region", "Budget",
	"ClampCount", "PrioLow", "PrioNormal", "PrioHigh", "PrioAlways",
	// ADR 0008: styling, text and border helpers.
	"Style", "Span", "Wrapped", "NewSpan", "SpansWidth",
	"Wrap", "Truncate", "TruncateASCII", "truncate", "truncateString",
	"TruncSuffix", "AscTruncSuffix",
	"BorderStyle", "BorderGlyphs", "BorderPlain", "BorderRounded",
	"BorderDouble", "BorderThick", "BorderASCII",
	// ADR 0009: the command layer. Chord, Command and Binding are the three a
	// widget is most likely to define privately — "chord" for its own key
	// matching, "command" for its own button click, "binding" for its own row —
	// and a widget that defines one has a private version of the framework's,
	// which is the exact failure ADR 0007 §1 rule 4 exists to prevent.
	"Command", "CommandID", "Ctx", "Scope", "ScopeGlobal", "ScopeScreen", "ScopeFocus",
	"Chord", "ParseChord", "ChordOf", "Binding", "Entry", "Registry",
	"Commandable", "Clickable",
	// A private glyph table of any other name. Border thresholds are NOT here:
	// ADR 0007 §1 rule 5 says thresholds are local named constants, and ADR 0008
	// fixes their VALUES (W >= 2 for a border, W >= 5 for a title) without making
	// the constant itself framework vocabulary. The example's constants are
	// pinned to those values by a test in its own package.
	"glyphs", "borderGlyphs", "cornerRunes", "dividerRunes", "boxRunes",
}

// declPatterns extract top-level declarations. Go files are scanned as text
// because the point is to catch a declaration in a package that is not supposed
// to have one, which is a source-level rule rather than a type-level one.
var declPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?m)^func\s+([A-Za-z_]\w*)\s*[\(\[]`),
	regexp.MustCompile(`(?m)^type\s+([A-Za-z_]\w*)\b`),
	regexp.MustCompile(`(?m)^(?:const|var)\s+([A-Za-z_]\w*)\b`),
	// Grouped const/var blocks: "const (" then a line of names.
	regexp.MustCompile(`(?m)^(?:const|var)\s*\(([\s\S]*?)^\)`),
}

// vocabExceptions are the files allowed to declare the reserved names, each with
// the reason. The list is deliberately explicit rather than a prefix rule, so
// that adding an exception is a visible, reviewable act.
//
// Note that `render` is here for `clamp`, which predates ADR 0007 and is
// framework-internal clamping of a config value rather than a widget-local
// responsive helper — a different thing that happens to share a name. It is not
// in scope for either rule.
var vocabExceptions = map[string]string{
	"buffer/style.go":         "buffer owns Style",
	"buffer/span.go":          "buffer owns Span, Wrapped, Wrap and Truncate",
	"buffer/border.go":        "buffer owns the border glyph table",
	"buffer/colour.go":        "buffer owns Colour and its sentinels",
	"geometry/geometry.go":    "geometry is the leaf both buffer and layout import",
	"geometry/align.go":       "geometry owns Align: cell-free geometry with no dependencies",
	"render/render.go":        "framework-internal config clamping, predates ADR 0007",
	"internal/ansi/ansi.go":   "internal; ansi.Style is an alias of buffer.Style, not a declaration",
	"keymap/command.go":       "keymap owns Command, CommandID and Ctx",
	"keymap/scope.go":         "keymap owns Scope and its three values",
	"keymap/chord.go":         "keymap owns Chord, ParseChord and ChordOf",
	"keymap/registry.go":      "keymap owns Binding and Registry",
	"keymap/describe.go":      "keymap owns Entry and the discoverability queries",
	"keymap/participation.go": "keymap owns Commandable and Clickable; they cannot live in widget.go, because their method sets name keymap types and the root package must not import this one",
	// The two collisions ADR 0009 names and accepts rather than renames.
	//
	// form.Binding is a display pair (a key label and a description) and
	// keymap.Binding is a chord reaching a command. Different things, different
	// packages, no assignability either way — the ansi.Style collision shape ADR
	// 0008 removed. ADR 0009 §8 defers reconciling them to the next change to
	// widgets/form that touches KeyHint, because renaming a shipped widget's
	// exported type in the same change that introduces a second one is worse
	// than the collision.
	//
	// docsgen.Entry is a widget-catalog row for the documentation generator and
	// predates the command layer; it is in internal/ and shares nothing with
	// keymap.Entry but the noun.
	"widgets/form/keyhint.go":     "form.Binding is the display pair ADR 0009 §8 explicitly leaves in place",
	"internal/docsgen/docsgen.go": "docsgen.Entry is a documentation-catalog row, unrelated to keymap.Entry",
}

// TestNoPackageRedefinesSharedVocabulary is the enforceable form of ADR 0007 §1
// rule 4 and ADR 0008's extension of it. Nothing in the type system prevents a
// widget from defining its own Wrap, so the rule is asserted in a test that
// fails when someone does.
func TestNoPackageRedefinesSharedVocabulary(t *testing.T) {
	forbidden := make(map[string]bool, len(forbiddenDecls))
	for _, name := range forbiddenDecls {
		forbidden[name] = true
	}

	for _, file := range goFiles(t) {
		rel := relToRoot(t, file)
		if _, excused := vocabExceptions[rel]; excused {
			continue
		}
		src := readFile(t, file)
		seen := declaredNames(src)
		for _, name := range sortedKeys(seen) {
			if forbidden[name] {
				t.Errorf("%s declares %s; the shared vocabulary lives in buffer and geometry. "+
					"If a shared helper is missing something, that is a bug in the ADR, not a private helper here",
					rel, name)
			}
		}
	}
}

// boxDrawingRanges are the Unicode blocks from which a BORDER rune can be drawn.
//
// Scoped to Box Drawing (U+2500-257F) on purpose. Block Elements (U+2580-259F,
// the shade and half-block characters) and Geometric Shapes (U+25A0-25FF) are
// excluded because they are legitimately used for texture — a progress bar, a
// heatmap, a diff scene's background — and are not borders. Including them would
// have flagged the diff benchmark's scene fill and the input decoder's bullet, so
// the rule would have been noise rather than a rule.
var boxDrawingRanges = []runeRange{
	{0x2500, 0x257F}, // Box Drawing
}

// boxDrawingOwner is the one file permitted to contain a box-drawing rune
// literal. buffer/border_test.go is listed with it because a test that asserts on
// the table has to be able to name the runes it is asserting about.
var boxDrawingOwner = map[string]bool{
	"buffer/border.go":      true,
	"buffer/border_test.go": true,
}

// exampleFiles are the per-widget godoc examples. Each one's // Output comment is
// the ASSERTION the example is verified against — the cell grid the renderer
// produced — so a widget whose chrome is a border necessarily writes box-drawing
// runes into its source. That is the same reason buffer/border_test.go is listed
// above: a test that asserts on what was painted has to be able to name the runes
// it painted.
//
// They are listed here rather than matched by suffix so that adding a widget
// package's example is a deliberate act at the same place the rule is written
// down, rather than something a new file does silently by being named well.
var exampleFiles = []string{
	"widgets/basic/example_test.go",
	"widgets/block/example_test.go",
	"widgets/data/example_test.go",
	"widgets/dialog/example_test.go",
	"widgets/form/example_test.go",
	"widgets/menu/example_test.go",
	"widgets/split/example_test.go",
	"widgets/viz/example_test.go",
}

// isExampleFile reports whether rel is one of the per-widget examples named
// above, which are excepted from the literal scan for the reason given there.
func isExampleFile(rel string) bool {
	for _, f := range exampleFiles {
		if rel == f {
			return true
		}
	}
	return false
}

// TestBoxDrawingRunesLiveInOneFile is the rule the whole border vocabulary
// exists to enforce: exactly one glyph table in the repository.
//
// A second table means "BorderThick" is a different rune in a Table than it is in
// a Block, and nothing catches it because both compile. The literals here are
// found by decoding each .go file as UTF-8, so a rune written as an escape
// sequence in a non-border file is not covered — the escape would have to be
// deliberately written to defeat this, at which point it is a comment away from
// being caught by a human.
func TestBoxDrawingRunesLiveInOneFile(t *testing.T) {
	for _, file := range goFiles(t) {
		rel := relToRoot(t, file)
		if boxDrawingOwner[rel] || isExampleFile(rel) {
			continue
		}
		src := readFile(t, file)
		for i, r := range src {
			if !inBoxDrawingRanges(r) {
				continue
			}
			line := lineOf(src, i)
			t.Errorf("%s:%d contains the box-drawing rune %U (%s); every border glyph must come "+
				"from buffer.BorderStyle.Glyphs, never from a literal. Use Glyphs(ascii) and let "+
				"the table supply it", rel, line, r, strconv.QuoteToASCII(string(r)))
		}
	}
}

// TestBorderGlyphTableIsTheOnlyTable is the same rule stated structurally: no
// other package may declare a variable or type whose name suggests it holds
// border runes. The literal scan above is the primary guard; this catches a table
// assembled from arithmetic or from strconv calls, which the literal scan cannot.
//
// Test files are skipped here because the literal scan already covers them, and a
// test function named after the rule would otherwise trip its own regexp.
func TestBorderGlyphTableIsTheOnlyTable(t *testing.T) {
	suspect := regexp.MustCompile(`(?i)(border|corner|divider|box)[A-Za-z]*(runes?|glyphs?|chars?)`)
	for _, file := range goFiles(t) {
		rel := relToRoot(t, file)
		if boxDrawingOwner[rel] || isExampleFile(rel) || strings.HasSuffix(rel, "_test.go") {
			continue
		}
		for _, name := range sortedKeys(declaredNames(readFile(t, file))) {
			if suspect.MatchString(name) && !strings.Contains(name, "Style") {
				t.Errorf("%s declares %s, which looks like a private border glyph table; "+
					"buffer.BorderStyle is the only one", rel, name)
			}
		}
	}
}

// TestGeometryImportsNothing pins the reason Align could go in geometry and Style
// could not: geometry is the leaf. If it ever imports buffer, the "lowest package
// that can hold it without an import cycle" rule stops being checkable and the
// next author has to guess where the next shared type goes.
func TestGeometryImportsNothing(t *testing.T) {
	for _, file := range goFiles(t) {
		rel := relToRoot(t, file)
		if !strings.HasPrefix(rel, "geometry/") {
			continue
		}
		for _, imp := range imports(readFile(t, file)) {
			if strings.Contains(imp, "github.com/serkanalgur/termmosaic") {
				t.Errorf("%s imports %s; geometry must stay a dependency-free leaf", rel, imp)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// runeRange is a closed interval of code points, mirroring geometry's own table.
type runeRange struct{ lo, hi rune }

// inBoxDrawingRanges reports whether r falls in any of the given intervals.
func inBoxDrawingRanges(r rune) bool {
	for _, rg := range boxDrawingRanges {
		if r >= rg.lo && r <= rg.hi {
			return true
		}
	}
	return false
}

// goFiles returns every .go file in the module, excluding testdata.
func goFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == ".git" || base == "testdata" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", repoRoot, err)
	}
	if len(out) == 0 {
		t.Fatalf("found no .go files under %s; the scan is not looking at anything", repoRoot)
	}
	return out
}

// relToRoot converts a walked path into a module-relative slash path, e.g.
// "buffer/border.go". Walk starts at repoRoot (".."), so its paths carry a
// leading "../" that is stripped rather than resolved: the module directory name
// varies with where the repository is checked out and must not appear in a
// message.
func relToRoot(t *testing.T, path string) string {
	t.Helper()
	rel := filepath.ToSlash(path)
	for strings.HasPrefix(rel, "./") {
		rel = rel[len("./"):]
	}
	if rel == "" || strings.HasPrefix(rel, "..") {
		t.Fatalf("path %s is outside the module", path)
	}
	return rel
}

// readFile returns the contents of path, failing the test if it cannot be read.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

// declaredNames returns every top-level name src declares, including the
// individual names inside grouped const and var blocks.
func declaredNames(src string) map[string]bool {
	names := make(map[string]bool)
	consumed := make([]bool, len(declPatterns))

	for i, re := range declPatterns {
		locs := re.FindAllStringSubmatchIndex(src, -1)
		if i == len(declPatterns)-1 {
			// The grouped const/var pattern: each capture is a whole block, whose
			// lines are "Name = value" or a bare "Name" on its own line.
			for _, loc := range locs {
				block := src[loc[2]:loc[3]]
				for _, line := range strings.Split(block, "\n") {
					line = strings.TrimSpace(line)
					if line == "" || strings.HasPrefix(line, "//") {
						continue
					}
					name, _, _ := strings.Cut(line, "=")
					name = strings.TrimSpace(strings.Fields(name + " ")[0])
					if isIdent(name) {
						names[name] = true
					}
				}
			}
			continue
		}
		for _, loc := range locs {
			names[src[loc[2]:loc[3]]] = true
			consumed[i] = true
		}
	}
	_ = consumed
	return names
}

// isIdent reports whether s is a plain identifier and not a keyword or blank.
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

// imports returns the import paths in a Go source file's import block.
func imports(src string) []string {
	start := strings.Index(src, "import (")
	if start < 0 {
		return nil
	}
	end := strings.Index(src[start:], "\n)")
	if end < 0 {
		return nil
	}
	var out []string
	for _, line := range strings.Split(src[start+len("import ("):start+end], "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		out = append(out, strings.Trim(line, `"`))
	}
	return out
}

// lineOf returns the 1-based line number of byte offset i in src.
func lineOf(src string, i int) int {
	return 1 + strings.Count(src[:i], "\n")
}

// sortedKeys returns m's keys in sorted order, so failure messages are stable.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// sortStrings is a tiny insertion sort, so this file adds no import to a package
// whose import graph other tests assert on.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
