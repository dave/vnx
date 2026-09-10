package vnx_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
	"github.com/dave/vnx/go/pkg/vnx/geojson"
	"github.com/dave/vnx/go/pkg/vnx/gpx"
	"github.com/dave/vnx/go/pkg/vnx/kml"
)

// The WRITER OBLIGATION (spec §4.1): a document this library writes carries no
// coordinate finer than vnx.CoordinateDecimals and no elevation finer than
// vnx.ElevationDecimals. round.go states the rule and the four encoders each
// call it; nothing until this file asserted the RESULT.
//
// ⚠ THE GOLDENS CANNOT WITNESS IT. Rounding changed only the goldens whose
// fixtures happened to hold noisy numbers, and a golden says "these bytes",
// not "no writer emits more than six places" — the next channel added to a
// position, or the next writer, is outside every one of them. This test reads
// the EMITTED BYTES of every accept corpus fixture through all four writers and
// asks the question directly.
//
// ⚠ IT IS THE COUNTERPART OF
// TestTheReaderPreservesFullPrecisionAndTheWriterRounds below. A writer rounds
// at the boundary; a reader preserves whatever it was handed. The two halves
// are stated together on purpose, because reading either alone suggests the
// other is a bug.

// maxCoordinateDecimals and maxElevationDecimals are the constants restated as
// this test's expectation, deliberately NOT read from vnx. A test that imports
// the constant it is guarding moves with it, and the point here is that the
// ruled numbers — 6 and 1 — do not change silently.
const (
	maxCoordinateDecimals = 6
	maxElevationDecimals  = 1
)

// foundPos is one position as a writer SPELLED it. The literals are the exact
// text from the output, not parsed doubles: the obligation is about how many
// digits leave the file, so parsing them and spelling them again would ask a
// different question.
type foundPos struct {
	channel string // which geometry channel it came from, for the vacuity guard
	path    string // where in the output, for the failure message
	lon     string
	lat     string
	ele     string // "" when the channel carried none
}

func TestWritersRoundEveryCoordinateTheyEmit(t *testing.T) {
	fixtures := append(loadAcceptFixtures(t), noisyFixture())

	// Fixtures that carry no geometry AT ALL, named rather than skipped by a
	// zero check.
	//
	// ⚠ THE VACUITY GUARD BELOW IS THE POINT OF THIS FILE, so a fixture is
	// excused from it only by being listed here. Letting the guard skip any
	// fixture whose walk came back empty would excuse exactly the fixture that
	// had silently LOST its positions, which is the case it exists to catch.
	// accept/empty-collections.vnx tests the difference between an absent
	// collection and an empty one (spec §3.1) and holds no positions by design.
	noGeometry := map[string]bool{"empty-collections.vnx": true}
	if n := len(noGeometry); n > 1 {
		t.Logf("%d fixtures are excused the vacuity guard; keep that list short", n)
	}

	// Every channel a VNX position can appear in. If the walk stops visiting
	// one — a key renamed, a nesting level added — this set goes short and the
	// test says which channel it lost, rather than passing on the ones it can
	// still see. That is the failure mode this whole file exists to avoid.
	wantChannels := []string{
		"area.polygon",
		"chain.link.endpoint",
		"network.arc.pips",
		"network.area.polygon",
		"network.node",
		"network.point.at",
		"route.arc.pips",
		"track.segment.pips",
		"waypoint.at",
	}
	seenChannels := map[string]bool{}

	totals := map[string]int{}
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			for _, w := range writersUnderTest() {
				var b bytes.Buffer
				if err := w.write(&b, f.doc); err != nil {
					t.Fatalf("%s: %v", w.name, err)
				}
				got := w.positions(t, b.Bytes())
				// The vacuity guard. A corpus-wide loop that examines zero
				// numbers passes every assertion in it, so a fixture whose
				// shape means the walk found nothing is a FAILURE — either the
				// walk is broken or the fixture does not belong in a writer
				// test.
				if len(got) == 0 && w.everyDocumentHasGeometry && !noGeometry[f.name] {
					t.Fatalf("%s: the walk found no positions at all in %d bytes of output — this assertion is vacuous",
						w.name, b.Len())
				}
				totals[w.name] += len(got)
				for _, p := range got {
					if w.name == "vnx" {
						seenChannels[p.channel] = true
					}
					checkPlaces(t, w.name, p)
				}
			}
		})
	}

	for _, name := range wantChannels {
		if !seenChannels[name] {
			t.Errorf("no accept fixture exercised the %s channel; the walk is not visiting it, or the corpus no longer covers it", name)
		}
	}
	// And nothing NEW appeared unremarked: a channel the walk names but the
	// list does not is a channel someone added without deciding it rounds.
	for name := range seenChannels {
		if !contains(wantChannels, name) {
			t.Errorf("the walk visited channel %q, which is not in wantChannels — add it deliberately", name)
		}
	}
	keys := make([]string, 0, len(totals))
	for k := range totals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Logf("%s: %d positions checked across %d fixtures", k, totals[k], len(fixtures))
	}
	// ⚠ THE CORPUS-WIDE VACUITY GUARD, and the only one the three LOSSY writers
	// have. The per-fixture guard above is gated on everyDocumentHasGeometry,
	// which is true of VNX alone, so until 2026-09-04 the totals below were
	// logged and never asserted: if the GPX token matcher or the KML tuple
	// splitter stopped matching — an element renamed, a namespace added — that
	// writer would be checked against ZERO positions and this test would stay
	// green while asserting nothing about it.
	//
	// It is a corpus-wide claim on purpose. Any one lossy writer may find
	// nothing in any one fixture (GPX has no polygon), but no writer can find
	// nothing across the whole accept corpus.
	for _, w := range writersUnderTest() {
		if totals[w.name] == 0 {
			t.Errorf("%s: the walk found no positions in ANY of the %d fixtures — the extractor is broken, not the corpus empty; every assertion made about %s above was vacuous",
				w.name, len(fixtures), w.name)
		}
	}
}

