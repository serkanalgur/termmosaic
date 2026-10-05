package keymap_test

import (
	"os"
	"strings"
	"testing"
)

// TestHandleDocumentsPrecedence checks the one edit ADR 0009 makes to
// widget.go, by reading the file.
//
// It is in this file rather than mixed_test.go because the mixed-mechanism
// observation is about behaviour and this is about a comment, and a behavioural
// test that also greps a source file fails for two unrelated reasons at once.
//
// The comment is the ONLY concession the ADR makes. It is not enforced by any
// type — a keymap that shadows a widget's case is silent, as
// TestTheShadowedKeyIsSilent demonstrates — so the sentence telling a widget
// author that it happens is the entire mitigation, and a sentence that gets
// edited away during a refactor leaves the sharp edge undocumented.
//
// The three phrases are checked separately because each is load-bearing and
// each could be lost to a well-meaning rewrite:
//
//   - "declined": the events arrive only after the keymap has passed on them,
//     which is the direction of the precedence.
//   - "shadows": the consequence for the widget's own switch, named as an effect
//     rather than as a rule the reader has to infer.
//   - "the fallback" together with "not the": what a widget author should
//     therefore build, which is the actionable half. The phrase is matched in
//     two pieces because gofmt rewraps a doc comment to the line width, so the
//     exact string a human typed is not stable and a test that asserts it is
//     asserting the wrapping rather than the sentence.
func TestHandleDocumentsPrecedence(t *testing.T) {
	src, err := os.ReadFile("../widget.go")
	if err != nil {
		t.Fatalf("reading ../widget.go: %v", err)
	}
	text := string(src)

	// Isolate the Handle doc comment, which is the only place this text may
	// live: a copy of the paragraph elsewhere would rot, and a paragraph in the
	// package comment would not be where a widget author looks.
	i := strings.Index(text, "// Handle offers the event")
	if i < 0 {
		t.Fatal("widget.go has no Handle doc comment; the precedence paragraph is missing entirely")
	}
	rest := text[i:]
	end := strings.Index(rest, "\n\tHandle(")
	if end < 0 {
		t.Fatal("could not find the end of the Handle doc comment")
	}
	doc := rest[:end]
	// gofmt rewraps doc comments, so the sentences are matched on the words that
	// carry the meaning rather than on the line breaks a human produced. The
	// comment markers come out first: leaving them in would insert a "//" into
	// the middle of every wrapped sentence and no phrase would ever match.
	var words []string
	for _, line := range strings.Split(doc, "\n") {
		words = append(words, strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "//"))...)
	}
	flat := strings.Join(words, " ")

	for _, phrase := range []string{"declined", "shadows", "the fallback, not the primary path"} {
		if !strings.Contains(flat, phrase) {
			t.Errorf("Widget.Handle's doc comment does not contain %q.\nGot:\n%s", phrase, doc)
		}
	}
	if !strings.Contains(doc, "ADR 0009") {
		t.Errorf("Widget.Handle's doc comment does not cite ADR 0009; the precedence rule has to be traceable to the decision that made it.\nGot:\n%s", doc)
	}

	// The method set itself is unchanged. This is the guard on the ADR's
	// "no method added, none changed, none deprecated", and it is worth having
	// as a test because a fifth method would compile everywhere and break
	// nothing until 24 widgets failed to satisfy the interface.
	if !strings.Contains(text, "Handle(Event) bool") {
		t.Error("Widget.Handle's signature changed; ADR 0009 §2.3 freezes it and the whole catalog depends on it")
	}
	for _, method := range []string{"Bounds() Rect", "Draw(buf *buffer.Buffer)", "Invalidate()"} {
		if !strings.Contains(text, method) {
			t.Errorf("Widget no longer declares %s; ADR 0009 §2.3 changes no method of the interface", method)
		}
	}
}
