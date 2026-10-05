//go:build windows

package term

import (
	"errors"
	"os"
	"testing"

	"github.com/serkanalgur/termmosaic"
)

// The Windows backend is a deliberate loud-error stub (ADR 0001), not a partial
// implementation. That makes its correct behaviour a *documented set of exact
// error identities*, so these tests assert identities rather than messages: if
// the stub ever starts returning a wrapped or different error, ErrWindowsStub
// stops matching and callers lose the only programmatic handle they have.
//
// NOTE: these tests have never been executed. CI cross-*builds* Windows but does
// not run the suite there, so until a Windows runner executes them they are
// verified only by `GOOS=windows go vet ./...` type-checking.

// stubFiles returns a readable and a writable *os.File that are not a console,
// so the tests exercise argument handling rather than anything Windows-specific.
// os.DevNull is "NUL" on Windows and opens fine for both directions.
func stubFiles(t *testing.T) (in, out *os.File) {
	t.Helper()
	in, err := os.OpenFile(os.DevNull, os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("open devnull for reading: %v", err)
	}
	out, err = os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		in.Close()
		t.Fatalf("open devnull for writing: %v", err)
	}
	t.Cleanup(func() { in.Close(); out.Close() })
	return in, out
}

func newStubTerminal(t *testing.T) *WindowsTerminal {
	t.Helper()
	in, out := stubFiles(t)
	term, err := Open(in, out, env(map[string]string{"TERM": "xterm-256color"}))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { term.Close() })
	return term
}

// TestOpenRejectsNilFiles mirrors the nil validation in Open. It is duplicated
// here because term_test.go's version only asserts that *some* error comes back
// for (nil, nil); this pins each rejection individually, since the two
// arguments are distinct mistakes and the Windows stub validates them on purpose.
func TestWindowsOpenRejectsEachNilFile(t *testing.T) {
	in, out := stubFiles(t)

	cases := []struct {
		name string
		in   *os.File
		out  *os.File
	}{
		{"nil input", nil, out},
		{"nil output", in, nil},
		{"both nil", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			term, err := Open(tc.in, tc.out, nil)
			if err == nil {
				term.Close()
				t.Fatal("Open accepted a nil file; the stub must still validate arguments")
			}
			if term != nil {
				t.Error("Open returned a terminal alongside an error; a rejected Open must return nil")
			}
		})
	}
}

// TestOpenAcceptsValidFiles is the counterpart: Open must succeed so the program
// can start and print a readable error, which is the whole point of the stub.
func TestOpenAcceptsValidFiles(t *testing.T) {
	in, out := stubFiles(t)
	term, err := Open(in, out, env(nil))
	if err != nil {
		t.Fatalf("Open with valid files: %v", err)
	}
	defer term.Close()
	if term == nil {
		t.Fatal("Open returned no terminal and no error")
	}
	if term.in != in || term.out != out {
		t.Error("Open did not retain the files it was given")
	}
}

// TestOpenDefaultsGetenv guards the nil-getenv fallback at the top of Open: a
// caller that does not care about the environment must not have to know that
// DetectCaps needs one, and must not panic.
func TestOpenDefaultsGetenv(t *testing.T) {
	in, out := stubFiles(t)
	term, err := Open(in, out, nil)
	if err != nil {
		t.Fatalf("Open with nil getenv: %v", err)
	}
	defer term.Close()
	// Whatever the ambient environment says, Capabilities must be populated
	// rather than left as a zero value with no error reported.
	_ = term.Capabilities()
}

// TestEveryConsoleOperationReportsTheStubError is the core of the stub's
// contract. Each operation is checked with errors.Is against ErrWindowsStub, so
// the sentinel stays a single comparable value for callers.
func TestEveryConsoleOperationReportsTheStubError(t *testing.T) {
	term := newStubTerminal(t)

	cases := []struct {
		name string
		call func() error
	}{
		{"EnterRawMode", term.EnterRawMode},
		{"LeaveRawMode", term.LeaveRawMode},
		{"EnterAltScreen", term.EnterAltScreen},
		{"LeaveAltScreen", term.LeaveAltScreen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if !errors.Is(err, ErrWindowsStub()) {
				t.Errorf("%s = %v, want the stub error", tc.name, err)
			}
		})
	}
}

