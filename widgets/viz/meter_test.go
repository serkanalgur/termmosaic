package viz

import (
	"math"
	"strings"
	"testing"

	"github.com/serkanalgur/termmosaic/buffer"
)

// zonedMeter returns a bordered meter 40 cells wide with the default zones.
func zonedMeter(w, h int) *Meter {
	m := NewMeter(buffer.Rect{X: 0, Y: 0, W: w, H: h})
	m.Block().SetBorder(buffer.BorderPlain)
	return m
}

func TestMeterDrawsZonesNameAndThreshold(t *testing.T) {
	m := zonedMeter(40, 3)
	m.Set(95)
	got := rows(t, 40, 3, m)
	// The border row is Block's, and widgets/block tests it; what matters here
	// is that the frame is where it belongs.
	wantFramed(t, got, 0)
	// The zone NAME is the point of a meter: the sentence, not the shade.
	if !strings.Contains(got[1], "critical") {
		t.Errorf("the active zone's name is not on screen: %q", got[1])
	}
	if !strings.Contains(got[1], "95") {
		t.Errorf("the reading is not on screen: %q", got[1])
	}
	// The threshold marker and the zone boundaries are glyphs, so both are visible
	// with no colour at all.
	if !strings.ContainsRune(got[1], markerRune) {
		t.Errorf("no threshold marker in %q", got[1])
	}
	if countRune(got[1], zoneEdgeRune) < 1 {
		t.Errorf("no zone boundary in %q", got[1])
	}
}

func TestMeterZoneNameChangesWithTheValue(t *testing.T) {
	// The three readings are in three different bands, so the printed name must
	// change. Comparing two values that would render identically proves nothing, so
	// each pair is asserted to differ.
	m := zonedMeter(40, 3)
	names := map[float64]string{}
	for _, v := range []float64{10, 75, 95} {
		m.Set(v)
		got := rows(t, 40, 3, m)[1]
		switch {
		case strings.Contains(got, "critical"):
			names[v] = "critical"
		case strings.Contains(got, "warn"):
			names[v] = "warn"
		default:
			names[v] = "ok"
		}
	}
	if names[10] != "ok" || names[75] != "warn" || names[95] != "critical" {
		t.Errorf("zone names are wrong: %v", names)
	}
	m.Set(10)
	okRow := rows(t, 40, 3, m)[1]
	m.Set(95)
	critRow := rows(t, 40, 3, m)[1]
	assertDifferent(t, "a reading in the ok band against one in the critical band", okRow, critRow)
}

func TestMeterFillTracksTheValueWithinItsZone(t *testing.T) {
	m := zonedMeter(40, 3)
	m.Set(60)
	low := rows(t, 40, 3, m)[1]
	m.Set(68)
	high := rows(t, 40, 3, m)[1]
	assertDifferent(t, "60% against 68% of the same meter", low, high)
	// Both are in the ok band, so the difference is the fill and not the name.
	if !strings.Contains(low, "ok") || !strings.Contains(high, "ok") {
		t.Errorf("both readings should be in the ok band: %q / %q", low, high)
	}
}

func TestMeterValueBoundariesClampRatherThanPanic(t *testing.T) {
	m := zonedMeter(30, 3)
	cases := []struct {
		name string
		set  float64
		want float64
	}{
		{"zero", 0, 0},
		{"at the top", 100, 100},
		{"negative", -10, 0},
		{"over the top", 250, 100},
		{"NaN", math.NaN(), 0},
		{"infinity", math.Inf(1), 100},
	}
	for _, c := range cases {
		m.Set(c.set)
		if m.Value != c.want {
			t.Errorf("%s: Set(%v) left Value at %v, want %v", c.name, c.set, m.Value, c.want)
		}
		m.Draw(cellBuf(30, 3))
	}
	// A degenerate scale is a meter that reads zero rather than dividing by zero.
	m.ScaleMax = 0
	m.ScaleMin = 0
	m.Set(5)
	m.Draw(cellBuf(30, 3))
	// A meter with no zones at all must still draw its track.
	bare := NewMeter(buffer.Rect{X: 0, Y: 0, W: 20, H: 1})
	bare.SetZones(nil)
	bare.Set(5)
	bare.Draw(cellBuf(20, 1))
}

