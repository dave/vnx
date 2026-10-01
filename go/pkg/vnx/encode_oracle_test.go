package vnx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// The differential oracle.
//
// It freezes what Encode emits over far more inputs than the two committed
// goldens, so that a rewrite of the encoder has something to be differential
// AGAINST. Both goldens are `Selection{Everything: true}` exports of one library
// shape; between them they miss every case listed below.
//
// ⚠ IT COMPARES JSON TREES, NOT BYTES. Key order, number spelling and
// whitespace are the writer's own business (§8.1), so an expectation pins what
// the encoder SAYS rather than how it spells it. The two normalisations that
// are not about spelling — §3.1's absent-versus-empty, and channels this
// version does not name — are applied; rounding and the shortest spelling of a
// position are NOT, because those are rules the encoder must still obey and
// normalising them would delete the oracle's grip on them.
//
// ⚠ AN EXPECTATION FILE IS NEVER REGENERATED TO MAKE THIS GREEN. A difference
// here is the finding. There is deliberately no -update flag and no env gate: a
// regeneration path in the committed tree is a path someone takes at 2am. To
// add a case, add it to generatedOracleCases, write the expectation by hand or
// with a throwaway generator, READ it, and commit it.
//
// ⚠ A CORPUS THAT LACKS A CASE CANNOT FAIL ON IT. An earlier audit probe
// declared its type coverage complete over eight documents and missed `int`
// entirely, because none of those documents carried a point style with an icon
// size. TestOracleCorpusCoversTheCasesTheGoldensMiss asserts the coverage
// rather than trusting that someone remembered.
const oracleDir = "testdata/oracle"

func ip(v int) *int             { return &v }
func bp(v bool) *bool           { return &v }
func tp(t time.Time) *time.Time { return &t }
func negZero() float64          { return math.Copysign(0, -1) }
func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return t.UTC()
}

type oracleCase struct {
	name string
	doc  *Document
}

// TestEncodeMatchesItsFrozenOutput is the oracle itself.
func TestEncodeMatchesItsFrozenOutput(t *testing.T) {
	cases := oracleCases(t)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(oracleDir, c.name+".vnx")
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("no frozen expectation for this case: %v\n"+
					"Add one DELIBERATELY, having read the bytes — never by regenerating to go green.", err)
			}
			got, err := Encode(c.doc)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			diff, err := compareDocuments(got, want, treeOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if diff != "" {
				t.Fatalf("output changed: %s", diff)
			}
		})
	}
}

// An expectation file with no case behind it is a case that was deleted, or
// renamed, and took its coverage with it silently. The corpus cannot shrink
// without this going red.
func TestEveryOracleExpectationHasACase(t *testing.T) {
	cases := oracleCases(t)
	have := map[string]bool{}
	for _, c := range cases {
		have[c.name] = true
	}
	entries, err := os.ReadDir(oracleDir)
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".vnx")
		if name == e.Name() {
			t.Fatalf("%s is not a .vnx expectation", e.Name())
		}
		files++
		if !have[name] {
			t.Errorf("%s has no case behind it — a case was renamed or deleted", e.Name())
		}
	}
	if files != len(cases) {
		t.Errorf("%d expectation files for %d cases", files, len(cases))
	}
	if files == 0 {
		t.Fatal("no expectations found — dead probe, not a clean result")
	}
}

// The acceptance criterion of T1, asserted rather than asserted-in-prose.
//
// Every top-level collection must appear NON-EMPTY in at least one case AND
// EMPTY in at least one. The empty half is the one the goldens cannot do:
// documentTree guards every branch with `if len(...) > 0` (encode.go:46-121),
// so an empty collection OMITS ITS KEY, and both goldens carry all six
// collections populated. A rewrite that emitted `"areas": []` would pass both goldens.
//
// The collection list is derived by reflection over Document, so another
// collection added to the model and forgotten here fails instead of being
// quietly uncovered.
func TestOracleCorpusCoversEveryCollectionFullAndEmpty(t *testing.T) {
	cases := oracleCases(t)
	names := documentCollectionFields()
	if len(names) < 7 {
		t.Fatalf("reflection found %d slice fields on Document, expected at least the 6 collections "+
			"and the application keys: %v", len(names), names)
	}
	full := map[string]bool{}
	empty := map[string]bool{}
	for _, c := range cases {
		v := reflect.ValueOf(*c.doc)
		for _, n := range names {
			if v.FieldByName(n).Len() > 0 {
				full[n] = true
			} else {
				empty[n] = true
			}
		}
	}
	for _, n := range names {
		if n == "AppKeys" {
			oracleAppKeyArm(t, cases)
		}
		if !full[n] {
			t.Errorf("no case has a non-empty %s", n)
		}
		if !empty[n] {
			t.Errorf("no case has an empty %s — the key-omission rule is unguarded for it", n)
		}
	}
}

