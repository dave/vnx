package vnx

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

// TestFolderPathAtTheRootIsTheBareName pins §2.7's easiest half, which is also
// the half a wrong implementation gets wrong in the most visible way: a leading
// " / " on every root item in the library.
func TestFolderPathAtTheRootIsTheBareName(t *testing.T) {
	d := &Document{Folders: []Folder{{ID: "f1", Name: "Foo"}}}
	path, ok := FolderPath(d, "")
	if !ok || path != "" {
		t.Fatalf("FolderPath(root) = %q, %v; want \"\", true — an item at the root keeps its bare "+
			"name with NO prefix, and the root is a found path, not a failure", path, ok)
	}
	name, ok := FlatName(d, "", "Ben Nevis")
	if !ok || name != "Ben Nevis" {
		t.Fatalf("FlatName at the root = %q, %v; want %q, true", name, ok, "Ben Nevis")
	}
}

// TestFolderPathJoinsAncestorsRootFirst is the ruling itself: "Foo / Bar / Baz
// / Route name", space-slash-space, outermost folder first.
func TestFolderPathJoinsAncestorsRootFirst(t *testing.T) {
	d := &Document{Folders: []Folder{
		{ID: "baz", Name: "Baz", Parent: "bar"},
		{ID: "foo", Name: "Foo"},
		{ID: "bar", Name: "Bar", Parent: "foo"},
	}}
	path, ok := FolderPath(d, "baz")
	if !ok || path != "Foo / Bar / Baz" {
		t.Fatalf("FolderPath = %q, %v; want %q, true", path, ok, "Foo / Bar / Baz")
	}
	name, ok := FlatName(d, "baz", "Route name")
	if !ok || name != "Foo / Bar / Baz / Route name" {
		t.Fatalf("FlatName = %q, %v; want %q, true", name, ok, "Foo / Bar / Baz / Route name")
	}
}

// TestFolderPathCannotHangOnACycle is the acceptance criterion "a writer must
// not hang on any input".
//
// ⚠ THE TRIGGER IS NARROW AND THE TEST SAYS SO. resolveParents clears a
// circular parent at DECODE, so a Document read from a file never carries one.
// A HAND-BUILT Document can, and pkg/vnx is a public package whose Document is
// an ordinary Go struct — which is the whole reason the bound is not optional.
//
// If the bound is removed this test does not fail; it never returns. That is
// what the deadline is for: `go test` kills the binary after its timeout and
// the failure is a panic naming this function, which is loud but slow. The
// go-routine-with-a-timeout shape below turns it back into a normal red.
func TestFolderPathCannotHangOnACycle(t *testing.T) {
	d := &Document{Folders: []Folder{
		{ID: "a", Name: "A", Parent: "b"},
		{ID: "b", Name: "B", Parent: "a"},
	}}
	done := make(chan struct{})
	var path string
	var ok bool
	go func() {
		path, ok = FolderPath(d, "a")
		close(done)
	}()
	select {
	case <-done:
	case <-timeoutAfterASecond():
		t.Fatal("FolderPath did not return on a cyclic parent chain — the walk is not bounded, and " +
			"a writer given this document would spin forever instead of exporting")
	}
	if ok || path != "" {
		t.Fatalf("FolderPath on a cycle = %q, %v; want \"\", false — there is no path, and a "+
			"partial one would be a plausible answer nothing marks as partial", path, ok)
	}
	// The item is KEPT under its bare name: losing a position in a tree these
	// formats cannot express is not worth dropping content over.
	if name, ok := FlatName(d, "a", "Ben Nevis"); ok || name != "Ben Nevis" {
		t.Fatalf("FlatName on a cycle = %q, %v; want the bare name and false", name, ok)
	}
}

