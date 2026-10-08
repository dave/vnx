package vnx

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// SPEC §3.9: a top-level key containing a full stop is an application key. Its
// value is any JSON value, kept verbatim, and never reported. The accept tier
// round-trips a corpus file holding several, so without these that round trip
// rests on behaviour nothing tested directly.
func TestAppKeysArePreservedOpaquelyAndSorted(t *testing.T) {
	const src = `{"vnx":"0.10.0",
	  "org.example.z": {"anything":{"at":["all"]}},
	  "app.viatrail.rulesets":[{"name":"Hide minor","rules":[{"op":"equals"}]}],
	  "com.example.null": null,
	  "com.example.empty": [],
	  "com.example.string": "text"}`
	doc, diags, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("an application key must not be reported or picked over: %v", diags)
	}
	var tree map[string]any
	if err := json.Unmarshal([]byte(src), &tree); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"app.viatrail.rulesets", "com.example.empty", "com.example.null",
		"com.example.string", "org.example.z"}
	var gotKeys []string
	for _, k := range doc.AppKeys {
		gotKeys = append(gotKeys, k.Key)
		if !reflect.DeepEqual(k.Value, tree[k.Key]) {
			t.Errorf("%s not preserved verbatim:\n got %#v\nwant %#v", k.Key, k.Value, tree[k.Key])
		}
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("application keys %v, want %v, sorted by key", gotKeys, wantKeys)
	}
}

// The rule is TOP-LEVEL ONLY. On any other object a dotted key is an
// unrecognised key and is reported; an undotted unknown key at the top level
// is still unknown too.
func TestADottedKeyBelowTheTopLevelIsReported(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.10.0","rulesets":[],
	  "waypoints":[{"at":[1,2],"app.viatrail.x":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, d := range diags {
		got[d.Path] = d.Code
	}
	want := map[string]string{"/rulesets": CodeUnknownKey, "/waypoints/0/app.viatrail.x": CodeUnknownKey}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostics %v, want %v", got, want)
	}
}

// An application key survives a read-modify-write untouched — a null stays
// null and an empty value stays empty, because §3.9 exempts it from §8.1's
// absent-equals-empty normalisation.
func TestAppKeysSurviveAReadModifyWrite(t *testing.T) {
	const src = `{"vnx":"0.10.0","app.viatrail.rulesets":[{"zebra":1,"apple":[2,{"nested":true}]}],
	  "com.example.null":null,"com.example.empty-array":[],"com.example.empty-object":{},"com.example.n":0}`
	doc, _, err := Decode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	diff, err := compareDocuments(got, []byte(src), treeOptions{emitBoundary: true})
	if err != nil {
		t.Fatal(err)
	}
	if diff != "" {
		t.Fatalf("the application keys did not survive: %s", diff)
	}
}

// The comparison must not normalise an application key's empty value away,
// or the round trip above could not fail on a writer that dropped one.
func TestTheTreeComparisonSeesADroppedEmptyAppKey(t *testing.T) {
	diff, err := compareDocuments([]byte(`{"vnx":"0.10.0"}`),
		[]byte(`{"vnx":"0.10.0","com.example.empty":[]}`), treeOptions{emitBoundary: true})
	if err != nil {
		t.Fatal(err)
	}
	if diff == "" {
		t.Fatal("a dropped application key holding [] compared equal; §3.9 forbids that normalisation")
	}
}

// A key with no full stop is not an application key, and a reader would take
// it for an unrecognised key and drop it. The encoder refuses it rather than
// writing a document that loses it; it refuses a repeated key for the same
// reason.
func TestEncodeRefusesAnAppKeyItCannotWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []AppKey
		want string
	}{
		{"no full stop", []AppKey{{Key: "rulesets", Value: []any{}}}, "no full stop"},
		{"a format key", []AppKey{{Key: "folders", Value: []any{}}}, "no full stop"},
		{"empty", []AppKey{{Key: "", Value: nil}}, "no full stop"},
		{"twice", []AppKey{{Key: "a.b", Value: 1.0}, {Key: "a.b", Value: 2.0}}, "twice"},
		{"typed nil slice", []AppKey{{Key: "a.b", Value: []any(nil)}}, "nil"},
		{"typed nil map", []AppKey{{Key: "a.b", Value: map[string]any(nil)}}, "nil"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A populated collection, so a refusal after the first member
			// would leave bytes behind.
			doc := &Document{Generator: "g", Waypoints: []Waypoint{{Name: "w", At: Position{Lon: 1, Lat: 2}}},
				AppKeys: tc.keys}
			var b bytes.Buffer
			err := EncodeTo(&b, doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("EncodeTo = %v, want an error containing %q", err, tc.want)
			}
			// Refused before the first byte, not part-way through the object.
			if b.Len() != 0 {
				t.Fatalf("a refused document wrote %d bytes: %q", b.Len(), b.String())
			}
		})
	}
}