// oracleAppKeyArm is the application-key arm of the coverage guard above.
//
// ⚠ A NON-EMPTY AppKeys IN A CASE IS NOT ENOUGH. The reflection above counts
// slice lengths in the MODEL, and a writer that dropped application keys would
// leave every model full while every file lost them. So this arm fills one
// dotted key through the reflected field and asserts Encode wrote it. It also
// asserts that every key a case holds is in that case's frozen expectation, so
// the expectations cannot lose one without a red here.
func oracleAppKeyArm(t *testing.T, cases []oracleCase) {
	t.Helper()
	const key = "com.example.oracle-arm"
	doc := &Document{}
	reflect.ValueOf(doc).Elem().FieldByName("AppKeys").Set(
		reflect.ValueOf([]AppKey{{Key: key, Value: nil}}))
	out, err := Encode(doc)
	if err != nil {
		t.Fatalf("encoding a document holding one application key: %v", err)
	}
	var tree map[string]any
	if err := json.Unmarshal(out, &tree); err != nil {
		t.Fatal(err)
	}
	if v, ok := tree[key]; !ok || v != nil {
		t.Errorf("a document holding the application key %q = null encoded as %s", key, out)
	}

	checked := 0
	for _, c := range cases {
		if len(c.doc.AppKeys) == 0 {
			continue
		}
		want, err := os.ReadFile(filepath.Join(oracleDir, c.name+".vnx"))
		if err != nil {
			t.Fatal(err)
		}
		var frozen map[string]any
		if err := json.Unmarshal(want, &frozen); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, k := range c.doc.AppKeys {
			if _, ok := frozen[k.Key]; !ok {
				t.Errorf("%s holds application key %q and its frozen expectation does not", c.name, k.Key)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no oracle case holds an application key — this arm checked nothing")
	}
}

func documentCollectionFields() []string {
	var out []string
	dt := reflect.TypeOf(Document{})
	for i := 0; i < dt.NumField(); i++ {
		if dt.Field(i).Type.Kind() == reflect.Slice {
			out = append(out, dt.Field(i).Name)
		}
	}
	sort.Strings(out)
	return out
}

// The type-coverage acceptance. Each of these is known to be absent from at
// least one existing fixture, and the `int` one was missed outright by an
// earlier probe.
func TestOracleCorpusCoversTheCasesTheGoldensMiss(t *testing.T) {
	cases := oracleCases(t)

	var (
		timedPip        bool // Position.Time — *int64
		iconSize        bool // PointStyle.IconSize — *int
		importance      bool // PointStyle.Importance — *int
		bothIntStyle    bool // icon-size AND importance on one style
		labelVisible    bool // PointStyle.LabelVisible — *bool, tristate
		nilEle          bool
		nilTime         bool
		mixedTiming     bool // timed and untimed pips in ONE track
		signedZero      bool
		appKey          bool
		appKeyNull      bool
		appKeyEmpty     bool
		dataRecord      bool
		emptyArray      bool // an emitted [] somewhere
		singleElemArray bool
		deepFolders     bool
		unicodeName     bool
		rtlName         bool
		big             bool // a value at or beyond 2^53
		tiny            bool // 1e-7 / 0.000001 class
		huge            bool // 1e21 class
	)
	rtl := regexp.MustCompile(`[\x{0590}-\x{08FF}]`)
	nonASCII := regexp.MustCompile(`[^\x00-\x7F]`)

	for _, c := range cases {
		d := c.doc
		for _, k := range d.AppKeys {
			appKey = true
			if k.Value == nil {
				appKeyNull = true
			}
			if arr, ok := k.Value.([]any); ok && len(arr) == 0 {
				appKeyEmpty = true
			}
		}
		depth := map[string]int{}
		for _, fo := range d.Folders {
			if len(fo.Data) > 0 {
				dataRecord = true
			}
			if fo.Parent == "" {
				depth[fo.ID] = 1
			} else {
				depth[fo.ID] = depth[fo.Parent] + 1
			}
			if depth[fo.ID] >= 5 {
				deepFolders = true
			}
			checkName(fo.Name, nonASCII, rtl, &unicodeName, &rtlName)
		}
		eachPosition(d, func(p Position) {
			if p.Time != nil {
				timedPip = true
				if *p.Time >= 1<<53 {
					big = true
				}
			} else {
				nilTime = true
			}
			if p.Ele == nil {
				nilEle = true
			} else {
				e := math.Abs(*p.Ele)
				if e >= 1e21 {
					huge = true
				}
				if e > 0 && e <= 1e-6 {
					tiny = true
				}
				if e >= 1<<53 {
					big = true
				}
				if *p.Ele == 0 && math.Signbit(*p.Ele) {
					signedZero = true
				}
			}
			if p.Lon == 0 && math.Signbit(p.Lon) {
				signedZero = true
			}
			if p.Lat == 0 && math.Signbit(p.Lat) {
				signedZero = true
			}
		})
		eachPointStyle(d, func(s Style) {
			if _, ok := s["icon-size"]; ok {
				iconSize = true
			}
			_, hasSize := s["icon-size"]
			_, hasImportance := s["importance"]
			if hasImportance {
				importance = true
			}
			if hasSize && hasImportance {
				bothIntStyle = true
			}
			if _, ok := s["label-visible"]; ok {
				labelVisible = true
			}
		})
		eachData(d, func(r Record) { dataRecord = true })
		for _, tr := range d.Tracks {
			if len(tr.Segments) == 0 {
				emptyArray = true
			}
			if len(tr.Segments) == 1 {
				singleElemArray = true
			}
			for _, s := range tr.Segments {
				if len(s.Pips) == 0 {
					emptyArray = true
				}
				if len(s.Pips) == 1 {
					singleElemArray = true
				}
				var timed, untimed bool
				for _, p := range s.Pips {
					if p.Time != nil {
						timed = true
					} else {
						untimed = true
					}
				}
				if timed && untimed {
					mixedTiming = true
				}
			}
			var timed, untimed bool
			for _, s := range tr.Segments {
				for _, p := range s.Pips {
					if p.Time != nil {
						timed = true
					} else {
						untimed = true
					}
				}
			}
			if timed && untimed {
				mixedTiming = true
			}
			checkName(tr.Name, nonASCII, rtl, &unicodeName, &rtlName)
		}
		for _, r := range d.Routes {
			if len(r.Walk) == 0 {
				emptyArray = true
			}
			if len(r.Walk) == 1 {
				singleElemArray = true
			}
			checkName(r.Name, nonASCII, rtl, &unicodeName, &rtlName)
		}
		for _, w := range d.Waypoints {
			checkName(w.Name, nonASCII, rtl, &unicodeName, &rtlName)
		}
		for _, n := range d.Networks {
			for _, ch := range n.Highlights {
				if len(ch.Walk) == 0 {
					emptyArray = true
				}
				if len(ch.Walk) == 1 {
					singleElemArray = true
				}
			}
			checkName(n.Name, nonASCII, rtl, &unicodeName, &rtlName)
		}
	}

	for _, want := range []struct {
		got  bool
		what string
	}{
		{timedPip, "a timed pip (Position.Time, *int64)"},
		{iconSize, "a point style carrying icon-size (*int)"},
		{importance, "a point style carrying importance (*int)"},
		{bothIntStyle, "one point style carrying icon-size AND importance"},
		{labelVisible, "a point style carrying label-visible (*bool tristate)"},
		{nilEle, "a position with nil Ele"},
		{nilTime, "a position with nil Time"},
		{mixedTiming, "timed and untimed pips in one track"},
		{signedZero, "a negative-zero coordinate or elevation"},
		{appKey, "an application key (neither golden has one)"},
		{appKeyNull, "an application key whose value is null"},
		{appKeyEmpty, "an application key whose value is an empty array"},
		{dataRecord, "an item carrying a data record"},
		{emptyArray, "an emitted empty array"},
		{singleElemArray, "a single-element array"},
		{deepFolders, "folders nested at least 5 deep"},
		{unicodeName, "a non-ASCII name"},
		{rtlName, "an RTL name"},
		{big, "a number at or beyond 2^53"},
		{tiny, "a number in the 1e-7 / 0.000001 class"},
		{huge, "a number in the 1e21 class"},
	} {
		if !want.got {
			t.Errorf("the corpus has no case with %s — it cannot fail on it", want.what)
		}
	}
}

func checkName(s string, nonASCII, rtl *regexp.Regexp, unicode, isRTL *bool) {
	if nonASCII.MatchString(s) {
		*unicode = true
	}
	if rtl.MatchString(s) {
		*isRTL = true
	}
}

// The negative-zero guard, with its own liveness assertion.
//
// -0 was found by writing a corpus case and not by reading the encoder, so it
// may be in neither golden. Two halves:
// Go WOULD emit `-0` here (the premise — checked, so the guard cannot pass
// because the hazard evaporated), and the frozen bytes carry `0`.
func TestNegativeZeroCaseIsFrozenAsPlainZero(t *testing.T) {
	if out, err := json.Marshal(negZero()); err != nil || string(out) != "-0" {
		t.Fatalf("premise gone: json.Marshal(-0.0) = %q (err %v), want %q — "+
			"this guard exists because Go emits -0 where ECMAScript emits 0", out, err, "-0")
	}
	want, err := os.ReadFile(filepath.Join(oracleDir, "negative-zero.vnx"))
	if err != nil {
		t.Fatal(err)
	}
	// A bare -0 token: `-0` not followed by a digit or a decimal point, so
	// -0.5 and -0125 do not trip it.
	bare := regexp.MustCompile(`-0(?:[^.0-9]|$)`)
	if loc := bare.FindIndex(want); loc != nil {
		t.Fatalf("the frozen bytes carry a bare -0 at byte %d: %s", loc[0], excerpt(want, loc[0]))
	}
	if !bytes.Contains(want, []byte("[0, 0")) {
		t.Fatal("the frozen bytes do not carry a rendered signed zero at all; this case would pass vacuously")
	}
}

// ---------------------------------------------------------------------------
// The corpus.
// ---------------------------------------------------------------------------

func oracleCases(t *testing.T) []oracleCase {
	t.Helper()
	cases := generatedOracleCases()
	cases = append(cases, corpusOracleCases(t)...)
	seen := map[string]bool{}
	for _, c := range cases {
		if seen[c.name] {
			t.Fatalf("two oracle cases are named %q; one would overwrite the other's expectation", c.name)
		}
		seen[c.name] = true
	}
	return cases
}

// corpusOracleCases drives the oracle from the spec-4 conformance corpus rather
// than duplicating it: loadCorpus already carries the anti-shrink guards, and a
// corpus file edited without thought reddens here too.
//
// Reject cases are excluded — there is no document to encode. Everything that
// decodes, does.
func corpusOracleCases(t *testing.T) []oracleCase {
	t.Helper()
	var out []oracleCase
	for _, c := range loadCorpus(t) {
		if c.expect.Kind == "reject" {
			continue
		}
		doc, _, _, err := c.decode()
		if err != nil {
			t.Fatalf("%s is a %s case but did not decode: %v", c.name, c.expect.Kind, err)
		}
		// c.name is "accept/example-network.vnx"; flatten it to a filesystem
		// name that keeps the archive/document distinction visible.
		flat := strings.ReplaceAll(strings.ReplaceAll(c.name, "/", "-"), ".", "-")
		out = append(out, oracleCase{name: "corpus-" + flat, doc: doc})
	}
	if len(out) == 0 {
		t.Fatal("the conformance corpus contributed no cases — it is not being loaded")
	}
	return out
}

func generatedOracleCases() []oracleCase {
	return []oracleCase{
		{"empty-document", &Document{}},
		{"scalars-only", &Document{
			Version:   "0.8.0",
			Generator: "viatrail/oracle",
			Created:   "2026-09-02T10:00:00Z",
		}},
		{"every-collection-empty-slice", &Document{
			Generator: "empty-slices",
			Folders:   []Folder{},
			Waypoints: []Waypoint{},
			Areas:     []Area{},
			Tracks:    []Track{},
			Routes:    []Route{},
			Networks:  []Network{},
			AppKeys:   []AppKey{},
		}},
		{"folders-deeply-nested", oracleFolders()},
		{"waypoint-styles", oracleWaypointStyles()},
		{"unicode-and-escapes", oracleUnicode()},
		{"negative-zero", oracleNegativeZero()},
		{"extreme-numbers", oracleExtremeNumbers()},
		{"int64-beyond-2-53", oracleBigInt64()},
		{"timed-and-untimed-pips", oracleTiming()},
		{"single-element-and-empty-arrays", oracleDegenerateArrays()},
		{"application-keys", oracleAppKeys()},
		{"data-records-everywhere", oracleData()},
		{"network-full", &Document{Generator: "network-full", Networks: []Network{oracleNetwork()}}},
		{"network-minimal", &Document{Generator: "network-minimal", Networks: []Network{{Name: "bare"}}}},
		{"areas", oracleAreas()},
		{"track-timestamps", oracleTrackTimestamps()},
		{"every-collection-populated", oracleEverything()},
	}
}

func oracleFolders() *Document {
	d := &Document{Generator: "folders"}
	// Six levels: the deepest indentation any folder chain produces, and the
	// case a flat two-level fixture cannot reach.
	parent := ""
	for i := 1; i <= 6; i++ {
		id := fmt.Sprintf("f%d", i)
		d.Folders = append(d.Folders, Folder{
			ID:     id,
			Name:   fmt.Sprintf("level %d", i),
			Parent: parent,
		})
		parent = id
	}
	d.Folders = append(d.Folders,
		Folder{ID: "hidden", Name: "hidden one", Parent: "f6", Hidden: true},
		Folder{ID: "noted", Name: "Grüße 🏔️", Notes: "notes on a folder", Parent: "f1"},
		Folder{ID: "carrier", Name: "مسار الجبل", Parent: "", Data: []Record{
			{Type: "app.viatrail.test/colour", Body: map[string]any{"value": "#ff8800"}},
		}},
		// Nothing but an id: every optional key omitted.
		Folder{ID: "bare"},
	)
	return d
}

func oracleWaypointStyles() *Document {
	at := Position{Lon: -3.6712, Lat: 57.1180}
	return &Document{
		Generator: "waypoint-styles",
		Waypoints: []Waypoint{
			// ⚠ icon-size AND importance together, both *int (item.go:17,21),
			// written natively into the tree at encode.go:404,407. This is the
			// case the earlier type-coverage probe missed entirely.
			{Name: "every style field", At: at, Style: Style{"color": "#ff0000", "icon": "summit", "icon-size": 35, "pin-shape": "teardrop", "label-position": "above", "label-visible": true, "importance": 5}},
			{Name: "label hidden", At: at, Style: Style{"label-visible": false}},
			{Name: "icon size only", At: at, Style: Style{"icon-size": 16}},
			{Name: "importance only", At: at, Style: Style{"importance": 1}},
			// Absent LabelVisible is Auto, and Auto is spelled by the key being
			// gone (item.go:9-13). A rewrite that wrote false here would flatten
			// Auto to always-hidden.
			{Name: "no style at all", At: at},
			{Name: "hidden waypoint", At: at, Hidden: true, Folder: "somewhere", Notes: "note"},
		},
	}
}

func oracleUnicode() *Document {
	at := Position{Lon: 1.5, Lat: 2.5}
	return &Document{
		Generator: "unicode",
		Waypoints: []Waypoint{
			{Name: "Grüße aus München 🏔️", At: at},
			{Name: "日本語のトレイル", At: at},
			{Name: "مسار الجبل الأحمر", Notes: "עברית", At: at},
			// json.Marshal HTML-escapes these; freezing that is the point.
			{Name: `<script>alert("x") & 'y'</script>`, At: at},
			{Name: "quote \" backslash \\ slash / tab \t newline \n", At: at},
			{Name: "control  and ", At: at},
			{Name: "combining é and ligature ﬁ", At: at},
			{Name: "  line separator  ", At: at},
		},
	}
}

func oracleNegativeZero() *Document {
	nz := negZero()
	return &Document{
		Generator: "negative-zero",
		Waypoints: []Waypoint{
			// Null Island, spelled with the sign bit set on both channels.
			{Name: "signed zero position", At: Position{Lon: nz, Lat: nz}},
			{Name: "signed zero elevation", At: Position{Lon: nz, Lat: 1, Ele: &nz}},
			{Name: "signed zero with time", At: Position{Lon: 1, Lat: nz, Ele: &nz, Time: i(0)}},
		},
		Areas: []Area{{
			Name: "ring through the origin",
			Polygon: []Position{
				{Lon: nz, Lat: nz},
				{Lon: 1, Lat: nz},
				{Lon: 1, Lat: 1},
			},
			Style: Style{"fill-opacity": nz, "stroke-width": nz},
		}},
		// Verbatim blocks recurse through the same writer, so the rule must
		// hold inside them too.
		AppKeys: []AppKey{{Key: "com.example.signed-zero",
			Value: []any{map[string]any{"threshold": nz, "range": []any{nz, 0.0}}}}},
	}
}

func oracleExtremeNumbers() *Document {
	// The four spellings the reference implementations disagree about, plus the
	// 2^53 boundary. Python is the outlier on these.
	vals := []float64{
		1e-7,
		0.000001,
		1e21,
		340.0,
		9007199254740991, // 2^53 - 1, exactly representable
		-1e-7,
		5e-324, // smallest subnormal
		1.7976931348623157e308,
		0.1,
		1.0 / 3.0,
	}
	d := &Document{Generator: "extreme-numbers"}
	for n, v := range vals {
		ele := v
		d.Waypoints = append(d.Waypoints, Waypoint{
			Name: fmt.Sprintf("value %d", n),
			At:   Position{Lon: 0.1, Lat: 0.2, Ele: &ele},
		})
	}
	op := 1e-7
	w := 1e21
	d.Routes = []Route{{
		Name:  "extreme style",
		Style: Style{"opacity": op, "width": w},
		Nodes: map[string]Vertex{"node-1": {Lon: 1, Lat: 2}, "node-2": {Lon: 3, Lat: 4}},
		Arcs:  map[string]Arc{"arc-1": {From: "node-1", To: "node-2", Pips: []Vertex{}}},
		Walk:  []Pass{{Arc: "arc-1", From: &Endpoint{Node: "node-1"}, To: &Endpoint{Node: "node-2"}}},
	}}
	return d
}

// A pip time above 2^53 is NOT representable as a float64, and today's encoder
// takes every number through Float64(). Whatever it does with
// this is frozen here rather than assumed — position.go:20-25 warns that the
// internal MinInt64 sentinel must never reach the wire for exactly this reason.
func oracleBigInt64() *Document {
	return &Document{
		Generator: "big-int64",
		Tracks: []Track{{
			Name: "times around 2^53",
			Segments: []Segment{{Pips: []Position{
				{Lon: 1, Lat: 2, Time: i(9007199254740991)}, // 2^53 - 1
				{Lon: 1, Lat: 2, Time: i(9007199254740992)}, // 2^53
				{Lon: 1, Lat: 2, Time: i(9007199254740993)}, // 2^53 + 1
				{Lon: 1, Lat: 2, Time: i(-9007199254740993)},
				{Lon: 1, Lat: 2, Time: i(1778569391000)}, // an ordinary stamp
			}}},
		}},
	}
}

func oracleTiming() *Document {
	e := 340.5
	return &Document{
		Generator: "timing",
		Tracks: []Track{{
			Name:    "mixed timing",
			Started: TimestampOf(utc("2026-05-11T09:00:00Z")),
			Ended:   TimestampOf(utc("2026-05-11T17:30:00.25Z")),
			Segments: []Segment{
				// Fully timed and elevated: [lon, lat, ele, t].
				{Pips: []Position{
					{Lon: -3.1, Lat: 57.1, Ele: &e, Time: i(1778569391000)},
					{Lon: -3.2, Lat: 57.2, Ele: &e, Time: i(1778569401000)},
				}},
				// No time at all: [lon, lat, ele].
				{Pips: []Position{
					{Lon: -3.3, Lat: 57.3, Ele: &e},
					{Lon: -3.4, Lat: 57.4},
				}},
				// Interior absence: elevation gone, time present, so the null
				// CANNOT be trimmed — [lon, lat, null, t].
				{Pips: []Position{
					{Lon: -3.5, Lat: 57.5, Time: i(1778569411000)},
					{Lon: -3.6, Lat: 57.6, Ele: &e, Time: i(1778569421000)},
					{Lon: -3.7, Lat: 57.7},
					{Lon: -3.8, Lat: 57.8, Ele: &e},
				}},
			},
		}},
	}
}

func oracleDegenerateArrays() *Document {
	return &Document{
		Generator: "degenerate-arrays",
		Tracks: []Track{
			{Name: "no segments", Segments: []Segment{}},
			{Name: "one empty segment", Segments: []Segment{{}}},
			{Name: "one segment one pip", Segments: []Segment{{Pips: []Position{{Lon: 1, Lat: 2}}}}},
		},
		Routes: []Route{
			{Name: "no arcs", Walk: []Pass{}},
			{Name: "one arc one pip", Nodes: map[string]Vertex{"a": {Lon: 1, Lat: 2}, "b": {Lon: 5, Lat: 6}},
				Arcs: map[string]Arc{"arc": {From: "a", To: "b", Pips: []Vertex{{Lon: 3, Lat: 4}}}},
				Walk: []Pass{{Arc: "arc", From: &Endpoint{Node: "a"}, To: &Endpoint{Node: "b"}}}},
			{Name: "one arc no pips", Nodes: map[string]Vertex{"a": {Lon: 1, Lat: 2}, "b": {Lon: 5, Lat: 6}},
				Arcs: map[string]Arc{"arc": {From: "a", To: "b"}},
				Walk: []Pass{{Arc: "arc", From: &Endpoint{Node: "b"}, To: &Endpoint{Node: "a"}}}},
		},
		Networks: []Network{{
			Name:       "highlights without passes",
			Nodes:      map[string]Vertex{"node-1": {Lon: 1, Lat: 2}},
			Highlights: []Highlight{{Name: "empty highlight", Walk: []Pass{}}},
		}},
	}
}

func oracleAppKeys() *Document {
	// Carried VERBATIM and uninspected (SPEC §3.9), but the encoder still
	// recurses into each value — so every writer rule applies inside a value
	// this package refuses to understand. NEITHER GOLDEN HAS ONE.
	//
	// ⚠ THE null AND THE EMPTY VALUES ARE THE POINT. §3.9 exempts an
	// application key from absent-equals-empty, so a writer that skipped a key
	// holding null, [] or {} would drop content; each must come back.
	return &Document{
		Generator: "application-keys",
		AppKeys: []AppKey{{Key: "app.viatrail.rulesets", Value: []any{
			map[string]any{
				"id":   "r1",
				"name": "شرط",
				// Scalars sort before containers, both lexicographically, and
				// the classification is by the value in THIS object.
				"when":  []any{map[string]any{"field": "surface", "operator": "is", "value": "paved"}},
				"then":  map[string]any{"color": "#123456", "width": 2.5},
				"empty": map[string]any{},
				"none":  nil,
				"flag":  true,
				"list":  []any{},
				"nums":  []any{1.0, 2.5, nil, -3.0},
			},
			map[string]any{"id": "r2"},
			"a bare string in the block",
			42.0,
			nil,
			true,
			[]any{},
		}},
			{Key: "com.example.empty-array", Value: []any{}},
			{Key: "com.example.empty-object", Value: map[string]any{}},
			{Key: "com.example.null", Value: nil},
			{Key: "com.example.number", Value: 42.0},
			{Key: "com.example.string", Value: "a bare string"},
		},
	}
}

func oracleData() *Document {
	rec := func(t string, body map[string]any) Record { return Record{Type: t, Body: body} }
	pips := []Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 4}}
	nested := rec("app.viatrail.test/nested", map[string]any{
		"value":  map[string]any{"deep": []any{1.0, map[string]any{"deeper": nil}}},
		"@extra": "a sigil that is not @type",
	})
	return &Document{
		Generator: "data-records",
		Folders:   []Folder{{ID: "f", Name: "carrier", Data: []Record{rec("t/folder", map[string]any{"value": 1.0})}}},
		Waypoints: []Waypoint{{Name: "w", At: Position{Lon: 1, Lat: 2}, Data: []Record{
			rec("t/waypoint", map[string]any{"value": "x"}),
			// A record with NO body beyond its @type.
			rec("t/bodyless", nil),
			nested,
		}}},
		Areas: []Area{{Name: "a", Polygon: []Position{{Lon: 0, Lat: 0}, {Lon: 1, Lat: 0}, {Lon: 1, Lat: 1}},
			Data: []Record{rec("t/area", map[string]any{"value": true})}}},
		Tracks: []Track{{Name: "t", Segments: []Segment{{Pips: pips}},
			Data: []Record{rec("t/track", map[string]any{"value": nil})}}},
		Routes: []Route{{Name: "r", Nodes: map[string]Vertex{"node-1": {Lon: 1, Lat: 2}, "node-2": {Lon: 3, Lat: 4}},
			Arcs: map[string]Arc{"arc-1": {From: "node-1", To: "node-2", Data: []Record{rec("t/arc", map[string]any{"value": 2.0})}}},
			Walk: []Pass{{Arc: "arc-1", From: &Endpoint{Node: "node-1"}, To: &Endpoint{Node: "node-2"}}},
			Data: []Record{rec("t/route", map[string]any{"value": []any{1.0, 2.0}})}}},
		Networks: []Network{{
			Name:  "n",
			Nodes: map[string]Vertex{"node-1": {Lon: 1, Lat: 2}, "node-2": {Lon: 3, Lat: 4}},
			Arcs: map[string]Arc{"arc-1": {From: "node-1", To: "node-2",
				Data: []Record{rec("t/network-arc", map[string]any{"value": "arc"})}}},
			Highlights: []Highlight{{Name: "c", Walk: []Pass{{Arc: "arc-1", From: &Endpoint{Node: "node-1"}, To: &Endpoint{Node: "node-2"}}},
				Data: []Record{rec("t/highlight", map[string]any{"value": "highlight"})}}},
			Data: []Record{rec("t/network", map[string]any{"value": "net"})},
		}},
	}
}