// TestFolderPathRefusesAnUnknownFolder covers the other unbuildable path: an
// item naming a folder that is not in this document. Reachable from `vnx
// convert` on a third-party file, where nothing minted the folders.
func TestFolderPathRefusesAnUnknownFolder(t *testing.T) {
	d := &Document{Folders: []Folder{{ID: "foo", Name: "Foo"}}}
	if path, ok := FolderPath(d, "nope"); ok || path != "" {
		t.Fatalf("FolderPath(unknown) = %q, %v; want \"\", false", path, ok)
	}
	// A KNOWN folder whose parent is unknown is the same case: the rest of the
	// path is unknowable, so there is no path.
	d.Folders = append(d.Folders, Folder{ID: "orphan", Name: "Orphan", Parent: "gone"})
	if path, ok := FolderPath(d, "orphan"); ok || path != "" {
		t.Fatalf("FolderPath(dangling parent) = %q, %v; want \"\", false — returning \"Orphan\" "+
			"would be a half-path with nothing marking it partial", path, ok)
	}
}

// TestEveryWriterCodeIsRegisteredAndDistinct is the liveness check on the Code
// registry. An empty registry and a complete one are the same green from
// vnxbridge's mapping guard, which enumerates THIS list.
func TestEveryWriterCodeIsRegisteredAndDistinct(t *testing.T) {
	codes := WriterCodes()
	const floor = 6
	if len(codes) < floor {
		t.Fatalf("WriterCodes returned %d codes, expected at least %d — spec 5b T3 declares the "+
			"whole set (GPX network, GPX area, application key, empty folder, folder path, no geometry) so "+
			"that three parallel writer agents never edit diagnostic.go at once", len(codes), floor)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if c == "" {
			t.Error("a registered Code is the empty string — an empty Code is refused at the bridge")
		}
		if seen[c] {
			t.Errorf("Code %q is registered twice — one Code, one meaning, one key", c)
		}
		seen[c] = true
	}
	for _, want := range []string{
		CodeGPXNetworkUnsupported, CodeGPXAreaUnsupported, CodeAppKeyUnsupported,
		CodeEmptyFolderDropped, CodeFolderPathUnavailable, CodeItemNoGeometry,
	} {
		if !seen[want] {
			t.Errorf("Code %q is declared but not in WriterCodes — writerCode() is what registers "+
				"it, and a Code registered nowhere is a Code the mapping guard cannot see", want)
		}
	}
}

// TestWriterCodePrefixesNameTheScopeOfTheRule pins the naming scheme T4-T6
// must follow, because "which prefix" is exactly the question three parallel
// agents answer three ways.
func TestWriterCodePrefixesNameTheScopeOfTheRule(t *testing.T) {
	allowed := map[string]bool{"gpx": true, "kml": true, "geojson": true, "app-key": true, "folder": true, "item": true}
	for _, c := range WriterCodes() {
		prefix, rest, found := strings.Cut(c, ".")
		if !found || rest == "" {
			t.Errorf("Code %q is not `prefix.what` — the prefix names the scope of the rule", c)
			continue
		}
		if !allowed[prefix] {
			t.Errorf("Code %q has prefix %q, which is neither a format (only that format drops "+
				"the thing) nor a model noun (every format does)", c, prefix)
		}
		if strings.ContainsAny(rest, "_ ") {
			t.Errorf("Code %q spells its tail with an underscore or a space — the codes are "+
				"hyphenated, and the spelling is a wire commitment the public CLI prints", c)
		}
	}
}

// --- StickyWriter -----------------------------------------------------------

type failingWriter struct {
	after int // bytes to accept before failing
	n     int
	calls int
	err   error
}

func (f *failingWriter) Write(p []byte) (int, error) {
	f.calls++
	if f.n >= f.after {
		return 0, f.err
	}
	room := f.after - f.n
	if room >= len(p) {
		f.n += len(p)
		return len(p), nil
	}
	f.n += room
	return room, f.err
}

