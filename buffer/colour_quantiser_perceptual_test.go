package buffer

// Perceptual audit of the colour degradation ladder.
//
// docs/STATUS.md carries "Color model and degradation ladder" as PROPOSED with
// the note that the redmean mapping "is not yet validated". This file is the
// validation: it measures the DefaultQuantiser against a perceptually uniform
// metric, separates the error the palette forces from the error the quantiser
// adds, and pins the result with regression thresholds so the decision cannot
// silently rot. If a future change to redmean, the palettes, or a replacement
// Quantiser makes the mapping worse, these tests fail.
//
// Metric: CIEDE2000 (delta E 2000) on sRGB->CIE Lab (D65), implemented below
// and pinned against Sharma, Wu & Dalal's 34 reference pairs. CIEDE2000 is the
// current CIE standard for colour difference: it weights lightness, chroma and
// hue by what the eye actually resolves, which plain Lab Euclidean distance
// (CIE76) does not — CIE76 overstates differences among dark blues and near
// neutrals, exactly the region a terminal palette is densest in. Measuring in
// RGB, as redmean itself operates, would beg the question: the approximation
// would be graded by the same geometry it was built from.
//
// Sampling of the truecolor cube: the full 16,777,216-colour cube cannot be
// scanned against a 256-entry CIEDE2000 oracle inside a test budget (16.7M x
// 256 x ~190 ns = ~13 minutes), so the sweep is systematic: every channel
// takes the values 0, 5, 10, ..., 255 (a step-5 lattice, 52^3 = 140,608
// colours, 0.84% of the cube, uniform in each channel). Two exactness passes
// sit on top of it: every one of the 256 palette members is enumerated (a
// palette member has zero gamut error by definition, so its error is 100%
// selection error), and the 20 worst lattice points per rung are re-scanned in
// their full +-4 neighbourhood (20 x 729 colours) so the reported max is not
// an artefact of lattice alignment. The theme test enumerates its colours
// exactly.
//
// Error decomposition, which is the whole analysis:
//
//	total(c)     = dE00(c, palette[quantiser(c)])   what the user sees
//	gamut(c)     = min over palette of dE00(c, p)    unavoidable: no exact match exists
//	selection(c) = total(c) - gamut(c)               >= 0; the quantiser's own fault
//
// A colour outside the 256 cube has gamut error no quantiser can remove; only
// selection error is a defect. Collapsing the two would either overstate the
// ladder (blaming redmean for the palette's coarseness) or understate it
// (letting a bad pick hide behind an unavoidable one). At the 256 rung the
// palette is all 256 xterm entries, which includes the 16 named colours, so
// "quantised to a named ANSI colour instead of a cube entry" is scored as a
// legitimate pick; at the 16 rung only the 16 named colours exist.
//
// Percentiles use nearest-rank on the sorted sample: p_q = sorted[int(q*(n-1))].
//
// A finding this audit pins directly: the distance function in colour.go is
// NAMED redmean but is not redmean in effect. Its rmean weighting divides by
// 256 with integer arithmetic, and rmean <= 255 for every pair of uint8
// channels, so both weight terms are identically 2 and the formula collapses
// to fixed-weight 2*dr^2 + 4*dg^2 + 2*db^2 in gamma RGB. The hue errors
// measured below (brick red -> olive, coral -> dark olive, dark green ->
// grey) are exactly what that fixed weighting predicts. See
// TestQuantiserEffectiveDistanceIsPinned, which fails if anyone "fixes" the
// division — because that fix changes every 256/16-colour byte the renderer
// emits and must be a deliberate, re-audited decision, not a drive-by.