// checkPlaces is the assertion itself.
func checkPlaces(t *testing.T, format string, p foundPos) {
	t.Helper()
	for _, c := range []struct {
		what string
		lit  string
		max  int
	}{
		{"longitude", p.lon, maxCoordinateDecimals},
		{"latitude", p.lat, maxCoordinateDecimals},
		{"elevation", p.ele, maxElevationDecimals},
	} {
		if c.lit == "" {
			continue
		}
		n, err := decimalPlaces(c.lit)
		if err != nil {
			t.Errorf("%s %s: %s %q is not a number: %v", format, p.path, c.what, c.lit, err)
			continue
		}
		if n > c.max {
			t.Errorf("%s %s: %s written as %s — %d decimal places, and a writer may emit at most %d",
				format, p.path, c.what, c.lit, n, c.max)
		}
	}
}

// decimalPlaces counts the decimal places a NUMERIC LITERAL carries, after its
// exponent is applied: 1e-7 is seven places, 3.5e2 is none. Trailing zeros do
// not count, so `320.0` is zero places and not one — they add no precision and
// no writer here emits them anyway.
func decimalPlaces(lit string) (int, error) {
	if _, err := strconv.ParseFloat(lit, 64); err != nil {
		return 0, err
	}
	mant, exp := lit, 0
	if i := strings.IndexAny(lit, "eE"); i >= 0 {
		mant = lit[:i]
		e, err := strconv.Atoi(strings.TrimPrefix(lit[i+1:], "+"))
		if err != nil {
			return 0, err
		}
		exp = e
	}
	frac := 0
	if i := strings.IndexByte(mant, '.'); i >= 0 {
		frac = len(strings.TrimRight(mant[i+1:], "0"))
	}
	if n := frac - exp; n > 0 {
		return n, nil
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// The writers under test
// ---------------------------------------------------------------------------

type writerUnderTest struct {
	name      string
	write     func(w io.Writer, d *vnx.Document) error
	positions func(t *testing.T, out []byte) []foundPos
	// everyDocumentHasGeometry says the vacuity guard applies per fixture. VNX
	// is lossless, so every accept fixture round-trips its geometry and a walk
	// that finds nothing is broken. The three lossy writers legitimately drop
	// whole item kinds — GPX has no polygon — so for them the guard is the
	// corpus-wide channel coverage above plus their own goldens.
	everyDocumentHasGeometry bool
}

func writersUnderTest() []writerUnderTest {
	report := func(f func(io.Writer, *vnx.Document) (vnx.WriteReport, error)) func(io.Writer, *vnx.Document) error {
		return func(w io.Writer, d *vnx.Document) error {
			_, err := f(w, d)
			return err
		}
	}
	return []writerUnderTest{
		{"vnx", vnx.EncodeTo, vnxPositions, true},
		{"geojson", report(geojson.Write), geojsonPositions, false},
		{"gpx", report(gpx.Write), gpxPositions, false},
		{"kml", report(kml.Write), kmlPositions, false},
	}
}

// ---------------------------------------------------------------------------
// VNX: walk the emitted JSON by the shape the format defines
// ---------------------------------------------------------------------------

// vnxPositions navigates to each position channel by name rather than
// collecting every number in the file. That distinction is load-bearing:
// unknown `data` records are opaque passthrough and carry whatever numbers
// their author wrote, and a blind number sweep would demand a writer round a
// payload it must preserve.
func vnxPositions(t *testing.T, out []byte) []foundPos {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber() // keep each number's original spelling
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		t.Fatalf("emitted VNX does not parse: %v", err)
	}
	var found []foundPos
	add := func(channel, path string, v any) {
		if p, ok := asPosition(v); ok {
			p.channel, p.path = channel, path
			found = append(found, p)
		}
	}
	addEach := func(channel, path string, v any) {
		for i, e := range asArray(v) {
			add(channel, fmt.Sprintf("%s[%d]", path, i), e)
		}
	}

	for i, w := range asArray(doc["waypoints"]) {
		add("waypoint.at", fmt.Sprintf("/waypoints/%d/at", i), asObject(w)["at"])
	}
	for i, a := range asArray(doc["areas"]) {
		addEach("area.polygon", fmt.Sprintf("/areas/%d/polygon", i), asObject(a)["polygon"])
	}
	for i, r := range asArray(doc["routes"]) {
		for j, arc := range asArray(asObject(r)["arcs"]) {
			addEach("route.arc.pips", fmt.Sprintf("/routes/%d/arcs/%d/pips", i, j), asObject(arc)["pips"])
		}
	}
	for i, tr := range asArray(doc["tracks"]) {
		for j, seg := range asArray(asObject(tr)["segments"]) {
			addEach("track.segment.pips", fmt.Sprintf("/tracks/%d/segments/%d/pips", i, j), asObject(seg)["pips"])
		}
	}
	for i, n := range asArray(doc["networks"]) {
		net := asObject(n)
		base := fmt.Sprintf("/networks/%d", i)
		for id, node := range asObject(net["nodes"]) {
			add("network.node", base+"/nodes/"+id, node)
		}
		for id, arc := range asObject(net["arcs"]) {
			addEach("network.arc.pips", base+"/arcs/"+id+"/pips", asObject(arc)["pips"])
		}
		for j, a := range asArray(net["areas"]) {
			addEach("network.area.polygon", fmt.Sprintf("%s/areas/%d/polygon", base, j), asObject(a)["polygon"])
		}
		for j, p := range asArray(net["points"]) {
			add("network.point.at", fmt.Sprintf("%s/points/%d/at", base, j), asObject(p)["at"])
		}
		for j, c := range asArray(net["chains"]) {
			for k, l := range asArray(asObject(c)["links"]) {
				link := asObject(l)
				// An endpoint is a node id (a string, nothing to round) or an
				// INLINE position. asPosition ignores the string form.
				for _, end := range []string{"from", "to"} {
					add("chain.link.endpoint",
						fmt.Sprintf("%s/chains/%d/links/%d/%s", base, j, k, end), link[end])
				}
			}
		}
	}
	return found
}

// asPosition recognises the positional array [lon, lat, ele?, time?]. Anything
// else — a node-id string, an absent key — is not a position and reports false.
func asPosition(v any) (foundPos, bool) {
	a, ok := v.([]any)
	if !ok || len(a) < 2 {
		return foundPos{}, false
	}
	lon, ok1 := a[0].(json.Number)
	lat, ok2 := a[1].(json.Number)
	if !ok1 || !ok2 {
		return foundPos{}, false
	}
	p := foundPos{lon: lon.String(), lat: lat.String()}
	// The third channel is elevation, and it is `null` when a timed pip has no
	// elevation. Time, the fourth, is an integer count of milliseconds and has
	// nothing to round.
	if len(a) > 2 {
		if ele, ok := a[2].(json.Number); ok {
			p.ele = ele.String()
		}
	}
	return p, true
}

func asArray(v any) []any {
	a, _ := v.([]any)
	return a
}

func asObject(v any) map[string]any {
	o, _ := v.(map[string]any)
	return o
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// GeoJSON: every `coordinates` member, at whatever depth its geometry nests it
// ---------------------------------------------------------------------------

func geojsonPositions(t *testing.T, out []byte) []foundPos {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	var fc map[string]any
	if err := dec.Decode(&fc); err != nil {
		t.Fatalf("emitted GeoJSON does not parse: %v", err)
	}
	var found []foundPos
	for i, f := range asArray(fc["features"]) {
		g := asObject(asObject(f)["geometry"])
		walkGeoJSONCoords(g["coordinates"], fmt.Sprintf("/features/%d/geometry/coordinates", i), &found)
	}
	return found
}

// walkGeoJSONCoords descends the nesting a Point, LineString, Polygon or Multi*
// puts around its positions, which is one to three array levels. It recurses on
// arrays and stops at the first one asPosition recognises, so the depth never
// has to be known per geometry type.
func walkGeoJSONCoords(v any, path string, out *[]foundPos) {
	if p, ok := asPosition(v); ok {
		p.channel, p.path = "geojson.coordinates", path
		*out = append(*out, p)
		return
	}
	for i, e := range asArray(v) {
		walkGeoJSONCoords(e, fmt.Sprintf("%s[%d]", path, i), out)
	}
}

// ---------------------------------------------------------------------------
// GPX: lat/lon are attributes, elevation is a child element
// ---------------------------------------------------------------------------

func gpxPositions(t *testing.T, out []byte) []foundPos {
	t.Helper()
	var found []foundPos
	dec := xml.NewDecoder(bytes.NewReader(out))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("emitted GPX does not parse: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "wpt", "trkpt", "rtept":
			p := foundPos{channel: "gpx." + se.Name.Local, path: se.Name.Local}
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "lat":
					p.lat = a.Value
				case "lon":
					p.lon = a.Value
				}
			}
			if p.lat == "" || p.lon == "" {
				t.Fatalf("GPX <%s> has no lat/lon attributes", se.Name.Local)
			}
			found = append(found, p)
		case "ele":
			// <ele> belongs to the point that opened before it. There is
			// always one: GPX has no elevation outside a point.
			var text string
			if err := dec.DecodeElement(&text, &se); err != nil {
				t.Fatalf("GPX <ele>: %v", err)
			}
			if len(found) == 0 {
				t.Fatal("GPX <ele> outside any point")
			}
			found[len(found)-1].ele = strings.TrimSpace(text)
		}
	}
	return found
}

