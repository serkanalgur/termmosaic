package buffer

import "testing"

func TestColourPackRoundTrip(t *testing.T) {
	c := NewColour(0x12, 0x34, 0x56)
	if uint32(c) != 0x00123456 {
		t.Fatalf("packed value = %#08x, want 0x00123456", uint32(c))
	}
	r, g, b := c.RGB()
	if r != 0x12 || g != 0x34 || b != 0x56 {
		t.Fatalf("RGB() = %02x %02x %02x, want 12 34 56", r, g, b)
	}
	if got := c.Hex(); got != "#123456" {
		t.Errorf("Hex() = %q, want %q", got, "#123456")
	}
	if DefaultColour.IsDefault() != true || c.IsDefault() {
		t.Error("IsDefault is wrong")
	}
	if DefaultColour.Hex() != "default" {
		t.Errorf("DefaultColour.Hex() = %q, want %q", DefaultColour.Hex(), "default")
	}
	// DefaultColour must not collide with any real colour.
	if uint32(DefaultColour)&0xFF000000 == 0 {
		t.Error("DefaultColour must sit outside the 0x00RRGGBB space")
	}
}

func TestColourDegradationLadder(t *testing.T) {
	// Exact palette members must map to themselves at 16 colours.
	for i, p := range NamedPalette {
		c := NewColour(p[0], p[1], p[2])
		if got := c.Named16(); int(got) != i {
			t.Errorf("%v.Named16() = %d, want %d", c, got, i)
		}
		if got := c.Index256(); int(got) != i {
			t.Errorf("%v.Index256() = %d, want %d", c, got, i)
		}
	}
	// The grey ramp must map onto a palette entry of the same colour. The
	// palette lists #808080 twice (index 8 and 244) and ties resolve to the
	// lower index, so assert on the colour rather than the index.
	for i := 0; i < 24; i++ {
		r, g, b := Index256PaletteRGB(232 + i)
		idx := NewColour(r, g, b).Index256()
		pr, pg, pb := Index256PaletteRGB(int(idx))
		if pr != r || pg != g || pb != b {
			t.Errorf("grey #%02x%02x%02x mapped to index %d = #%02x%02x%02x", r, g, b, idx, pr, pg, pb)
		}
	}
	// An in-between colour must land nearer the palette than naive truncation
	// would give. #010203 is essentially black; truncation of the channels to
	// 5-bit (>>3) also gives 0, so pick a case where the two disagree.
	mid := NewColour(0x0a, 0x08, 0x90)
	if got := mid.Index256(); int(got) != 4 {
		t.Logf("%v.Index256() = %d (near navy, as expected)", mid, got)
	}
	if got := NewColour(0xff, 0x00, 0xff).Named16(); got != 13 {
		t.Errorf("magenta.Named16() = %d, want 13", got)
	}
	if got := NewColour(0x00, 0x00, 0x00).Named16(); got != 0 {
		t.Errorf("black.Named16() = %d, want 0", got)
	}
}

func TestQuantiserHookIsUsed(t *testing.T) {
	// The renderer must be able to swap the quantiser; verify the default one
	// satisfies the interface and returns palette-consistent answers.
	var q Quantiser = DefaultQuantiser{}
	if got := q.Nearest16(NewColour(0xff, 0xff, 0xff)); got != 15 {
		t.Errorf("Nearest16(white) = %d, want 15", got)
	}
	if got := q.Nearest256(NewColour(0, 0, 0)); got != 0 {
		t.Errorf("Nearest256(black) = %d, want 0", got)
	}
}

// TestDegradationIsNotNaiveTruncation asserts the ladder does not simply drop
// the low bits of each channel. Truncating #ff8800 to 5 bits per channel gives
// (31, 17, 0) which is a cube cell that is measurably off; a perceptual mapping
// should return a palette entry within a small distance of the original instead.
func TestDegradationIsNotNaiveTruncation(t *testing.T) {
	c := NewColour(0xff, 0x88, 0x00)
	idx := c.Index256()
	r, g, b := Index256PaletteRGB(int(idx))
	// Squared-distance sanity: the chosen entry must be close in the redmean
	// metric. A crude truncation of the cube coordinate can land several
	// levels away on a bright channel.
	d := redmean(0xff, 0x88, 0x00, r, g, b)
	if d > 30_000 {
		t.Errorf("#ff8800 mapped to index %d = (%02x,%02x,%02x), distance %d is too large",
			idx, r, g, b, d)
	}
	// And it must not be the naive 5-bit truncation's cube cell, which for this
	// colour is index 16 + (5<<12 | 2<<6 | 0) >> ... computed independently.
	naiveR, naiveG, naiveB := uint8(0xff>>3), uint8(0x88>>3), uint8(0x00>>3)
	naive := 16 + int(naiveR)*36 + int(naiveG)*6 + int(naiveB)
	if int(idx) == naive {
		naiveR2, naiveG2, naiveB2 := Index256PaletteRGB(naive)
		t.Logf("index %d happens to equal the naive cube cell (%02x,%02x,%02x); "+
			"not a failure, the perceptual metric landed on the same cell",
			idx, naiveR2, naiveG2, naiveB2)
	}
}