import (
	"fmt"
	"math"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Thresholds pinned by the measurements in the audit. Read this block as
// "what the numbers were when the decision was made", not as "what is good":
// several bounds below knowingly sit above a measured defect, because a
// regression test pins behaviour and the judgement lives in the report, not
// in a round number. The defects each bound is honestly carrying:
//
//   - themeMaxSelection256 = 3.0 pins search.accent (#7ac2e8, the search
//     example's stAccent), where the quantiser picks #87afd7 over the
//     dE00-nearest #87d7ff at selection error 2.600 — just above the ~2.3 JND.
//     The two candidates differ from each other by dE00 11.8, so this is a
//     visible dulling of the accent, though still "a light blue", not a hue
//     flip.
//   - themeMaxSelection16 = 12.0 pins markets.down (#d86a62), where the
//     quantiser picks grey #808080 over red #ff0000 at selection error
//     10.240 — a semantic collapse (the "down" colour loses its redness and
//     collides with markets.flat, which also renders #808080 at this rung).
//     Also pins markets.up (#3cc88c -> cyan instead of bright green,
//     selection 7.151). This bound is NOT a clean bill of health for the 16
//     rung.
//   - perceptualMaxSelection256 = 24.0 and perceptualMeanSelection256 = 1.5
//     pin the full-cube sweep: refined max selection error 21.201 (worst
//     case #747334 -> #875f5f where #808000 is dE00-optimal), mean 1.323,
//     p95 7.121, p99 12.130, with 19.35% of the lattice above the JND. The
//     worst cases are plausible UI colours (dark brick red #9b3228 -> olive
//     #875f00; dark green #32732d -> grey #4e4e4e), not pathological corners.
//     These bounds pin a DEFECTIVE baseline on purpose: replacing the
//     quantiser with a Lab-space one should drive them towards zero, and
//     lowering them is the point.
//   - perceptualMaxSelection16 = 40.0 and perceptualMeanSelection16 = 4.2
//     pin the 16-rung sweep: refined max 36.821 (bright coral #e6553c ->
//     dark olive #808000 where red #ff0000 is dE00-optimal at 7.4), mean
//     3.800, p95 16.512, with 37.50% above the JND and 15.98% above 10. This
//     rung is broken for plausible UI colours; the bounds exist so the
//     breakage cannot silently worsen before the replacement lands.
//   - themeMaxTotal256/16 pin total error including unavoidable gamut error.
//     The 16-rung totals are large because the 16-colour palette is coarse
//     (markets.accent #54a8f0 -> #c0c0c0 at dE00 23.6 even with an optimal
//     pick — verified: silver 23.57 beats blue #0000ff at 42.25 under
//     dE00's lightness weighting): that is palette geometry, not the
//     quantiser's doing.
//
// JND ("just noticeable difference") in CIEDE2000 is ~2.3; ~5 is clearly
// visible; ~10+ reads as a different colour. Selection error is the part of
// the mapping the quantiser controls — a perfect quantiser has selection
// error 0 everywhere.
const (
	// perceptualMaxSelection256 is the worst selection error allowed at the
	// 256 rung over the lattice sweep plus the +-4 refinement neighbourhoods.
	// Measured: 21.201. Headroom ~13%.
	perceptualMaxSelection256 = 24.0

	// perceptualMaxSelection16 is the same bound at the 16 rung.
	// Measured: 36.821. Headroom ~8.6%.
	perceptualMaxSelection16 = 40.0

	// perceptualMeanSelection256 guards against a systemic drift that a max
	// bound alone could miss (many small errors instead of one big one).
	// Measured: 1.323.
	perceptualMeanSelection256 = 1.5

	// perceptualMeanSelection16 is the 16-rung counterpart.
	// Measured: 3.800.
	perceptualMeanSelection16 = 4.2

	// themeMaxSelection256 / themeMaxSelection16 bound the selection error on
	// the colours examples/search and examples/markets actually paint. See
	// the block above: 256 carries a 2.6 defect on stAccent, 16 carries a
	// 10.2 defect on the markets "down" colour.
	themeMaxSelection256 = 3.0
	themeMaxSelection16  = 12.0

	// themeMaxTotal256 bounds what the user sees for a theme colour on a 256
	// terminal. Includes unavoidable gamut error (tinted greys neutralise
	// against the xterm grey ramp at dE00 ~7.5), so it is looser than the
	// selection bound.
	themeMaxTotal256 = 9.0

	// themeMaxTotal16 bounds what the user sees on a 16-colour terminal. The
	// 16-colour palette is genuinely coarse; this pins the observed quality
	// so it cannot silently worsen, it does not claim the rung is fine.
	themeMaxTotal16 = 30.0
)

// ---------------------------------------------------------------------------
// sRGB -> CIE Lab (D65) and CIEDE2000.
//
// The CIEDE2000 implementation follows Sharma, Wu & Dalal (2005), "The
// CIEDE2000 Color-Difference Formula: Implementation Notes, Supplementary
// Test Data, and Mathematical Observations". TestQuantiserMetricIsPinned
// checks it against their 34 reference pairs, so the measuring instrument
// itself is under regression control: if a refactor breaks the metric, the
// audit cannot quietly start reporting wrong numbers.
// ---------------------------------------------------------------------------

// lab is a CIE L*a*b* coordinate triple.
type lab struct{ l, a, b float64 }

// srgbChannelToLinear converts one sRGB channel (0-255) to linear light.
func srgbChannelToLinear(v uint8) float64 {
	c := float64(v) / 255.0
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}

// labWhiteD65 is the D65 white point in the sRGB RGB->XYZ matrix's scale.
var labWhiteD65 = [3]float64{0.95047, 1.00000, 1.08883}

// rgbToLab converts an sRGB triple to CIE Lab under D65.
func rgbToLab(r, g, b uint8) lab {
	rl, gl, bl := srgbChannelToLinear(r), srgbChannelToLinear(g), srgbChannelToLinear(b)
	x := 0.4124564*rl + 0.3575761*gl + 0.1804375*bl
	y := 0.2126729*rl + 0.7151522*gl + 0.0721750*bl
	z := 0.0193339*rl + 0.1191920*gl + 0.9503041*bl
	f := func(t float64) float64 {
		if t > 216.0/24389.0 {
			return math.Cbrt(t)
		}
		return (24389.0/27.0*t + 16.0) / 116.0
	}
	fx, fy, fz := f(x/labWhiteD65[0]), f(y/labWhiteD65[1]), f(z/labWhiteD65[2])
	return lab{116*fy - 16, 500 * (fx - fy), 200 * (fy - fz)}
}

// deltaE00 is the CIEDE2000 colour difference between two Lab colours.
func deltaE00(x, y lab) float64 {
	const deg = math.Pi / 180
	c1 := math.Hypot(x.a, x.b)
	c2 := math.Hypot(y.a, y.b)
	cbar := (c1 + c2) / 2
	g := 0.5 * (1 - math.Sqrt(math.Pow(cbar, 7)/(math.Pow(cbar, 7)+math.Pow(25.0, 7))))
	a1p := (1 + g) * x.a
	a2p := (1 + g) * y.a
	c1p := math.Hypot(a1p, x.b)
	c2p := math.Hypot(a2p, y.b)
	h1p := math.Atan2(x.b, a1p) / deg
	if h1p < 0 {
		h1p += 360
	}
	h2p := math.Atan2(y.b, a2p) / deg
	if h2p < 0 {
		h2p += 360
	}
	dLp := y.l - x.l
	dCp := c2p - c1p
	dhp := 0.0
	if c1p*c2p != 0 {
		dhp = h2p - h1p
		if dhp > 180 {
			dhp -= 360
		} else if dhp < -180 {
			dhp += 360
		}
	}
	dHp := 2 * math.Sqrt(c1p*c2p) * math.Sin(dhp*deg/2)
	lbarp := (x.l + y.l) / 2
	cbarp := (c1p + c2p) / 2
	// Sharma's reference form: when either chroma is zero the mean hue is the
	// raw sum (it is multiplied by zero everywhere it appears, so the choice
	// is immaterial to dE00 — but keeping the branch matches the reference
	// and keeps the initialisation meaningful).
	hbarp := h1p + h2p
	if c1p*c2p != 0 {
		if math.Abs(h1p-h2p) <= 180 {
			hbarp = (h1p + h2p) / 2
		} else if h1p+h2p < 360 {
			hbarp = (h1p + h2p + 360) / 2
		} else {
			hbarp = (h1p + h2p - 360) / 2
		}
	}
	t := 1 - 0.17*math.Cos((hbarp-30)*deg) + 0.24*math.Cos(2*hbarp*deg) +
		0.32*math.Cos((3*hbarp+6)*deg) - 0.20*math.Cos((4*hbarp-63)*deg)
	dtheta := 30 * math.Exp(-math.Pow((hbarp-275)/25, 2))
	rc := 2 * math.Sqrt(math.Pow(cbarp, 7)/(math.Pow(cbarp, 7)+math.Pow(25.0, 7)))
	sl := 1 + (0.015*math.Pow(lbarp-50, 2))/math.Sqrt(20+math.Pow(lbarp-50, 2))
	sc := 1 + 0.045*cbarp
	sh := 1 + 0.015*cbarp*t
	rt := -math.Sin(2*dtheta*deg) * rc
	return math.Sqrt(math.Pow(dLp/sl, 2) + math.Pow(dCp/sc, 2) + math.Pow(dHp/sh, 2) +
		rt*(dCp/sc)*(dHp/sh))
}

// sharmaPairs is Table 1 of Sharma, Wu & Dalal (2005): 34 pairs of CIELAB
// values with the expected dE00. This is the reference every conforming
// implementation is checked against.
var sharmaPairs = []struct {
	lab1, lab2 lab
	want       float64
}{
	{lab{50.0000, 2.6772, -79.7751}, lab{50.0000, 0.0000, -82.7485}, 2.0425},
	{lab{50.0000, 3.1571, -77.2803}, lab{50.0000, 0.0000, -82.7485}, 2.8615},
	{lab{50.0000, 2.8361, -74.0200}, lab{50.0000, 0.0000, -82.7485}, 3.4412},
	{lab{50.0000, -1.3802, -84.2814}, lab{50.0000, 0.0000, -82.7485}, 1.0000},
	{lab{50.0000, -1.1848, -84.8006}, lab{50.0000, 0.0000, -82.7485}, 1.0000},
	{lab{50.0000, -0.9009, -85.5211}, lab{50.0000, 0.0000, -82.7485}, 1.0000},
	{lab{50.0000, 0.0000, 0.0000}, lab{50.0000, -1.0000, 2.0000}, 2.3669},
	{lab{50.0000, -1.0000, 2.0000}, lab{50.0000, 0.0000, 0.0000}, 2.3669},
	{lab{50.0000, 2.4900, -0.0010}, lab{50.0000, -2.4900, 0.0009}, 7.1792},
	{lab{50.0000, 2.4900, -0.0010}, lab{50.0000, -2.4900, 0.0010}, 7.1792},
	{lab{50.0000, 2.4900, -0.0010}, lab{50.0000, -2.4900, 0.0011}, 7.2195},
	{lab{50.0000, 2.4900, -0.0010}, lab{50.0000, -2.4900, 0.0012}, 7.2195},
	{lab{50.0000, -0.0010, 2.4900}, lab{50.0000, 0.0009, -2.4900}, 4.8045},
	{lab{50.0000, -0.0010, 2.4900}, lab{50.0000, 0.0010, -2.4900}, 4.8045},
	{lab{50.0000, -0.0010, 2.4900}, lab{50.0000, 0.0011, -2.4900}, 4.7461},
	{lab{50.0000, -0.0010, 2.4900}, lab{50.0000, 0.0012, -2.4900}, 4.7461},
	{lab{50.0000, 2.5000, 0.0000}, lab{50.0000, 0.0000, -2.5000}, 4.3065},
	{lab{50.0000, 2.5000, 0.0000}, lab{73.0000, 25.0000, -18.0000}, 27.1492},
	{lab{50.0000, 2.5000, 0.0000}, lab{61.0000, -5.0000, 29.0000}, 22.8977},
	{lab{50.0000, 2.5000, 0.0000}, lab{56.0000, -27.0000, -3.0000}, 31.9030},
	{lab{50.0000, 2.5000, 0.0000}, lab{58.0000, 24.0000, 15.0000}, 19.4535},
	{lab{50.0000, 2.5000, 0.0000}, lab{50.0000, 3.1736, 0.5854}, 1.0000},
	{lab{50.0000, 2.5000, 0.0000}, lab{50.0000, 3.2972, 0.0000}, 1.0000},
	{lab{50.0000, 2.5000, 0.0000}, lab{50.0000, 1.8634, 0.5757}, 1.0000},
	{lab{50.0000, 2.5000, 0.0000}, lab{50.0000, 3.2592, 0.3350}, 1.0000},
	{lab{60.2574, -34.0099, 36.2677}, lab{60.4626, -34.1751, 39.4387}, 1.2644},
	{lab{63.0109, -31.0961, -5.8663}, lab{62.8187, -29.7946, -4.0864}, 1.2630},
	{lab{61.2901, 3.7196, -5.3901}, lab{61.4292, 2.2480, -4.9620}, 1.8731},
	{lab{35.0831, -44.1164, 3.7933}, lab{35.0232, -40.0716, 1.5901}, 1.8645},
	{lab{22.7233, 20.0904, -46.6940}, lab{23.0331, 14.9730, -42.5619}, 2.0373},
	{lab{36.4612, 47.8580, 18.3852}, lab{36.2715, 50.5065, 21.2231}, 1.4146},
	{lab{90.8027, -2.0831, 1.4410}, lab{91.1528, -1.6435, 0.0447}, 1.4441},
	{lab{90.9257, -0.5406, -0.9208}, lab{88.6381, -0.8985, -0.7239}, 1.5381},
	{lab{6.7747, -0.2908, -2.4247}, lab{5.8714, -0.0985, -2.2286}, 0.6377},
	{lab{2.0776, 0.0795, -1.1350}, lab{0.9033, -0.0636, -0.5514}, 0.9082},
}

// TestQuantiserMetricIsPinned pins the measuring instrument itself: the
// CIEDE2000 implementation must reproduce Sharma's 34 reference values, be
// symmetric, and give exactly 0 for identical colours. Lab conversion is
// checked against the standard sRGB D65 primaries.
func TestQuantiserMetricIsPinned(t *testing.T) {
	var maxErr float64
	for i, p := range sharmaPairs {
		got := deltaE00(p.lab1, p.lab2)
		err := math.Abs(got - p.want)
		if err > maxErr {
			maxErr = err
		}
		// Pair 14 (1-based) sits exactly on the |d h'| = 180 discontinuity of
		// the formula: its two colours are mirror images across the a* axis at
		// a* = +-0.001, so the hue difference is 180.0000 degrees in exact
		// arithmetic and the branch that computes the mean hue is decided by
		// float rounding. Sharma's table records 4.8045 for the "below" side
		// and 4.7461 for the "above" side (pairs 13-16); this implementation
		// lands pair 14 on the above side, 0.058 away from the table entry.
		// Every other pair must match to 1e-4; the boundary pair gets 0.1
		// because both sides of the discontinuity are legitimate values of the
		// formula, not because the metric is loose.
		tol := 1e-4
		if i == 13 {
			tol = 0.1
		}
		if err > tol {
			t.Errorf("Sharma pair %d: dE00 = %.6f, want %.4f (err %.6f)", i+1, got, p.want, err)
		}
		if math.Abs(deltaE00(p.lab1, p.lab2)-deltaE00(p.lab2, p.lab1)) > 1e-9 {
			t.Errorf("Sharma pair %d is asymmetric", i+1)
		}
	}
	if d := deltaE00(lab{50, 10, -20}, lab{50, 10, -20}); d != 0 {
		t.Errorf("identical colours gave dE00 = %v, want exactly 0", d)
	}
	t.Logf("CIEDE2000 vs 34 Sharma reference pairs: max abs error %.2e", maxErr)

	checks := []struct {
		c       Colour
		l, a, b float64
		tol     float64
	}{
		{NewColour(0, 0, 0), 0, 0, 0, 0.01},
		{NewColour(255, 255, 255), 100, 0, 0, 0.01},
		{NewColour(255, 0, 0), 53.24, 80.09, 67.20, 0.05},
		{NewColour(0, 255, 0), 87.73, -86.18, 83.18, 0.05},
		{NewColour(0, 0, 255), 32.30, 79.19, -107.86, 0.05},
		{NewColour(255, 255, 0), 97.14, -21.55, 94.48, 0.05},
	}
	for _, c := range checks {
		r0, g0, b0 := c.c.RGB()
		got := rgbToLab(r0, g0, b0)
		if math.Abs(got.l-c.l) > c.tol || math.Abs(got.a-c.a) > c.tol || math.Abs(got.b-c.b) > c.tol {
			t.Errorf("%v -> Lab(%.2f, %.2f, %.2f), want (%.2f, %.2f, %.2f)",
				c.c, got.l, got.a, got.b, c.l, c.a, c.b)
		}
	}
	// A pure grey must be achromatic: any a* or b* here is an RGB->Lab bug.
	for _, v := range []uint8{0, 0x40, 0x80, 0xc0, 0xff} {
		g := rgbToLab(v, v, v)
		if math.Abs(g.a) > 0.01 || math.Abs(g.b) > 0.01 {
			t.Errorf("grey %02x is not achromatic: %+v", v, g)
		}
	}
}

// ---------------------------------------------------------------------------
// Palettes in Lab, once.
// ---------------------------------------------------------------------------

var perceptualNamedLab [16]lab
var perceptual256Lab [256]lab

func init() {
	for i, p := range NamedPalette {
		perceptualNamedLab[i] = rgbToLab(p[0], p[1], p[2])
	}
	for i, p := range index256Palette {
		perceptual256Lab[i] = rgbToLab(p[0], p[1], p[2])
	}
}

// paletteLab returns the Lab colour of palette entry i at the given rung.
func paletteLab(rung, i int) lab {
	if rung == 16 {
		return perceptualNamedLab[i]
	}
	return perceptual256Lab[i]
}

// paletteHex renders palette entry i at the given rung as #rrggbb.
func paletteHex(rung, i int) string {
	if rung == 16 {
		return fmt.Sprintf("#%02x%02x%02x", NamedPalette[i][0], NamedPalette[i][1], NamedPalette[i][2])
	}
	r, g, b := Index256PaletteRGB(i)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// ---------------------------------------------------------------------------
// Measurement core.
// ---------------------------------------------------------------------------

// colourMeasurement is the gamut/selection decomposition for one source
// colour at one rung.
type colourMeasurement struct {
	total, gamut, selection float64
	pick, ideal             int
}

// measureColour computes the decomposition for src at the given rung using q.
// The oracle ("ideal") is an exhaustive CIEDE2000 search over the rung's
// palette; selection error is clamped at 0 because float noise can make
// total-gamut dip to ~-1e-12 when the pick is optimal.
func measureColour(src Colour, rung int, q Quantiser) colourMeasurement {
	r, g, b := src.RGB()
	srcLab := rgbToLab(r, g, b)
	n := 256
	if rung == 16 {
		n = 16
	}
	ideal, idealD := 0, math.Inf(1)
	for i := 0; i < n; i++ {
		if d := deltaE00(srcLab, paletteLab(rung, i)); d < idealD {
			ideal, idealD = i, d
		}
	}
	pick := int(q.Nearest256(src))
	if rung == 16 {
		pick = int(q.Nearest16(src))
	}
	total := deltaE00(srcLab, paletteLab(rung, pick))
	if total < idealD {
		total = idealD
	}
	selection := total - idealD
	if selection < 0 {
		selection = 0
	}
	return colourMeasurement{total: total, gamut: idealD, selection: selection, pick: pick, ideal: ideal}
}

// naiveCubeIndex is the baseline a perceptual mapping must beat: plain 5-bit
// truncation of each channel onto the 6x6x6 cube. It ignores the named
// colours and the grey ramp entirely.
func naiveCubeIndex(r, g, b uint8) int {
	return 16 + int(r>>3)*36 + int(g>>3)*6 + int(b>>3)
}

// sweepRow is one lattice sample, stored compactly so a 140k sweep stays in
// cache-friendly territory; hex strings are formatted only for reporting.
type sweepRow struct {
	r, g, b     uint8
	lstar       float64
	total       float64
	selection   float64
	pick, ideal int
}

// stat is the five-number summary of a sample.
type stat struct {
	n                     int
	mean, median          float64
	p95, p99, max         float64
	over23, over5, over10 int
}

// summarise computes the five-number summary plus exceedance counts (the
// counts are meaningful only for selection-error samples; they are computed
// for all samples but reported for selection).
func summarise(ds []float64) stat {
	s := stat{n: len(ds)}
	if len(ds) == 0 {
		return s
	}
	cp := append([]float64(nil), ds...)
	sort.Float64s(cp)
	total := 0.0
	for _, v := range cp {
		total += v
		if v > 2.3 {
			s.over23++
		}
		if v > 5 {
			s.over5++
		}
		if v > 10 {
			s.over10++
		}
	}
	s.mean = total / float64(len(cp))
	s.median = cp[len(cp)/2]
	s.p95 = cp[int(0.95*float64(len(cp)-1))]
	s.p99 = cp[int(0.99*float64(len(cp)-1))]
	s.max = cp[len(cp)-1]
	return s
}

// logStat writes one line of the five-number summary.
func logStat(t *testing.T, name string, s stat) {
	t.Helper()
	t.Logf("%-14s n=%d mean=%.3f median=%.3f p95=%.3f p99=%.3f max=%.3f", name, s.n, s.mean, s.median, s.p95, s.p99, s.max)
}

// TestQuantiserPaletteMembersAreFixedPoints is the exactness invariant: every
// colour that IS in the rung's palette must map to itself. A palette member
// has zero gamut error, so any non-zero error here is 100% selection error —
// the quantiser failing on the easiest possible input. It also pins the one
// legitimate duplicate in the xterm palette (#808080 at indices 8 and 244):
// the pick must land on an identical RGB, whichever index wins the tie.
func TestQuantiserPaletteMembersAreFixedPoints(t *testing.T) {
	q := DefaultQuantiser{}
	for i, p := range index256Palette {
		c := NewColour(p[0], p[1], p[2])
		pick := int(q.Nearest256(c))
		pr, pg, pb := Index256PaletteRGB(pick)
		if pr != p[0] || pg != p[1] || pb != p[2] {
			t.Errorf("rung 256: palette member %d (#%02x%02x%02x) mapped to index %d (#%02x%02x%02x)",
				i, p[0], p[1], p[2], pick, pr, pg, pb)
		}
	}
	for i, p := range NamedPalette {
		c := NewColour(p[0], p[1], p[2])
		if pick := int(q.Nearest16(c)); pick != i {
			t.Errorf("rung 16: named colour %d (#%02x%02x%02x) mapped to index %d", i, p[0], p[1], p[2], pick)
		}
	}
}

// TestQuantiserEffectiveDistanceIsPinned pins what redmean in colour.go
// actually computes. The function's rmean weighting divides by 256 in integer
// arithmetic; for every pair of uint8 channels rmean is in 0..255, so
// rmean/256 == 0 and (255-rmean)/256 == 0 and the weights are identically 2.
// The effective distance is therefore fixed-weight 2*dr^2 + 4*dg^2 + 2*db^2
// in gamma-space RGB — a much cruder metric than redmean, and the direct
// cause of the hue-flip selection errors the sweep measures.
//
// This test deliberately FAILS if the formula is ever changed to true
// floating-point redmean (or to anything else): such a change alters every
// 256/16-colour byte the renderer emits, so it must be a deliberate decision
// that re-runs the whole audit and re-pins the thresholds — not a drive-by
// "fix" to a division.
func TestQuantiserEffectiveDistanceIsPinned(t *testing.T) {
	probes := [][6]uint8{
		// (r1,g1,b1,r2,g2,b2) spanning the rmean range: red-dominated,
		// green-dominated, near-equal, and the extremes.
		{255, 0, 0, 0, 255, 0},
		{255, 255, 255, 0, 0, 0},
		{230, 85, 60, 128, 128, 0},
		{155, 50, 40, 175, 0, 0},
		{1, 2, 3, 254, 253, 252},
		{200, 200, 200, 201, 199, 200},
		{0, 0, 0, 0, 0, 1},
	}
	for _, p := range probes {
		dr := int32(p[0]) - int32(p[3])
		dg := int32(p[1]) - int32(p[4])
		db := int32(p[2]) - int32(p[5])
		fixed := uint32(2*dr*dr + 4*dg*dg + 2*db*db)
		if got := redmean(p[0], p[1], p[2], p[3], p[4], p[5]); got != fixed {
			t.Errorf("redmean(%d,%d,%d,%d,%d,%d) = %d, fixed (2,4,2) gives %d — the effective metric changed",
				p[0], p[1], p[2], p[3], p[4], p[5], got, fixed)
		}
	}
}

// Example palettes, transcribed from the applications that ship with the
// framework. examples/search/theme.go and examples/markets/theme.go are
// package main and cannot be imported from a buffer test, so the RGB triples
// are copied here; the values are checked against the files by eye whenever
// either theme changes.
type themeColour struct {
	name string
	rgb  [3]uint8
}

var exampleThemeColours = []themeColour{
	// examples/search/theme.go
	{"search.bg", [3]uint8{0x0e, 0x11, 0x16}},
	{"search.panelBG", [3]uint8{0x14, 0x19, 0x21}},
	{"search.fg", [3]uint8{0xd8, 0xdc, 0xe4}},
	{"search.dim", [3]uint8{0x6a, 0x74, 0x82}},
	{"search.edge", [3]uint8{0x2c, 0x33, 0x3d}},
	{"search.accent", [3]uint8{0x7a, 0xc2, 0xe8}},
	{"search.warn", [3]uint8{0xd8, 0xa8, 0x40}},
	// examples/markets/theme.go
	{"markets.bg", [3]uint8{0x0e, 0x11, 0x16}},
	{"markets.panelBG", [3]uint8{0x14, 0x19, 0x21}},
	{"markets.fg", [3]uint8{0xd8, 0xdc, 0xe4}},
	{"markets.dim", [3]uint8{0x66, 0x70, 0x7e}},
	{"markets.edge", [3]uint8{0x2c, 0x33, 0x3d}},
	{"markets.up", [3]uint8{0x3c, 0xc8, 0x8c}},
	{"markets.down", [3]uint8{0xd8, 0x6a, 0x62}},
	{"markets.flat", [3]uint8{0x8a, 0x93, 0xa0}},
	{"markets.accent", [3]uint8{0x54, 0xa8, 0xf0}},
	{"markets.warn", [3]uint8{0xd8, 0xa8, 0x40}},
}

// TestQuantiserOnExampleThemes measures the quantiser on the colours the
// shipped examples actually paint. This is the evidence the DECIDED/REPLACE
// recommendation rests on: a theme colour is a colour a user sees, so its
// error — both the part the palette forces and the part the quantiser adds —
// is the number that matters for this project.
func TestQuantiserOnExampleThemes(t *testing.T) {
	q := DefaultQuantiser{}
	for _, tc := range exampleThemeColours {
		src := NewColour(tc.rgb[0], tc.rgb[1], tc.rgb[2])
		srcHex := fmt.Sprintf("#%02x%02x%02x", tc.rgb[0], tc.rgb[1], tc.rgb[2])
		srcLab := rgbToLab(tc.rgb[0], tc.rgb[1], tc.rgb[2])
		for _, d := range []struct {
			rung, maxTotal, maxSel float64
			label                  string
		}{
			{256, themeMaxTotal256, themeMaxSelection256, "256"},
			{16, themeMaxTotal16, themeMaxSelection16, "16"},
		} {
			rung := int(d.rung)
			m := measureColour(src, rung, q)
			t.Logf("%-15s %s | rung %s: pick %3d %s total=%.3f gamut=%.3f selection=%.3f (ideal %3d %s)",
				tc.name, srcHex, d.label, m.pick, paletteHex(rung, m.pick), m.total, m.gamut, m.selection,
				m.ideal, paletteHex(rung, m.ideal))
			if m.selection > d.maxSel {
				t.Errorf("%s (%s) at rung %s: selection error %.3f exceeds %.3f — the quantiser picked %s where %s is closer (L*=%.1f)",
					tc.name, srcHex, d.label, m.selection, d.maxSel,
					paletteHex(rung, m.pick), paletteHex(rung, m.ideal), srcLab.l)
			}
			if m.total > d.maxTotal {
				t.Errorf("%s (%s) at rung %s: total dE00 %.3f exceeds %.3f (gamut %.3f + selection %.3f)",
					tc.name, srcHex, d.label, m.total, d.maxTotal, m.gamut, m.selection)
			}
		}
	}
}

// TestQuantiserPerceptualQuality is the sweep. It measures both rungs over the
// step-5 lattice, decomposes total into gamut and selection, reports the
// five-number summary of each, compares redmean against naive 5-bit
// truncation as a baseline, refines the worst lattice points in their
// neighbourhoods, and asserts the thresholds pinned at the top of this file.
//
// Runtime: ~12 s plain, ~75-80 s under -race (measured; race instrumentation
// inflates the CIEDE2000 loop heavily). Intentionally not gated behind
// testing.Short(): this test IS the evidence for the colour-model decision,
// and CI runs it in full.
func TestQuantiserPerceptualQuality(t *testing.T) {
	q := DefaultQuantiser{}
	const step = 5
	const refineRadius = 4
	const refineTop = 20

	type rungReport struct {
		label        string
		rung         int
		total, gamut []float64
		selection    []float64
		rows         []sweepRow
	}
	reports := []*rungReport{
		{label: "256", rung: 256},
		{label: "16", rung: 16},
	}
	var naiveTotal, naiveSelection []float64

	for r := 0; r < 256; r += step {
		for g := 0; g < 256; g += step {
			for b := 0; b < 256; b += step {
				src := NewColour(uint8(r), uint8(g), uint8(b))
				srcLab := rgbToLab(uint8(r), uint8(g), uint8(b))
				for _, rep := range reports {
					m := measureColour(src, rep.rung, q)
					rep.total = append(rep.total, m.total)
					rep.gamut = append(rep.gamut, m.gamut)
					rep.selection = append(rep.selection, m.selection)
					rep.rows = append(rep.rows, sweepRow{
						r: uint8(r), g: uint8(g), b: uint8(b), lstar: srcLab.l,
						total: m.total, selection: m.selection, pick: m.pick, ideal: m.ideal,
					})
				}
				// Naive 5-bit truncation baseline against the same 256 oracle.
				ni := naiveCubeIndex(uint8(r), uint8(g), uint8(b))
				nr, ng, nb := Index256PaletteRGB(ni)
				nd := deltaE00(srcLab, rgbToLab(nr, ng, nb))
				// The oracle for this colour was computed inside measureColour;
				// recompute the gamut term cheaply from the 256 report.
				ngamut := reports[0].gamut[len(reports[0].gamut)-1]
				if nd < ngamut {
					nd = ngamut
				}
				naiveTotal = append(naiveTotal, nd)
				naiveSelection = append(naiveSelection, nd-ngamut)
			}
		}
	}

	for _, rep := range reports {
		tot := summarise(rep.total)
		gam := summarise(rep.gamut)
		sel := summarise(rep.selection)
		t.Logf("=== rung %s (lattice step %d, n=%d per vector) ===", rep.label, step, tot.n)
		logStat(t, "  total", tot)
		logStat(t, "  gamut", gam)
		logStat(t, "  selection", sel)
		t.Logf("  selection exceedances: >2.3 (JND): %.4f%%  >5: %.6f%%  >10: %.6f%%",
			100*float64(sel.over23)/float64(sel.n), 100*float64(sel.over5)/float64(sel.n), 100*float64(sel.over10)/float64(sel.n))

		// Worst lattice cases, most significant first.
		rows := append([]sweepRow(nil), rep.rows...)
		sort.Slice(rows, func(i, j int) bool { return rows[i].selection > rows[j].selection })
		t.Logf("  worst selection-error lattice cases (top %d):", refineTop)
		for i := 0; i < refineTop && i < len(rows); i++ {
			w := rows[i]
			t.Logf("    sel=%.3f total=%.3f L*=%.1f #%02x%02x%02x -> pick %d %s | ideal %d %s",
				w.selection, w.total, w.lstar, w.r, w.g, w.b, w.pick, paletteHex(rep.rung, w.pick),
				w.ideal, paletteHex(rep.rung, w.ideal))
		}

		// Refinement: re-scan the +-radius neighbourhood of the worst lattice
		// points so the asserted max is not an artefact of lattice alignment.
		refinedMax := sel.max
		var refinedWorst sweepRow
		for i := 0; i < refineTop && i < len(rows); i++ {
			base := rows[i]
			for dr := -refineRadius; dr <= refineRadius; dr++ {
				for dg := -refineRadius; dg <= refineRadius; dg++ {
					for db := -refineRadius; db <= refineRadius; db++ {
						nr, ng, nb := int(base.r)+dr, int(base.g)+dg, int(base.b)+db
						if nr < 0 || nr > 255 || ng < 0 || ng > 255 || nb < 0 || nb > 255 {
							continue
						}
						m := measureColour(NewColour(uint8(nr), uint8(ng), uint8(nb)), rep.rung, q)
						if m.selection > refinedMax {
							refinedMax = m.selection
							refinedWorst = sweepRow{
								r: uint8(nr), g: uint8(ng), b: uint8(nb),
								lstar: rgbToLab(uint8(nr), uint8(ng), uint8(nb)).l,
								total: m.total, selection: m.selection, pick: m.pick, ideal: m.ideal,
							}
						}
					}
				}
			}
		}
		t.Logf("  refined selection max over +- %d neighbourhoods of top %d: %.3f", refineRadius, refineTop, refinedMax)
		if refinedMax > sel.max {
			t.Logf("  refined worst case: sel=%.3f total=%.3f L*=%.1f #%02x%02x%02x -> pick %d %s | ideal %d %s",
				refinedWorst.selection, refinedWorst.total, refinedWorst.lstar,
				refinedWorst.r, refinedWorst.g, refinedWorst.b,
				refinedWorst.pick, paletteHex(rep.rung, refinedWorst.pick),
				refinedWorst.ideal, paletteHex(rep.rung, refinedWorst.ideal))
		}

		// Worst cases among plausible UI colours: L* >= 40, i.e. light
		// enough to be a foreground on a dark background or a mid-tone. A max
		// on a near-black nobody would paint is a different finding from a
		// max on stAccent.
		var ui []sweepRow
		for _, w := range rep.rows {
			if w.lstar >= 40 {
				ui = append(ui, w)
			}
		}
		sort.Slice(ui, func(i, j int) bool { return ui[i].selection > ui[j].selection })
		if len(ui) > 0 {
			t.Logf("  worst selection-error cases with L*>=40 (%d colours):", len(ui))
			for i := 0; i < 10 && i < len(ui); i++ {
				w := ui[i]
				t.Logf("    sel=%.3f total=%.3f L*=%.1f #%02x%02x%02x -> pick %d %s | ideal %d %s",
					w.selection, w.total, w.lstar, w.r, w.g, w.b, w.pick, paletteHex(rep.rung, w.pick),
					w.ideal, paletteHex(rep.rung, w.ideal))
			}
		}

		// Threshold assertions.
		maxBound, meanBound := perceptualMaxSelection256, perceptualMeanSelection256
		if rep.rung == 16 {
			maxBound, meanBound = perceptualMaxSelection16, perceptualMeanSelection16
		}
		if refinedMax > maxBound {
			t.Errorf("rung %s: refined max selection error %.3f exceeds %.3f (worst: #%02x%02x%02x L*=%.1f -> %s, ideal %s)",
				rep.label, refinedMax, maxBound, refinedWorst.r, refinedWorst.g, refinedWorst.b,
				refinedWorst.lstar, paletteHex(rep.rung, refinedWorst.pick), paletteHex(rep.rung, refinedWorst.ideal))
		}
		if sel.mean > meanBound {
			t.Errorf("rung %s: mean selection error %.4f exceeds %.4f", rep.label, sel.mean, meanBound)
		}
	}

	nt := summarise(naiveTotal)
	ns := summarise(naiveSelection)
	logStat(t, "naive total", nt)
	logStat(t, "naive select", ns)
	qTot := summarise(reports[0].total)
	if qTot.mean >= nt.mean {
		t.Errorf("redmean mean total dE00 %.3f no longer beats naive 5-bit truncation %.3f", qTot.mean, nt.mean)
	}
	t.Logf("redmean vs naive truncation, 256 rung mean total dE00: %.3f vs %.3f (redmean is %.2fx closer to optimal on average)",
		qTot.mean, nt.mean, nt.mean/qTot.mean)
}