// A nil Value is JSON null, and is written; only a TYPED nil is refused.
func TestANilAppKeyValueIsWrittenAsNull(t *testing.T) {
	out, err := Encode(&Document{AppKeys: []AppKey{{Key: "a.b", Value: nil}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"a.b":null`) {
		t.Fatalf("a nil Value was not written as null: %s", out)
	}
}

// An unknown key's diagnostic path is a JSON Pointer, so `/` and `~` in the
// key are escaped (RFC 6901). Unescaped, "a/b" would point at "b" inside "a".
func TestAnUnknownKeyPathIsEscaped(t *testing.T) {
	_, diags, err := Decode([]byte(`{"vnx":"0.10.0","a/b":1,"x~y.z":1,
	  "waypoints":[{"at":[1,2],"a/b":1,"x~y.z":1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, d := range diags {
		got[d.Path] = true
	}
	// "x~y.z" at the top level is an application key, so it is not reported.
	want := map[string]bool{"/a~1b": true, "/waypoints/0/a~1b": true, "/waypoints/0/x~0y.z": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostic paths %v, want %v", got, want)
	}
}

func appKeyNames(d *Document) []string {
	var out []string
	for _, k := range d.AppKeys {
		out = append(out, k.Key)
	}
	return out
}

func TestAppKeyHelpers(t *testing.T) {
	d := &Document{}
	if _, ok := d.GetAppKey("a.b"); ok {
		t.Fatal("an empty document has a.b")
	}
	for _, k := range []string{"m.m", "a.a", "z.z", "c.c"} {
		if err := d.SetAppKey(k, k); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := appKeyNames(d), []string{"a.a", "c.c", "m.m", "z.z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after inserts: %v, want %v, sorted", got, want)
	}
	// Replace in place: no second entry, order unchanged.
	if err := d.SetAppKey("c.c", nil); err != nil {
		t.Fatal(err)
	}
	if got, want := appKeyNames(d), []string{"a.a", "c.c", "m.m", "z.z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after replace: %v, want %v", got, want)
	}
	if v, ok := d.GetAppKey("c.c"); !ok || v != nil {
		t.Fatalf("GetAppKey(c.c) = %#v, %v; want nil, true — a present null", v, ok)
	}
	if v, ok := d.GetAppKey("m.m"); !ok || v != "m.m" {
		t.Fatalf("GetAppKey(m.m) = %#v, %v", v, ok)
	}
	if !d.DeleteAppKey("m.m") || d.DeleteAppKey("m.m") {
		t.Fatal("DeleteAppKey reports presence wrongly")
	}
	if got, want := appKeyNames(d), []string{"a.a", "c.c", "z.z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after delete: %v, want %v", got, want)
	}
	for _, bad := range []struct {
		key   string
		value any
	}{{"nodot", 1.0}, {"a.b", []any(nil)}, {"a.b", map[string]any(nil)}} {
		if err := d.SetAppKey(bad.key, bad.value); err == nil {
			t.Errorf("SetAppKey(%q, %#v) accepted what EncodeTo refuses", bad.key, bad.value)
		}
	}
	if got, want := appKeyNames(d), []string{"a.a", "c.c", "z.z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("a refused SetAppKey changed the list: %v", got)
	}
}

// A present application key is content, whatever its value: a document holding
// only `"com.example.x": null` is not empty.
func TestDocumentIsEmptyCountsAPresentAppKey(t *testing.T) {
	if !DocumentIsEmpty(&Document{}) {
		t.Fatal("premise: an empty document is empty")
	}
	if DocumentIsEmpty(&Document{AppKeys: []AppKey{{Key: "com.example.x", Value: nil}}}) {
		t.Fatal("a document holding one application key, value null, reported as empty")
	}
}