// TestStickyWriterLatchesAndStopsCalling is the property spec §4.1b names: the
// first error is kept, later writes are no-ops, and the sink underneath is not
// touched again. A writer that kept calling a failing file would keep
// half-writing after the failure it already lost.
func TestStickyWriterLatchesAndStopsCalling(t *testing.T) {
	boom := errors.New("disk gone")
	sink := &failingWriter{after: 4, err: boom}
	b := Stick(sink)
	b.WriteString("abcd")
	if b.Err() != nil {
		t.Fatalf("the working case failed: %v — a test whose sink fails immediately proves nothing", b.Err())
	}
	b.WriteString("efgh")
	if !errors.Is(b.Err(), boom) {
		t.Fatalf("Err = %v, want the sink's error", b.Err())
	}
	before := sink.calls
	b.WriteString("ijkl")
	b.WriteBytes([]byte("mnop"))
	if sink.calls != before {
		t.Errorf("the sink was called %d more time(s) after the latch — a latched writer must "+
			"no-op", sink.calls-before)
	}
}

// shortWriter accepts fewer bytes than it is given and reports NO error, which
// io.Writer forbids and real code does anyway.
type shortWriter struct{ total int }

func (s *shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	s.total += len(p) - 1
	return len(p) - 1, nil
}

// TestStickyWriterTreatsAShortWriteAsAnError is the case a hand-rolled wrapper
// gets wrong: an ignored short write means the producer returns nil, the sink
// Syncs, sets `committed`, and renames a TRUNCATED file into place as a
// successful export.
func TestStickyWriterTreatsAShortWriteAsAnError(t *testing.T) {
	b := Stick(&shortWriter{})
	b.WriteString("hello")
	if !errors.Is(b.Err(), io.ErrShortWrite) {
		t.Fatalf("Err = %v, want io.ErrShortWrite — a short write with no error is still a lost "+
			"byte, and trusting io.Writer's promise here hands the sink a truncated document", b.Err())
	}
}

// TestStickIsIdempotent pins the re-entrancy the encoders depend on:
// they recurse and every level calls Stick, so a second wrapper would latch its
// errors into an object the level above never reads.
func TestStickIsIdempotent(t *testing.T) {
	b := Stick(&shortWriter{})
	if again := Stick(b); again != b {
		t.Fatal("Stick wrapped an already-sticky writer — the inner one would hold the error and " +
			"the outer one would report clean")
	}
}

// TestStickyWriterIsTheOneTheEncoderUses closes the acceptance criterion that
// the encoder and the exported writer are ONE implementation. If the encoder
// grew a private copy, its errors would not reach a caller through this type.
func TestStickyWriterIsTheOneTheEncoderUses(t *testing.T) {
	sink := &failingWriter{after: 0, err: errors.New("no room")}
	if err := EncodeTo(sink, &Document{Points: []Point{{Name: "x", At: Position{Lon: -3, Lat: 57}}}}); err == nil {
		t.Fatal("EncodeTo returned nil against a sink that accepts nothing — it is not routing " +
			"its writes through StickyWriter, so a truncated document would be reported as a " +
			"clean one")
	}
}

// timeoutAfterASecond is the hang detector's clock. It is a helper rather than
// an inline time.After so the intent reads at the call site: this is not a
// performance assertion, it is "the bound is gone".
func timeoutAfterASecond() <-chan time.Time { return time.After(time.Second) }

// A lossy writer names every in-network item by its own name (a highlight's
// is optional, §3.3.3), and every item with no name by an integer unique
// within its network (Dave, 2026-11-03): counted in the order points, areas,
// highlights, skipping an integer another item already is.
func TestLabelNetworkNamesEveryInNetworkItem(t *testing.T) {
	d := &Document{Networks: []Network{
		{
			Name:       "A",
			Points:     []Point{{Name: "Hut"}, {}},
			Areas:      []Area{{Name: "2"}, {}},
			Highlights: []Highlight{{Name: "Ridge"}, {}, {}},
		},
		{Name: "B", Highlights: []Highlight{{}}},
	}}
	a := LabelNetwork(d, 0)
	got := fmt.Sprint(a.Points, a.Areas, a.Highlights)
	// "2" is taken by an area's own name, so the integers skip it.
	if want := "[Hut 1] [2 3] [Ridge 4 5]"; got != want {
		t.Errorf("network A labels %s, want %s", got, want)
	}
	b := LabelNetwork(d, 1)
	if fmt.Sprint(b.Highlights) != "[1]" {
		t.Errorf("network B labels %v, want [1]: the integers are per network", b.Highlights)
	}
}
