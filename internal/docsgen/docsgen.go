// Package docsgen renders every widget in the catalog and writes the captures
// the documentation site displays.
//
// # Why this exists at all
//
// The site's value proposition is a widget catalog with pictures of what each
// widget actually draws. A picture typed by hand drifts from the code the moment
// a widget changes, and nobody notices until a reader does. So the pictures here
// are not drawn: they are produced by rendering the widget through the same path
// every widget test uses (widgettest.Capture -> widget -> buffer -> renderer ->
// two-tier diff -> ANSI encoder -> headless.MemorySink) and then translating the
// cell grid that falls out.
//
// Two consequences are load-bearing and worth stating plainly:
//
//   - Converting CELLS, not ANSI. An ANSI converter knows only SGR, so it cannot
//     know that a Braille cell is a value rather than a glyph, and it renders a
//     gauge's dial as a scatter of unrelated characters. ADR 0002's Cell is 16
//     bytes carrying Ch, FG, BG and Attr, so cells -> text and cells -> HTML is
//     a field copy and a string build: lossless, and with no third-party
//     dependency added to a module that deliberately has two.
//
//   - Every widget gets a plain-text capture beside the colour one. Braille
//     (U+2800..28FF) and the Block Elements (U+2580..259F) can render at a
//     different advance width in a web font than a terminal gives them, which
//     destroys the alignment that gives a gauge or a sparkline its meaning. The
//     text capture is exact and unaffected by any font, so the shape is always
//     verifiable even when the colour capture misaligns.
//
// # Why an explicit registry rather than reflection
//
// Constructors are variadic and take concrete types — NewList(r, items ...ListItem),
// NewTree(r, nodes ...Node), NewTable(r, cols ...Column) — so reflection cannot call
// them. The registry below is therefore one hand-written closure per widget. That
// is the point of the file: the demo state lives next to the constructor it
// drives, in one readable place, rather than being spread across a generator's
// metadata.
//
// # Determinism
//
// Running Generate twice into two directories produces byte-identical files.
// There is no map iteration in an output path, no timestamp, no hostname and no
// randomness; the manifest is sorted by widget name. A docs build that produces a
// different byte stream on every run cannot be diffed, and a capture that cannot
// be diffed cannot be reviewed.
package docsgen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
	"github.com/serkanalgur/termmosaic/headless"
	"github.com/serkanalgur/termmosaic/widgets/widgettest"
)

// frames is how many frames each capture renders.
//
// Two is enough for a settled picture and one is not: the first Render is what
// populates the renderer's previous-cell state, so a single frame is the honest
// "cold" picture while the second is what a user sees. Every widget here is
// static, so the two are identical and the choice costs nothing — but rendering
// one frame would make the capture a picture of a different code path from the
// one the tests take.
const frames = 2

// captureWidths are the column counts every widget is captured at.
//
// Three fixed widths rather than a live resize, and that is a deliberate upgrade
// on one: a resize shows the same widget at the same moment, whereas these show
// degradation as three exact, named, diffable frames. ADR 0007 is about
// responsiveness, and this is how the site shows it rather than asserting it.
var captureWidths = []int{40, 80, 120}

// Widths returns a fresh copy of the column counts every capture is taken at.
//
// A copy because Entry.Widths is exported and therefore mutable by a caller, and
// a shared backing array would let one caller change every later capture.
func Widths() []int {
	out := make([]int, len(captureWidths))
	copy(out, captureWidths)
	return out
}

// rows returns a per-width height slice of length len(captureWidths), repeating
// h for every width when only one is given.
//
// A single height is the usual case. The optional extra exists for widgets whose
// narrow capture genuinely needs more rows to show the same thing: a table that
// loses two columns at 40 cells should not also lose its rows.
func rows(h ...int) []int {
	out := make([]int, len(captureWidths))
	switch len(h) {
	case 0:
		for i := range out {
			out[i] = 1
		}
	case 1:
		for i := range out {
			out[i] = h[0]
		}
	default:
		copy(out, h)
	}
	return out
}

