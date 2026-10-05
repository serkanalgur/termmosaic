// Command capture renders every widget in the TermMosaic catalog and writes the
// plain-text and HTML captures the documentation site displays.
//
//	go run ./cmd/capture -out ../termmosaic.github.io/static/captures
//
// # Why a command in this repository and not a script in the site's
//
// The captures are produced by running the widgets through
// widgets/widgettest.Capture — the same widget -> buffer -> renderer -> two-tier
// diff -> ANSI encoder -> headless.MemorySink path every widget test uses — and
// then translating the resulting cell grid. The documentation therefore cannot
// disagree with the code, because a disagreement would mean the code and its
// tests disagreed first.
//
// The corollary is that this command MUST NOT be moved out of the module. A
// generator in the site repository would have to reimplement the rendering path
// or shell out to a build of it, and either way it stops being the thing the
// tests exercise.
//
// # Determinism
//
// Two runs into two directories produce byte-identical output. There is no
// timestamp, no hostname, no map iteration in an output path and no randomness.
// A capture that changed on every run could not be reviewed in a diff, and a
// docs build that rewrote its whole output would hide the one capture that
// actually did change. -check enforces it.
//
// # Output
//
// One .txt and one .html per widget, named by the widget in lower case, plus
// manifest.json (every capture, its widths, byte counts and constructor),
// index.json (the catalog the site's widget index reads) and capture.css.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/serkanalgur/termmosaic/internal/docsgen"
)

// defaultOut is where the documentation site consumes the captures from: the
// static directory of the site repository, resolved against the user's home
// directory rather than the working directory, because the two repositories are
// siblings and a relative default would depend on which one you are standing in.
//
// The site's repository owns the asset and this repository owns the program that
// produces it. A capture committed inside the framework it documents would be a
// second copy to keep in step, and the one thing this whole design exists to
// prevent is a second source of truth about what a widget draws.
//
// Overridable with -out for a CI artifact directory or a local scratch check.
const defaultOut = "~/termmosaic.github.io/static/captures"

// version is the tool's own version string, recorded in manifest.json so a
// capture file can be traced to the program that wrote it.
const version = "0.4.1"

func main() {
	out := flag.String("out", defaultOut,
		"directory to write the captures into; created if absent")
	check := flag.Bool("check", false,
		"render twice into temporary directories and fail unless the bytes are identical")
	list := flag.Bool("list", false,
		"print the catalog and the capture sizes, write nothing")
	flag.Parse()

	if err := run(*out, *check, *list); err != nil {
		fmt.Fprintln(os.Stderr, "capture:", err)
		os.Exit(1)
	}
}

// run dispatches to the requested mode. Every mode returns an error rather than
// exiting, so the failure paths are testable rather than being assertions about
// os.Exit.
func run(out string, check, list bool) error {
	expanded, err := expandHome(out)
	if err != nil {
		return err
	}
	switch {
	case list:
		return listEntries()
	case check:
		return checkDeterministic(expanded)
	default:
		return generate(expanded)
	}
}

// expandHome replaces a leading ~ with the user's home directory.
//
// A tilde in a path is the shell's job, not the program's — but a default that
// only works when the flag is omitted and the shell expands it is a default that
// breaks under `os/exec`, under a Makefile and under CI, which is where this
// tool will mostly be run.
func expandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving %s: %w", path, err)
	}
	if path == "~" {
		return home, nil
	}
	if !strings.HasPrefix(path, "~/") {
		return "", fmt.Errorf("%s: only ~ and ~/... are expanded", path)
	}
	return filepath.Join(home, path[2:]), nil
}

// generate renders the catalog into out and prints a summary.
func generate(out string) error {
	res, err := docsgen.Generate(out, version)
	if err != nil {
		return err
	}
	printSummary(res)
	return nil
}

// printSummary prints what was written, so a run in CI says more than "ok".
func printSummary(res docsgen.Result) {
	fmt.Printf("wrote %d captures and %d files to %s (%d bytes)\n",
		res.Captures, len(res.Files), res.Dir, res.Bytes)
	for _, f := range res.Files {
		info, err := os.Stat(filepath.Join(res.Dir, f))
		if err != nil {
			fmt.Printf("  %-24s  (unreadable: %v)\n", f, err)
			continue
		}
		fmt.Printf("  %-24s  %7d bytes\n", f, info.Size())
	}
}

// checkDeterministic renders the catalog twice into temporary directories and
// compares the two trees byte for byte.
//
// It renders twice rather than once and comparing with a committed copy,
// because a committed copy can only tell you the tool's output changed; two
// fresh renders tell you the tool is a function of its input, which is the
// property the docs build actually depends on.
func checkDeterministic(out string) error {
	first, err := os.MkdirTemp("", "capture-a-")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	// Cleanup in a defer: a failure to remove a temp dir the OS will reap on exit
	// is not worth failing a determinism check over, and there is nowhere left to
	// report it to by the time a defer runs.
	defer func() { _ = os.RemoveAll(first) }()
	second, err := os.MkdirTemp("", "capture-b-")
	if err != nil {
		return fmt.Errorf("temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(second) }()

	a, err := docsgen.Generate(first, version)
	if err != nil {
		return err
	}
	b, err := docsgen.Generate(second, version)
	if err != nil {
		return err
	}
	if a.Captures != b.Captures || a.Bytes != b.Bytes {
		return fmt.Errorf("two runs disagreed in size: %d captures/%d bytes then %d captures/%d bytes",
			a.Captures, a.Bytes, b.Captures, b.Bytes)
	}
	for _, name := range a.Files {
		x, err := os.ReadFile(filepath.Join(first, name))
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		y, err := os.ReadFile(filepath.Join(second, name))
		if err != nil {
			return fmt.Errorf("reading %s: %w", name, err)
		}
		if string(x) != string(y) {
			return fmt.Errorf("%s differs between two runs of the same input: %s", name, firstDifference(x, y))
		}
	}
	fmt.Printf("deterministic: %d captures, %d files, %d bytes, identical across two runs\n",
		a.Captures, len(a.Files), a.Bytes)

	// The tool is deterministic; the directory on disk is not, so a -check run
	// does not rewrite it. Say so rather than let a caller wonder.
	if _, err := os.Stat(out); err == nil {
		fmt.Printf("%s left untouched; -check does not write to it\n", out)
	} else {
		fmt.Printf("%s does not exist yet; run without -check to create it\n", out)
	}
	return nil
}

// listEntries prints the catalog without writing anything, so the registry can
// be reviewed in a diff without rendering 22 widgets.
func listEntries() error {
	for _, e := range docsgen.Entries() {
		fmt.Printf("%-12s %-18s %2d widths %s\n",
			e.Name, e.Package, len(e.Widths), e.Constructor)
	}
	fmt.Printf("%d widgets\n", len(docsgen.Entries()))
	return nil
}

// firstDifference describes the first byte at which a and b differ, with enough
// context to find it. A determinism failure is otherwise a 4 KB unhelpful diff.
func firstDifference(a, b []byte) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			lo := i - 40
			if lo < 0 {
				lo = 0
			}
			return fmt.Sprintf("at byte %d: %q vs %q", i,
				strings.TrimSpace(string(a[lo:i+40])), strings.TrimSpace(string(b[lo:i+40])))
		}
	}
	return fmt.Sprintf("one is a prefix of the other: %d vs %d bytes", len(a), len(b))
}