// oracleNetwork is the shape T2's determinism test also needs: at least 8 nodes
// and 8 arcs, so map iteration has room to differ. A 1-entry map is trivially
// ordered.
func oracleNetwork() Network {
	n := Network{
		Name:   "eight by eight",
		Notes:  "enough map entries that iteration order can differ",
		Folder: "nets",
		Nodes:  map[string]Vertex{},
		Arcs:   map[string]Arc{},
	}
	for k := 1; k <= 10; k++ {
		lon := -3.0 - float64(k)/100
		lat := 57.0 + float64(k)/100
		ele := 300 + float64(k)
		n.Nodes[fmt.Sprintf("node-%d", k)] = Vertex{Lon: lon, Lat: lat, Ele: &ele}
	}
	for k := 1; k <= 10; k++ {
		from := fmt.Sprintf("node-%d", k)
		to := fmt.Sprintf("node-%d", (k%10)+1)
		n.Arcs[fmt.Sprintf("arc-%d", k)] = Arc{
			From: from,
			To:   to,
			Pips: []Vertex{
				{Lon: -3.5 - float64(k)/100, Lat: 57.5 + float64(k)/100},
			},
		}
	}
	mid := LonLat{Lon: -3.55, Lat: 57.55}
	n.Highlights = []Highlight{
		{
			Name:  "node to node",
			Style: Style{"color": "#00aa00", "line-style": "dashed", "opacity": float64(0.5), "width": float64(3)},
			Walk: []Pass{
				{Arc: "arc-1", From: &Endpoint{Node: "node-1"}},
				{Arc: "arc-2", Pinned: true, To: &Endpoint{Node: "node-3"}},
			},
		},
		{
			// A turn is a position on the arc where the walk goes back the
			// way it came: out from node-5 and back to it.
			Name:   "turnaround on arc-5",
			Hidden: true,
			Walk: []Pass{
				{Arc: "arc-5", From: &Endpoint{Node: "node-5"}, Turn: &mid, To: &Endpoint{Node: "node-5"}},
			},
		},
		{
			// A walk starting and ending part-way along arcs.
			Name: "mid-arc start and end",
			Walk: []Pass{
				{Arc: "arc-7", From: &Endpoint{At: &LonLat{Lon: -3.575, Lat: 57.575}}, To: &Endpoint{Node: "node-8"}},
				{Arc: "arc-8", To: &Endpoint{At: &LonLat{Lon: -3.585, Lat: 57.585}}},
			},
		},
	}
	n.Points = []Waypoint{
		{Name: "in-network point", At: Position{Lon: -3.5, Lat: 57.5}, Style: Style{"icon-size": 21, "importance": 3}},
	}
	n.Areas = []Area{
		{Name: "in-network area", Polygon: []Position{{Lon: -3.6, Lat: 57.6}, {Lon: -3.5, Lat: 57.6}, {Lon: -3.5, Lat: 57.5}},
			Style: Style{"fill-color": "#abcdef", "fill-opacity": float64(0.25), "stroke-color": "#000000", "stroke-width": float64(1.5)}},
	}
	n.Data = []Record{{Type: "app.viatrail.test/net", Body: map[string]any{"value": "n"}}}
	return n
}