// ---------------------------------------------------------------------------
// KML: whitespace-separated lon,lat[,ele] tuples inside <coordinates>
// ---------------------------------------------------------------------------

func kmlPositions(t *testing.T, out []byte) []foundPos {
	t.Helper()
	var found []foundPos
	dec := xml.NewDecoder(bytes.NewReader(out))
	n := 0
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("emitted KML does not parse: %v", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "coordinates" {
			continue
		}
		var text string
		if err := dec.DecodeElement(&text, &se); err != nil {
			t.Fatalf("KML <coordinates>: %v", err)
		}
		for _, tuple := range strings.Fields(text) {
			parts := strings.Split(tuple, ",")
			if len(parts) < 2 {
				t.Fatalf("KML coordinate tuple %q has no latitude", tuple)
			}
			p := foundPos{
				channel: "kml.coordinates",
				path:    fmt.Sprintf("coordinates[%d]", n),
				lon:     parts[0],
				lat:     parts[1],
			}
			if len(parts) > 2 {
				p.ele = parts[2]
			}
			found = append(found, p)
			n++
		}
	}
	return found
}

// ---------------------------------------------------------------------------
// The reader half of the rule
// ---------------------------------------------------------------------------

// TestTheReaderPreservesFullPrecisionAndTheWriterRounds pins the SURPRISING
// half of §4.1's rounding rule, and it is deliberate rather than an
// inconsistency.
//
// A READER preserves what it was handed; a WRITER rounds what it emits. The two
// halves are asserted on one number in one fixture so the distinction cannot be
// read as an accident.
//
// The writer's obligation is asserted corpus-wide by
// TestWritersRoundEveryCoordinateTheyEmit above: everything this library
// ORIGINATES goes out at 6/1 because every encoder rounds at its boundary. Do
// not "fix" this into agreement — deleting either half breaks the other's
// guarantee.
func TestTheReaderPreservesFullPrecisionAndTheWriterRounds(t *testing.T) {
	path := filepath.Join("..", "..", "..", "corpus", "accept", "full-precision-coordinate.vnx")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const fullPrecision = "-3.6712345678901235"
	if !bytes.Contains(data, []byte(fullPrecision)) {
		t.Fatalf("%s no longer holds %s; this test would pass vacuously", path, fullPrecision)
	}

	doc, _, err := vnx.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Waypoints[0].At.Lon != -3.6712345678901235 {
		t.Fatalf("the reader lost precision before the writer was asked: %v", doc.Waypoints[0].At.Lon)
	}
	enc, err := vnx.Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc, []byte(fullPrecision)) {
		t.Errorf("Encode emitted full precision; a writer rounds at the boundary:\n%s", enc)
	}
	if !bytes.Contains(enc, []byte("-3.671235")) {
		t.Errorf("Encode did not emit the rounded coordinate -3.671235:\n%s", enc)
	}
}