// TestReadReportsTheStubError pins Read, which returns a count alongside the
// error and could plausibly report bytes read before failing.
func TestReadReportsTheStubError(t *testing.T) {
	term := newStubTerminal(t)
	buf := make([]byte, 16)
	n, err := term.Read(buf)
	if !errors.Is(err, ErrWindowsStub()) {
		t.Errorf("Read error = %v, want the stub error", err)
	}
	if n != 0 {
		t.Errorf("Read consumed %d bytes; an unsupported read must consume none", n)
	}
}

// TestErrWindowsStubIsStable guards the exported accessor: callers use it for
// errors.Is, so returning a fresh error each call would silently break every
// one of them.
func TestErrWindowsStubIsStable(t *testing.T) {
	if ErrWindowsStub() != ErrWindowsStub() {
		t.Error("ErrWindowsStub returned two different errors; errors.Is would never match")
	}
	if errors.Is(errors.New("something else"), ErrWindowsStub()) {
		t.Error("an unrelated error must not match the stub error")
	}
}

// TestSizeIsTheConventionalFallback records that Size reports 80x25 rather than
// failing: the stub cannot query the console, and a caller reading a size at
// startup must get a usable number rather than an error it cannot handle.
func TestSizeIsTheConventionalFallback(t *testing.T) {
	term := newStubTerminal(t)
	w, h := term.Size()
	if w != 80 || h != 25 {
		t.Errorf("Size() = %dx%d, want the 80x25 fallback", w, h)
	}
}

// TestCapabilitiesAreStillDetected checks that the one non-error path through a
// Windows terminal is populated from the environment, as Open's doc promises.
func TestCapabilitiesAreStillDetected(t *testing.T) {
	in, out := stubFiles(t)
	term, err := Open(in, out, env(map[string]string{
		"TERM":      "xterm-256color",
		"COLORTERM": "truecolor",
	}))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer term.Close()

	caps := term.Capabilities()
	if !caps.TrueColor || !caps.Color256 {
		t.Errorf("Capabilities = %+v, want the values detected from the environment", caps)
	}
	if term.Capabilities() != caps {
		t.Error("Capabilities changed between calls on an unmutated terminal")
	}
}

// TestResizeEventsIsOpenAndThenClosed pins the Close contract: the channel is
// usable before Close, and Close both closes it and is idempotent. A second
// Close returning nil would, if the closed flag were lost, panic on a double
// channel close, so this also covers that guard.
func TestResizeEventsIsOpenAndThenClosed(t *testing.T) {
	term := newStubTerminal(t)

	resizes := term.ResizeEvents()
	if resizes == nil {
		t.Fatal("ResizeEvents returned nil")
	}
	select {
	case got, ok := <-resizes:
		t.Errorf("ResizeEvents yielded %+v (ok=%v) before Close; nothing writes to it", got, ok)
	default:
	}

	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, ok := <-resizes; ok {
		t.Error("the resize channel is still open after Close")
	}

	for i := 0; i < 2; i++ {
		if err := term.Close(); err != nil {
			t.Errorf("Close call %d = %v, want nil; Close must be idempotent", i+2, err)
		}
	}
}

// TestCloseStopsConsoleOperationsFailing confirms Close does not, and must not,
// make the stub errors go away: the caller still cannot draw.
func TestCloseStopsConsoleOperationsFailing(t *testing.T) {
	term := newStubTerminal(t)
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := term.EnterAltScreen(); !errors.Is(err, ErrWindowsStub()) {
		t.Errorf("EnterAltScreen after Close = %v, want the stub error", err)
	}
}

// TestWindowsTerminalSatisfiesTheInterface restates the compile-time assertion
// in the source as a test, so that deleting the assertion (as happened with the
// stray io.Writer one) leaves this as the check that still means something.
func TestWindowsTerminalSatisfiesTheInterface(t *testing.T) {
	var iface termmosaic.Terminal = newStubTerminal(t)
	if w, h := iface.Size(); w != 80 || h != 25 {
		t.Errorf("Size through the interface = %dx%d, want 80x25", w, h)
	}
}