func oracleAreas() *Document {
	ring := []Position{{Lon: 0.1, Lat: 0.2}, {Lon: 0.3, Lat: 0.2}, {Lon: 0.3, Lat: 0.4}}
	ele := 12.5
	timed := []Position{
		{Lon: 0.1, Lat: 0.2, Ele: &ele, Time: i(1778569391000)},
		{Lon: 0.3, Lat: 0.2, Ele: &ele, Time: i(1778569392000)},
		{Lon: 0.3, Lat: 0.4, Ele: &ele, Time: i(1778569393000)},
	}
	return &Document{
		Generator: "areas",
		Areas: []Area{
			{Name: "full style", Polygon: ring, Style: Style{"fill-color": "#112233", "fill-opacity": float64(0.4), "stroke-color": "#445566", "stroke-width": float64(2)}},
			{Name: "no style", Polygon: ring},
			{Name: "fill only", Polygon: ring, Style: Style{"fill-color": "#778899"}},
			{Name: "opacity only", Polygon: ring, Style: Style{"fill-opacity": float64(1)}},
			// An area ring whose positions carry elevation and time: the
			// position writer does not care which collection it is in.
			{Name: "timed ring", Notes: "ملاحظات", Hidden: true, Folder: "f", Polygon: timed},
		},
	}
}