// Entry is one widget in the catalog: what it is called, how to build it, and a
// demo state worth photographing.
//
// Construct is a closure rather than a func value because the constructors are
// variadic over concrete types and cannot be called generically. The demo state
// belongs with the constructor for the same reason: a picture is only as honest
// as the state that produced it, and the state is the interesting half.
type Entry struct {
	// Name is the widget's Go type name, and the capture file's stem.
	Name string
	// Package is the import path that declares it.
	Package string
	// Constructor is the godoc signature of the constructor used, recorded so the
	// site can print it without parsing Go source.
	Constructor string
	// Summary is the widget's one-line purpose, shown as the capture's caption.
	Summary string
	// Note is an optional honesty note about what the capture cannot show.
	Note string
	// Widths are the column counts to capture at, one per height.
	Widths []int
	// Heights are the row counts to capture at, parallel to Widths.
	Heights []int
	// Construct builds the widget at the given rect, already driven into its demo
	// state. It must not read the environment, the clock or a random source:
	// determinism is a property the package guarantees and this closure is the
	// only place it could be broken.
	Construct func(buffer.Rect) termmosaic.Widget
}

// Slug returns the file-name stem for the entry: the widget name lowercased.
// ProgressBar and TextInput become progressbar and textinput, matching the site
// page names the plan's information architecture uses.
func (e Entry) Slug() string { return strings.ToLower(e.Name) }

// capture is one widget rendered at one width.
type capture struct {
	entry  Entry
	width  int
	height int
	sink   *headless.MemorySink
	// minSize is the widget's own MinSize(), read rather than parsed because the
	// thresholds behind it are private constants.
	minSize    buffer.Size
	hasMinSize bool
}

// Result reports what Generate wrote, for the command's summary line.
type Result struct {
	// Dir is the directory the files were written to.
	Dir string
	// Files are the paths written, relative to Dir, sorted.
	Files []string
	// Captures is how many renderings were taken: one per widget per width. It
	// counts renderings, not files, so 22 widgets at three widths is 66.
	Captures int
	// Bytes is the total size of everything written, including the manifest, the
	// index and the stylesheet.
	Bytes int
}

// Manifest is the machine-readable index of every capture.
//
// The site builds its catalog from this rather than by listing a directory,
// because a file the manifest does not mention is a widget the site will not
// link, and a widget the site does not link is the exact failure the whole
// capture mechanism exists to prevent.
type Manifest struct {
	// Tool names the program that produced this manifest.
	Tool string `json:"tool"`
	// Version is the release of that program, so a capture on disk can be
	// traced to the code that wrote it.
	Version string `json:"version"`
	// Widths are the column counts every widget was captured at.
	Widths []int `json:"widths"`
	// Count is the number of widgets below, and is 22 for this catalog.
	Count int `json:"widgetCount"`
	// Widgets are the per-widget records, sorted by Name.
	Widgets []ManifestWidget `json:"widgets"`
}

// ManifestWidget is one widget's record in the Manifest.
type ManifestWidget struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Package     string `json:"package"`
	Constructor string `json:"constructor"`
	Summary     string `json:"summary"`
	Note        string `json:"note,omitempty"`
	Widths      []int  `json:"widths"`
	Heights     []int  `json:"heights"`
	// MinSize is the widget's own MinSize(), present only when the widget has one.
	// Text and Paragraph do not: they are single-purpose painters with no content
	// below which they would be wrong, and publishing 0x0 for them would be a
	// number the reader would trust and be wrong by.
	MinSize *CellSize `json:"minSize,omitempty"`
	Text    string    `json:"text"`
	// TextBytes and HTMLBytes are the sizes on disk, so the site can show a
	// weight or a size without a stat call.
	TextBytes int    `json:"textBytes"`
	HTML      string `json:"html"`
	HTMLBytes int    `json:"htmlBytes"`
}

// CellSize is a width and a height in cells, as MinSize() reports one.
type CellSize struct {
	W int `json:"w"`
	H int `json:"h"`
}

// Index is the short catalog the site's widget index page reads.
//
// It is deliberately a separate file from the Manifest: the index page wants a
// name and a link, the per-widget page wants the byte counts and the
// constructor. Merging them would make one of the two readers parse fields it
// does not use, and the manifest's job of being exhaustive would drag the index
// along with it.
type Index struct {
	Count   int          `json:"widgetCount"`
	Widgets []IndexEntry `json:"widgets"`
}