func TestMeterActiveZoneClampsRatherThanFailing(t *testing.T) {
	m := zonedMeter(30, 3)
	if i, ok := m.ActiveZone(500); !ok || i != 2 {
		t.Errorf("a reading above the top zone reports zone %d/%v, want the last zone", i, ok)
	}
	if i, ok := m.ActiveZone(-500); !ok || i != 0 {
		t.Errorf("a reading below the bottom zone reports zone %d/%v, want the first", i, ok)
	}
}

func TestMeterDegenerateSizesDoNotPanic(t *testing.T) {
	for _, size := range []buffer.Size{{W: 0, H: 0}, {W: 1, H: 1}, {W: 2, H: 1}, {W: 5, H: 2}, {W: 0, H: 4}, {W: 12, H: 0}} {
		m := NewMeter(buffer.Rect{X: 0, Y: 0, W: size.W, H: size.H})
		m.Block().SetBorder(buffer.BorderPlain)
		m.Set(50)
		m.Draw(cellBuf(size.W, size.H))
		m.Set(math.NaN())
		m.Draw(cellBuf(size.W, size.H))
	}
}

func TestMeterGrowsAndShrinksWithoutStaleCells(t *testing.T) {
	m := zonedMeter(40, 3)
	m.Set(95)
	wide := rows(t, 40, 3, m)
	m.SetBounds(buffer.Rect{X: 0, Y: 0, W: 16, H: 3})
	narrow := rows(t, 16, 3, m)
	if len([]rune(narrow[1])) > 16 {
		t.Errorf("row 1 is wider than the screen after shrinking: %q", narrow[1])
	}
	// The narrow meter has no room for the name, the bar and the number, so the
	// budget dropped something — and what is left must not be the wide layout's
	// leftovers.
	assertDifferent(t, "the wide and narrow meters", wide[1], narrow[1])
	m.SetBounds(buffer.Rect{X: 0, Y: 0, W: 40, H: 3})
	again := rows(t, 40, 3, m)
	if again[1] != wide[1] {
		t.Errorf("growing back did not restore the layout:\n got %q\nwant %q", again[1], wide[1])
	}
}

func TestMeterDrawIsAllocationFree(t *testing.T) {
	m := zonedMeter(40, 3)
	m.Set(75)
	buf := cellBuf(40, 3)
	drawAll(m, buf, 3)
	if got := testing.AllocsPerRun(200, func() { m.Draw(buf) }); got != 0 {
		t.Errorf("Draw allocated %v times per frame; the frame path must be free", got)
	}
	// And with the zones replaced, since that path measures and names.
	m.SetZones([]Zone{
		{Name: "a", From: 0, To: 33},
		{Name: "b", From: 33, To: 66},
		{Name: "c", From: 66},
	})
	m.Set(40)
	drawAll(m, buf, 3)
	if got := testing.AllocsPerRun(200, func() { m.Draw(buf) }); got != 0 {
		t.Errorf("Draw with custom zones allocated %v times per frame", got)
	}
}

func TestMeterMinSizeIncludesChrome(t *testing.T) {
	m := NewMeter(buffer.Rect{W: 1, H: 1})
	m.Block().SetBorder(buffer.BorderPlain)
	got := m.MinSize()
	if got.W != minMeterW+2 || got.H != minMeterH+2 {
		t.Errorf("MinSize = %+v, want {%d,%d}", got, minMeterW+2, minMeterH+2)
	}
}

func TestMeterHandlesNothing(t *testing.T) {
	m := zonedMeter(30, 3)
	if m.Handle(termmosaicEvent()) {
		t.Error("a meter consumed a zero event")
	}
}