func oracleTrackTimestamps() *Document {
	pips := []Position{{Lon: 1, Lat: 2}, {Lon: 3, Lat: 4}}
	return &Document{
		Generator: "track-timestamps",
		Tracks: []Track{
			// Whole seconds: no fractional part emitted (encode.go:203-211).
			{Name: "whole seconds", Started: TimestampOf(utc("2026-01-02T03:04:05Z")), Ended: TimestampOf(utc("2026-01-02T04:05:06Z")),
				Segments: []Segment{{Pips: pips}}},
			// Milliseconds, which a seconds-only layout silently truncated once.
			{Name: "milliseconds", Started: TimestampOf(utc("2026-01-02T03:04:05.123Z")), Ended: TimestampOf(utc("2026-01-02T03:04:05.100Z")),
				Segments: []Segment{{Pips: pips}}},
			{Name: "nanoseconds", Started: TimestampOf(utc("2026-01-02T03:04:05.123456789Z")),
				Segments: []Segment{{Pips: pips}}},
			// Not UTC on the way in; it must be UTC on the way out.
			{Name: "offset input", Ended: TimestampOf(utc("2026-01-02T03:04:05+01:00")),
				Segments: []Segment{{Pips: pips}}},
			{Name: "neither stamp", Segments: []Segment{{Pips: pips}}},
		},
	}
}