// IndexEntry is one widget's line in the Index.
type IndexEntry struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Package     string `json:"package"`
	Constructor string `json:"constructor"`
	Summary     string `json:"summary"`
	Text        string `json:"text"`
	HTML        string `json:"html"`
}

// Generate renders every entry in Entries at every width and writes the
// captures, the manifest and the index into dir, which is created if absent.
//
// The toolVersion is recorded in the manifest so a capture can be traced back to
// the program that wrote it.
//
// It returns the first error it meets, having written whatever preceded it, so
// a partial run leaves a partial directory rather than an empty one. Nothing is
// written for a widget whose render fails.
func Generate(dir, toolVersion string) (Result, error) {
	entries := Entries()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Result{}, fmt.Errorf("creating %s: %w", dir, err)
	}

	result := Result{Dir: dir}
	manifest := Manifest{Tool: "cmd/capture", Version: toolVersion, Widths: Widths()}
	index := Index{}

	for _, entry := range entries {
		record, files, total, captures, err := captureEntry(dir, entry)
		if err != nil {
			return Result{}, err
		}
		manifest.Widgets = append(manifest.Widgets, record)
		index.Widgets = append(index.Widgets, IndexEntry{
			Name:        record.Name,
			Slug:        record.Slug,
			Package:     record.Package,
			Constructor: record.Constructor,
			Summary:     record.Summary,
			Text:        record.Text,
			HTML:        record.HTML,
		})
		result.Files = append(result.Files, files...)
		result.Bytes += total
		result.Captures += captures
	}

	manifest.Count = len(manifest.Widgets)
	index.Count = len(index.Widgets)

	written, err := writeJSON(filepath.Join(dir, "manifest.json"), manifest)
	if err != nil {
		return Result{}, err
	}
	result.Files = append(result.Files, "manifest.json")
	result.Bytes += written

	written, err = writeJSON(filepath.Join(dir, "index.json"), index)
	if err != nil {
		return Result{}, err
	}
	result.Files = append(result.Files, "index.json")
	result.Bytes += written

	cssBytes := []byte(captureCSS)
	if err := writeFile(filepath.Join(dir, "capture.css"), cssBytes); err != nil {
		return Result{}, err
	}
	result.Files = append(result.Files, "capture.css")
	result.Bytes += len(cssBytes)
	_ = written

	sort.Strings(result.Files)
	return result, nil
}

// captureEntry renders one widget at every width and writes its .txt and .html.
func captureEntry(dir string, entry Entry) (ManifestWidget, []string, int, int, error) {
	if len(entry.Widths) != len(entry.Heights) {
		return ManifestWidget{}, nil, 0, 0, fmt.Errorf(
			"%s: %d widths but %d heights; the two are parallel by construction",
			entry.Name, len(entry.Widths), len(entry.Heights))
	}

	var (
		captures []capture
		files    []string
		total    int
	)
	for i, w := range entry.Widths {
		h := entry.Heights[i]
		sink, min, hasMin, err := renderOne(entry, w, h)
		if err != nil {
			return ManifestWidget{}, nil, 0, 0, err
		}
		captures = append(captures, capture{
			entry:      entry,
			width:      w,
			height:     h,
			sink:       sink,
			minSize:    min,
			hasMinSize: hasMin,
		})
	}

	slug := entry.Slug()
	textName := slug + ".txt"
	htmlName := slug + ".html"

	text := plainText(captures)
	textBytes := []byte(text)
	if err := writeFile(filepath.Join(dir, textName), textBytes); err != nil {
		return ManifestWidget{}, nil, 0, 0, err
	}

	htmlBytes := []byte(renderHTML(entry, captures))
	if err := writeFile(filepath.Join(dir, htmlName), htmlBytes); err != nil {
		return ManifestWidget{}, nil, 0, 0, err
	}

	record := ManifestWidget{
		Name:        entry.Name,
		Slug:        slug,
		Package:     entry.Package,
		Constructor: entry.Constructor,
		Summary:     entry.Summary,
		Note:        entry.Note,
		Widths:      append([]int(nil), entry.Widths...),
		Heights:     append([]int(nil), entry.Heights...),
		Text:        textName,
		TextBytes:   len(textBytes),
		HTML:        htmlName,
		HTMLBytes:   len(htmlBytes),
	}
	// MinSize is CALLED, not parsed: the thresholds behind it are private
	// constants, so reading the value is the only honest way to publish it, and
	// calling it on the demo widget is exactly the answer a reader would get
	// from the constructor they are looking at.
	if min, ok := firstMinSize(captures); ok {
		record.MinSize = &CellSize{W: min.W, H: min.H}
	}

	files = []string{textName, htmlName}
	total = len(textBytes) + len(htmlBytes)
	return record, files, total, len(captures), nil
}

