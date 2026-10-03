package term

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic"
	"github.com/serkanalgur/termmosaic/buffer"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetectCapsTrueColor(t *testing.T) {
	caps := DetectCaps(env(map[string]string{
		"TERM":      "xterm-256color",
		"COLORTERM": "truecolor",
		"LANG":      "en_US.UTF-8",
	}))
	if !caps.TrueColor || !caps.Color256 {
		t.Errorf("COLORTERM=truecolor must select truecolor, got %+v", caps)
	}
	if !caps.Unicode {
		t.Errorf("a UTF-8 locale must enable Unicode, got %+v", caps)
	}
	if got := caps.ColourDepth(); got != buffer.DepthTrueColor {
		t.Errorf("ColourDepth = %v, want truecolor", got)
	}
}

func TestDetectCapsLadder(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want buffer.ColourDepth
	}{
		{"colorterm 24bit", map[string]string{"COLORTERM": "24bit"}, buffer.DepthTrueColor},
		{"term 256color", map[string]string{"TERM": "xterm-256color"}, buffer.Depth256},
		{"term truecolor", map[string]string{"TERM": "screen.truecolor"}, buffer.DepthTrueColor},
		{"term direct", map[string]string{"TERM": "xterm-direct"}, buffer.DepthTrueColor},
		{"plain xterm", map[string]string{"TERM": "xterm"}, buffer.Depth16},
		{"dumb", map[string]string{"TERM": "dumb"}, buffer.Depth16},
		{"empty", map[string]string{}, buffer.Depth16},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DetectCaps(env(tc.env)).ColourDepth(); got != tc.want {
				t.Errorf("ColourDepth = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDetectCapsUnicodeLocale(t *testing.T) {
	for _, lang := range []string{"en_US.UTF-8", "en_US.utf8", "C.UTF-8"} {
		if !DetectCaps(env(map[string]string{"LANG": lang})).Unicode {
			t.Errorf("LANG=%s should enable Unicode", lang)
		}
	}
	for _, lang := range []string{"en_US.ISO-8859-1", "C", ""} {
		if DetectCaps(env(map[string]string{"LANG": lang})).Unicode {
			t.Errorf("LANG=%q should not enable Unicode", lang)
		}
	}
	// LC_ALL takes precedence over LANG.
	if DetectCaps(env(map[string]string{"LANG": "C", "LC_ALL": "en_US.UTF-8"})).Unicode != true {
		t.Error("LC_ALL should take precedence over LANG")
	}
	// LC_CTYPE is consulted when LC_ALL is absent.
	if DetectCaps(env(map[string]string{"LANG": "C", "LC_CTYPE": "en_US.UTF-8"})).Unicode != true {
		t.Error("LC_CTYPE should be consulted when LC_ALL is absent")
	}
}

func TestDetectCapsKittyIsNotAssumed(t *testing.T) {
	// Guessing wrong about the kitty protocol means a terminal that does not
	// implement it receives garbage, so it must not be enabled by default.
	if DetectCaps(env(map[string]string{"TERM": "xterm-256color"})).KittyKeyboard {
		t.Error("kitty keyboard must not be assumed")
	}
	if !DetectCaps(env(map[string]string{"TERM": "xterm-kitty"})).KittyKeyboard {
		t.Error("a kitty TERM should enable the kitty keyboard protocol")
	}
}

func TestDetectCapsOptimisticFeatures(t *testing.T) {
	caps := DetectCaps(env(nil))
	if !caps.Mouse || !caps.BracketedPaste {
		t.Errorf("mouse and bracketed paste should default on: %+v", caps)
	}
}

func TestDetectCapsIgnoresNoColor(t *testing.T) {
	// NO_COLOR is a user preference, not a device capability; the renderer's
	// NoColorFromEnv reads it. DetectCaps must not silently swallow it, or a
	// caller inspecting Caps cannot tell why colour is off.
	caps := DetectCaps(env(map[string]string{"NO_COLOR": "1", "COLORTERM": "truecolor"}))
	if !caps.TrueColor {
		t.Error("NO_COLOR must not change the detected capability")
	}
}

func TestFileSinkWritesVerbatim(t *testing.T) {
	var buf bytes.Buffer
	s := NewSink(&buf)
	in := []byte("\x1b[1;1Hhi")
	n, err := s.Write(in)
	if err != nil || n != len(in) {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if err := s.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if buf.String() != string(in) {
		t.Errorf("sink wrote %q, want %q", buf.String(), in)
	}
}

func TestSinkImplementsTheInterface(t *testing.T) {
	var _ termmosaic.Sink = NewSink(io.Discard)
}

func TestOpenRejectsNilFiles(t *testing.T) {
	if _, err := Open(nil, nil, nil); err == nil {
		t.Error("Open must reject a nil input file")
	}
}

func BenchmarkSinkWrite(b *testing.B) {
	s := NewSink(io.Discard)
	frame := []byte(strings.Repeat("\x1b[38;2;1;2;3m#", 512))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := s.Write(frame); err != nil {
			b.Fatal(err)
		}
	}
}