// The one case where every top-level collection is non-empty at once.
func oracleEverything() *Document {
	d := &Document{
		Version:   "0.8.0",
		Generator: "viatrail/oracle everything",
		Created:   "2026-09-02T12:00:00Z",
	}
	d.Folders = oracleFolders().Folders
	d.Waypoints = oracleWaypointStyles().Waypoints
	d.Areas = oracleAreas().Areas
	d.Tracks = oracleTiming().Tracks
	d.Routes = oracleExtremeNumbers().Routes
	d.Networks = []Network{oracleNetwork()}
	d.AppKeys = oracleAppKeys().AppKeys
	return d
}

// ---------------------------------------------------------------------------
// Walkers, used only by the coverage assertions.
// ---------------------------------------------------------------------------

func eachPosition(d *Document, fn func(Position)) {
	for _, w := range d.Waypoints {
		fn(w.At)
	}
	for _, a := range d.Areas {
		for _, p := range a.Polygon {
			fn(p)
		}
	}
	for _, t := range d.Tracks {
		for _, s := range t.Segments {
			for _, p := range s.Pips {
				fn(p)
			}
		}
	}
	graph := func(nodes map[string]Vertex, arcs map[string]Arc, walks ...[]Pass) {
		for _, v := range nodes {
			fn(v.Position())
		}
		for _, a := range arcs {
			for _, v := range a.Pips {
				fn(v.Position())
			}
		}
		for _, w := range walks {
			for _, p := range w {
				for _, e := range []*Endpoint{p.From, p.To} {
					if e != nil && e.At != nil {
						fn(e.At.Position())
					}
				}
				if p.Turn != nil {
					fn(p.Turn.Position())
				}
			}
		}
	}
	for _, r := range d.Routes {
		graph(r.Nodes, r.Arcs, r.Walk)
	}
	for _, n := range d.Networks {
		walks := make([][]Pass, 0, len(n.Highlights))
		for _, c := range n.Highlights {
			walks = append(walks, c.Walk)
		}
		graph(n.Nodes, n.Arcs, walks...)
		for _, w := range n.Points {
			fn(w.At)
		}
		for _, a := range n.Areas {
			for _, p := range a.Polygon {
				fn(p)
			}
		}
	}
}

func eachPointStyle(d *Document, fn func(Style)) {
	for _, w := range d.Waypoints {
		fn(w.Style)
	}
	for _, n := range d.Networks {
		for _, w := range n.Points {
			fn(w.Style)
		}
	}
}

func eachData(d *Document, fn func(Record)) {
	emit := func(rs []Record) {
		for _, r := range rs {
			fn(r)
		}
	}
	for _, f := range d.Folders {
		emit(f.Data)
	}
	for _, w := range d.Waypoints {
		emit(w.Data)
	}
	for _, a := range d.Areas {
		emit(a.Data)
	}
	for _, t := range d.Tracks {
		emit(t.Data)
	}
	for _, r := range d.Routes {
		emit(r.Data)
		for _, a := range r.Arcs {
			emit(a.Data)
		}
	}
	for _, n := range d.Networks {
		emit(n.Data)
		for _, a := range n.Arcs {
			emit(a.Data)
		}
		for _, c := range n.Highlights {
			emit(c.Data)
		}
		for _, w := range n.Points {
			emit(w.Data)
		}
		for _, a := range n.Areas {
			emit(a.Data)
		}
	}
}