// renderOne builds one entry at one size and renders it.
//
// It exists so the test can render an entry without going through Generate and
// writing a directory, and so the nil-widget and size-mismatch checks live in one
// place rather than at every call site.
func renderOne(entry Entry, w, h int) (*headless.MemorySink, buffer.Size, bool, error) {
	root := entry.Construct(buffer.Rect{X: 0, Y: 0, W: w, H: h})
	if root == nil {
		return nil, buffer.Size{}, false, fmt.Errorf("%s: Construct returned nil at %dx%d", entry.Name, w, h)
	}
	sink, err := widgettest.Capture(w, h, frames, root)
	if err != nil {
		return nil, buffer.Size{}, false, fmt.Errorf("%s at %dx%d: %w", entry.Name, w, h, err)
	}
	min, hasMin := minSizeOf(root)
	return sink, min, hasMin, nil
}

// minSizeOf asks a widget for its minimum size.
//
// Every widget in this catalog has a MinSize, and the interface check here is
// belt-and-braces rather than a fallback: a widget added later without one
// should have a manifest entry with a zero minSize, not a panic.
func minSizeOf(w termmosaic.Widget) (buffer.Size, bool) {
	type sizer interface{ MinSize() buffer.Size }
	if s, ok := w.(sizer); ok {
		return s.MinSize(), true
	}
	return buffer.Size{}, false
}

// writeJSON encodes v as indented JSON with a trailing newline and writes it.
//
// The trailing newline is not cosmetic: it is what makes the file well-formed
// under the POSIX text-file definition, and a file without one shows up as a
// whole-file diff in every future change.
func writeJSON(path string, v any) (int, error) {
	buf, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("encoding %s: %w", path, err)
	}
	buf = append(buf, '\n')
	if err := writeFile(path, buf); err != nil {
		return 0, err
	}
	return len(buf), nil
}

// writeFile writes b to path, creating parent directories as needed.
func writeFile(path string, b []byte) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// plainText returns the exact cell-grid text for an entry.
//
// The widest capture is used, and it is the widest because it shows the most: a
// plain-text capture is the fallback for a reader whose font misaligns Braille,
// and a fallback that has already been narrowed is a worse fallback. Trailing
// whitespace is trimmed per line by MemorySink.String, exactly as the golden
// files trim it, so this output is comparable with a test's expected value.
func plainText(captures []capture) string {
	if len(captures) == 0 {
		return ""
	}
	widest := captures[0]
	for _, c := range captures[1:] {
		if c.width > widest.width {
			widest = c
		}
	}
	var b bytes.Buffer
	b.WriteString(plainHeader(widest.entry, widest.width, widest.height))
	b.WriteString(widest.sink.String())
	b.WriteByte('\n')
	return b.String()
}

// plainHeader is the comment block a .txt capture opens with.
//
// It says what produced the file and at what size, because a bare grid of
// characters in a repository is unidentifiable six months later. It is a comment
// in the sense the format already implies: the first line of every entry names
// the widget, so the file stays greppable by widget name.
func plainHeader(e Entry, w, h int) string {
	return fmt.Sprintf("# %s — %s\n# package: %s\n# constructor: %s\n# %d columns x %d rows, rendered through widgettest.Capture\n\n",
		e.Name, e.Summary, e.Package, e.Constructor, w, h)
}