// ---------------------------------------------------------------------------
// The noisy fixture
// ---------------------------------------------------------------------------

// noisyFixture is a synthetic document carrying the digits the ruling is about,
// in every channel at once.
//
// ⚠ THE CORPUS ALONE CANNOT RED-PROOF THE ELEVATION HALF. Measured: with every
// RoundElevation call deleted from all four writers, the accept corpus goes red
// on coordinates and stays GREEN on elevations, because not one accept fixture
// holds an elevation with more than one decimal place. The corpus is a
// conformance corpus, not a noise generator, and adding a computed tail to its
// files would change what those cases are about. So the noise lives here, and
// the elevation obligation is guarded rather than merely stated.
func noisyFixture() acceptFixture {
	ele := func(v float64) *float64 { return &v }
	// The tails are what real arithmetic leaves: an interpolated elevation and
	// a coordinate that has been through a projection and back.
	noisy := func(n int) vnx.Position {
		return vnx.Position{
			Lon: -3.6712345678901235 + float64(n)*1e-13,
			Lat: 57.11812345678901 + float64(n)*1e-13,
			Ele: ele(328.06075289469285 + float64(n)),
		}
	}
	ring := []vnx.Position{noisy(0), noisy(1), noisy(2), noisy(0)}
	doc := &vnx.Document{
		Waypoints: []vnx.Waypoint{{Name: "Noisy waypoint", At: noisy(0)}},
		Areas:     []vnx.Area{{Name: "Noisy area", Polygon: ring}},
		Tracks: []vnx.Track{{
			Name:     "Noisy track",
			Segments: []vnx.Segment{{Pips: []vnx.Position{noisy(0), noisy(1), noisy(2)}}},
		}},
		Routes: []vnx.Route{{
			Name: "Noisy route",
			Arcs: []vnx.Arc{{Pips: []vnx.Position{noisy(0), noisy(1)}}},
		}},
		Networks: []vnx.Network{{
			Name:  "Noisy network",
			Nodes: map[string]vnx.Position{"n1": noisy(0), "n2": noisy(2)},
			Arcs: map[string]vnx.Arc{
				"a1": {From: "n1", To: "n2", Pips: []vnx.Position{noisy(0), noisy(1), noisy(2)}},
			},
			Points: []vnx.Waypoint{{Name: "Noisy point", At: noisy(1)}},
			Areas:  []vnx.Area{{Name: "Noisy network area", Polygon: ring}},
			Chains: []vnx.Chain{{
				Name: "Noisy chain",
				// One node endpoint and one INLINE endpoint, which is the only
				// endpoint form that carries coordinates of its own.
				Links: []vnx.Link{{
					Arc:  "a1",
					From: vnx.Endpoint{Node: "n1"},
					To:   vnx.Endpoint{At: &[]vnx.Position{noisy(1)}[0]},
				}},
			}},
		}},
	}
	return acceptFixture{name: "synthetic-noisy", doc: doc}
}

// ---------------------------------------------------------------------------
// Corpus loading
// ---------------------------------------------------------------------------

type acceptFixture struct {
	name string
	doc  *vnx.Document
}

// loadAcceptFixtures reads every accept case as a Document. It deliberately
// reads the DIRECTORY rather than a list: a fixture added later is covered by
// this test the day it lands, which is the only way a conformance corpus and
// the tests over it stay in step.
func loadAcceptFixtures(t *testing.T) []acceptFixture {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "corpus", "accept")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []acceptFixture
	for _, e := range entries {
		name := e.Name()
		isVNX := strings.HasSuffix(name, ".vnx")
		isVNZ := strings.HasSuffix(name, ".vnz")
		if !isVNX && !isVNZ {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var doc *vnx.Document
		if isVNZ {
			ct, _, err := vnx.DecodeContainer(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			doc = ct.Document
		} else if doc, _, err = vnx.Decode(data); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out = append(out, acceptFixture{name: name, doc: doc})
	}
	if len(out) == 0 {
		t.Fatal("no accept fixtures loaded; this test would pass vacuously")
	}
	return out
}
