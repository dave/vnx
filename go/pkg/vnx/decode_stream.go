package vnx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

// DecodeReader reads a VNX document from a stream. It returns what [Decode]
// returns, and [Decode] is this function over its bytes.
//
// The document is read token by token, and only what the [Document] keeps
// stays resident: a track's pips, an arc's pips and an area's ring are read
// one position at a time, so a document of any size costs about its Document
// and not a generic JSON tree of it, which is several times larger.
//
// ⚠ THE RESULT IS THE SAME AS READING A TREE, in every respect a caller can
// see: the same Document, the same diagnostics in the same order, the same
// refusals. Keys may come in any order, so diagnostics are held per key and
// emitted at the end in the order the format fixes. A key that appears twice
// is read twice and the last one wins, its diagnostics with it. An arc is
// finished once its owner's `nodes` is known, in sorted id order.
//
// ⚠ NOTHING IS REFUSED BEFORE THE END. A document that is not an object, or
// declares a version this reader refuses, is still read to its end, so a
// syntax error anywhere is [ErrMalformed] whatever came before it, and every
// number is parsed, so `1e400` anywhere is [ErrMalformed] too.
//
// An error from r is returned as it is, not as [ErrMalformed]: a reader that
// fails, or one that stops because a context was cancelled, is not a document
// that is malformed.
func DecodeReader(r io.Reader) (*Document, []Diagnostic, error) {
	src := &sourceReader{r: r}
	s := &streamReader{dec: json.NewDecoder(src)}
	doc, diags, refusal, err := s.document()
	// ⚠ src.err IS CHECKED WHETHER OR NOT THE DOCUMENT READ. json.Decoder can
	// drop a reader error that arrives with the last bytes of a value it then
	// completes, and a .vnz entry's CRC is checked on exactly that read.
	if src.err != nil {
		return nil, nil, src.err
	}
	if err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return nil, nil, fmt.Errorf("%w: %s", ErrMalformed, err)
	}
	if refusal != nil {
		return nil, nil, refusal
	}
	return doc, diags, nil
}

// sourceReader keeps the error its reader returned, so that DecodeReader can
// tell a failed read from bad JSON.
type sourceReader struct {
	r   io.Reader
	err error
}

func (s *sourceReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && err != io.EOF {
		s.err = err
	}
	return n, err
}

type streamReader struct {
	dec *json.Decoder
}

// collected is one of the document's collections: its items and the
// diagnostics they raised, held until the end so that they are emitted in the
// format's order whatever order the keys came in.
type collected[T any] struct {
	items []T
	// paths is the source path of each kept item, parallel to items. An
	// item's index in items is not its index in the file once an earlier one
	// was dropped, and a check made after the whole document is read (an
	// item's `folder`) must still report at the item's own place.
	paths []string
	diags []Diagnostic
}

// document reads the whole input. refusal is a document refused as a whole;
// err is input that cannot be read as JSON.
func (s *streamReader) document() (doc *Document, diags []Diagnostic, refusal, err error) {
	t, err := s.dec.Token()
	if err != nil {
		return nil, nil, nil, err
	}
	if t != json.Delim('{') {
		if err := s.skip(t); err != nil {
			return nil, nil, nil, err
		}
		if err := s.end(); err != nil {
			return nil, nil, nil, err
		}
		return nil, nil, fmt.Errorf("%w: top level is %s, not an object", ErrNotVNX, kindOf(t)), nil
	}

	// top holds every top-level key but the six collections: the header
	// fields and the application keys with their values, and each unknown key
	// with nil, because only its name is used.
	top := map[string]any{}
	var (
		folders  collected[Folder]
		points   collected[Point]
		networks collected[Network]
		plans    collected[Plan]
		tracks   collected[Track]
		areas    collected[Area]
	)
	for s.dec.More() {
		key, err := s.key()
		if err != nil {
			return nil, nil, nil, err
		}
		switch key {
		case "folders":
			seen := map[string]bool{}
			folders, err = readArray(s, "/folders", func(at string, diags []Diagnostic) (Folder, bool, []Diagnostic, error) {
				v, err := s.value()
				if err != nil {
					return Folder{}, false, diags, err
				}
				f, ok, diags := folderFromValue(v, at, seen, diags)
				return f, ok, diags, nil
			})
			if err == nil && folders.items != nil {
				folders.items, folders.diags = resolveParents(folders.items, folders.paths, folders.diags)
			}
		case "points":
			points, err = readArray(s, "/points", func(at string, diags []Diagnostic) (Point, bool, []Diagnostic, error) {
				v, err := s.value()
				if err != nil {
					return Point{}, false, diags, err
				}
				w, ok, diags := pointFromValue(v, at, false, diags)
				return w, ok, diags, nil
			})
		case "networks":
			networks, err = readArray(s, "/networks", s.network)
		case "plans":
			plans, err = readArray(s, "/plans", s.plan)
		case "tracks":
			tracks, err = readArray(s, "/tracks", s.track)
		case "areas":
			areas, err = readArray(s, "/areas", s.topArea)
		default:
			if isDocumentKey(key) {
				top[key], err = s.value()
			} else {
				top[key] = nil
				err = s.skipValue()
			}
		}
		if err != nil {
			return nil, nil, nil, err
		}
	}
	if _, err := s.dec.Token(); err != nil { // the closing brace
		return nil, nil, nil, err
	}
	if err := s.end(); err != nil {
		return nil, nil, nil, err
	}

	declared, ok := top["vnx"].(string)
	if !ok {
		if v, present := top["vnx"]; present {
			return nil, nil, fmt.Errorf("%w: `vnx` is %s, not a string", ErrNotVNX, jsonKind(v)), nil
		}
		return nil, nil, fmt.Errorf("%w: no `vnx` key", ErrNotVNX), nil
	}
	if err := checkVersion(declared); err != nil {
		return nil, nil, err, nil
	}

	// ⚠ AN ITEM'S `folder` IS CHECKED HERE, AFTER EVERY KEY IS READ, because
	// `folders` may come after the collections that name them. The parents
	// were resolved when `folders` was read; that check needs nothing else.
	held := folderIDs(folders.items)
	points.diags = clearAbsentFolders(points.items, points.paths, "point", held, func(p *Point) *string { return &p.Folder }, points.diags)
	networks.diags = clearAbsentFolders(networks.items, networks.paths, "network", held, func(n *Network) *string { return &n.Folder }, networks.diags)
	plans.diags = clearAbsentFolders(plans.items, plans.paths, "plan", held, func(r *Plan) *string { return &r.Folder }, plans.diags)
	tracks.diags = clearAbsentFolders(tracks.items, tracks.paths, "track", held, func(t *Track) *string { return &t.Folder }, tracks.diags)
	areas.diags = clearAbsentFolders(areas.items, areas.paths, "area", held, func(a *Area) *string { return &a.Folder }, areas.diags)

	doc = &Document{Version: declared}
	doc.Folders, diags = folders.items, append(diags, folders.diags...)
	doc.Points, diags = points.items, append(diags, points.diags...)
	doc.Networks, diags = networks.items, append(diags, networks.diags...)
	doc.Plans, diags = plans.items, append(diags, plans.diags...)
	doc.Tracks, diags = tracks.items, append(diags, tracks.diags...)
	doc.Areas, diags = areas.items, append(diags, areas.diags...)
	doc.AppKeys = decodeAppKeys(top)
	diags = reportUnknownKeysBy(top, "", isDocumentKey, diags)
	return doc, diags, nil, nil
}

// readArray reads one top-level collection, element by element. A key present
// but holding something other than an array is a diagnostic, and absence is
// normal: the caller never calls this for an absent key.
func readArray[T any](s *streamReader, path string, elem func(at string, diags []Diagnostic) (T, bool, []Diagnostic, error)) (collected[T], error) {
	var c collected[T]
	t, err := s.dec.Token()
	if err != nil {
		return c, err
	}
	if t != json.Delim('[') {
		c.diags = []Diagnostic{{Severity: Warning, Path: path,
			Message: fmt.Sprintf("`%s` is %s, expected an array — ignored", path[1:], kindOf(t))}}
		return c, s.skip(t)
	}
	c.items = make([]T, 0)
	for i := 0; s.dec.More(); i++ {
		item, ok, diags, err := elem(fmt.Sprintf("%s/%d", path, i), c.diags)
		c.diags = diags
		if err != nil {
			return c, err
		}
		if ok {
			c.items = append(c.items, item)
			c.paths = append(c.paths, fmt.Sprintf("%s/%d", path, i))
		}
	}
	_, err = s.dec.Token() // the closing bracket
	return c, err
}

// track reads one element of `tracks`, streaming its `segments`.
func (s *streamReader) track(at string, diags []Diagnostic) (Track, bool, []Diagnostic, error) {
	t, err := s.dec.Token()
	if err != nil {
		return Track{}, false, diags, err
	}
	if t != json.Delim('{') {
		return Track{}, false, append(diags, Diagnostic{Severity: Warning, Path: at,
			Message: fmt.Sprintf("track is %s, expected an object — dropped", kindOf(t))}), s.skip(t)
	}
	var segs []Segment
	var segDiags []Diagnostic
	segPresent, segOK := false, false
	obj, err := s.object(trackKeys, map[string]func() error{
		"segments": func() (err error) {
			segPresent = true
			segs, segOK, segDiags, err = s.segments(at + "/segments")
			return err
		},
	})
	if err != nil {
		return Track{}, false, diags, err
	}
	// `segments` is REQUIRED (§3.10). §6.3 rule 1: an absent one names the
	// track, a wrong-typed one the key.
	if !segPresent {
		return Track{}, false, append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodeWrongTypeRequired,
			Message: "`segments` is missing — track dropped"}), nil
	}
	if !segOK {
		return Track{}, false, append(diags, segDiags...), nil
	}
	tr, diags := trackFromObject(obj, at, segs, segDiags, diags)
	return tr, true, diags, nil
}

// segments reads a track's `segments`. ok is false when the value is not an
// array, which drops the track.
func (s *streamReader) segments(path string) ([]Segment, bool, []Diagnostic, error) {
	t, err := s.dec.Token()
	if err != nil {
		return nil, false, nil, err
	}
	if t != json.Delim('[') {
		return nil, false, []Diagnostic{{Severity: Warning, Path: path, Code: CodeWrongTypeRequired,
			Message: fmt.Sprintf("`segments` is %s, expected an array — track dropped", kindOf(t))}}, s.skip(t)
	}
	out := make([]Segment, 0)
	var diags []Diagnostic
	for i := 0; s.dec.More(); i++ {
		at := fmt.Sprintf("%s/%d", path, i)
		t, err := s.dec.Token()
		if err != nil {
			return nil, false, nil, err
		}
		if t != json.Delim('{') {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("segment is %s, expected an object — dropped", kindOf(t))})
			if err := s.skip(t); err != nil {
				return nil, false, nil, err
			}
			continue
		}
		var pr pipsRead
		obj, err := s.object(segmentKeys, map[string]func() error{
			"pips": func() error {
				pr = pipsRead{}
				return s.pips(&pr, at+"/pips", false)
			},
		})
		if err != nil {
			return nil, false, nil, err
		}
		diags = reportUnknownKeys(obj, at, segmentKeys, diags)
		// `pips` is REQUIRED (§3.10): without it the segment is dropped. §6.3
		// rule 1: an absent one names the segment, a wrong-typed one the key.
		if !pr.present {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodeWrongTypeRequired,
				Message: "`pips` is missing — segment dropped"})
			continue
		}
		if !pr.isArray {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at + "/pips", Code: CodeWrongTypeRequired,
				Message: fmt.Sprintf("`pips` is %s, expected an array — segment dropped", pr.kind)})
			continue
		}
		diags = append(diags, pr.diags...)
		if pr.failed {
			// The pips reader reported an unusable position. For a track the
			// owner it drops is the SEGMENT: spec §3.5 already makes the
			// interval between two segments a pause rather than geometry, so a
			// missing segment leaves a gap a consumer can see, where a missing
			// pip inside one leaves a shorter, plausible, wrong line.
			continue
		}
		out = append(out, Segment{Pips: pr.positions})
	}
	_, err = s.dec.Token() // the closing bracket
	return out, true, diags, err
}

// pipsRead is a position list read by [streamReader.pips]: whether the key was
// present, the JSON kind of a value that is not an array, and the positions.
type pipsRead struct {
	present, isArray bool
	kind             string
	// failed means a position was unusable; diags names it, and the owner of
	// the list is dropped.
	failed    bool
	positions []Position
	vertices  []Vertex
	// timed means a vertex carried a time, which a vertex drops.
	timed bool
	diags []Diagnostic
}

// pips reads a position list one position at a time, into positions, or into
// vertices for an arc.
//
// ⚠ AN UNUSABLE POSITION DOES NOT DROP THE PIP. Spec §6.2: geometry is
// continuous and structure is not, so a missing vertex changes the shape of a
// line and leaves nothing to indicate it — a shorter, plausible, wrong line.
// It marks the list failed, and the CALLER drops the object that owns it: an
// arc's pips drop the ARC, a track segment's pips drop the SEGMENT. The rest
// of the list is still read, because the document is still JSON to be checked.
func (s *streamReader) pips(pr *pipsRead, path string, vertices bool) error {
	pr.present = true
	t, err := s.dec.Token()
	if err != nil {
		return err
	}
	if t != json.Delim('[') {
		pr.kind = kindOf(t)
		return s.skip(t)
	}
	pr.isArray = true
	if vertices {
		pr.vertices = make([]Vertex, 0)
	} else {
		pr.positions = make([]Position, 0)
	}
	for i := 0; s.dec.More(); i++ {
		v, err := s.value()
		if err != nil {
			return err
		}
		if pr.failed {
			continue
		}
		posArr, isArr := v.([]any)
		if !isArr {
			pr.fail(Diagnostic{Severity: Warning, Path: fmt.Sprintf("%s/%d", path, i),
				Code:    CodePositionUnusable,
				Message: fmt.Sprintf("pip is %s, expected an array", jsonKind(v))})
			continue
		}
		// The path is only in an error's text, so it is built only for one.
		p, err := decodePosition(posArr, "")
		if err != nil {
			at := fmt.Sprintf("%s/%d", path, i)
			_, err = decodePosition(posArr, at)
			pr.fail(Diagnostic{Severity: Warning, Path: at, Code: CodePositionUnusable, Message: err.Error()})
			continue
		}
		if vertices {
			pr.timed = pr.timed || p.Time != nil
			pr.vertices = append(pr.vertices, Vertex{Lon: p.Lon, Lat: p.Lat, Ele: p.Ele})
		} else {
			pr.positions = append(pr.positions, p)
		}
	}
	_, err = s.dec.Token() // the closing bracket
	return err
}

func (pr *pipsRead) fail(d Diagnostic) {
	pr.failed = true
	pr.positions, pr.vertices = nil, nil
	pr.diags = append(pr.diags, d)
}

// graphRead is a network's or a plan's `nodes` and `arcs` as read. An arc is
// finished only by [graphRead.finish], because `nodes` may come after `arcs`.
type graphRead struct {
	// nodesKind and arcsKind are the JSON kind of a value that is not an
	// object; "" when the key is absent, null or an object.
	nodesKind, arcsKind string
	nodes               map[string]nodeRead
	arcs                map[string]*arcRead
}

type nodeRead struct {
	v     Vertex
	ok    bool
	diags []Diagnostic
}

type arcRead struct {
	kind string // the JSON kind of an arc that is not an object
	obj  map[string]any
	pips pipsRead
}

func (s *streamReader) nodes(g *graphRead, path string) error {
	g.nodesKind, g.nodes = "", nil
	t, err := s.dec.Token()
	if err != nil || t == nil {
		return err
	}
	if t != json.Delim('{') {
		g.nodesKind = kindOf(t)
		return s.skip(t)
	}
	g.nodes = map[string]nodeRead{}
	for s.dec.More() {
		id, err := s.key()
		if err != nil {
			return err
		}
		v, err := s.value()
		if err != nil {
			return err
		}
		var n nodeRead
		n.v, n.ok, n.diags = nodeFromValue(v, path+"/"+escapePointerToken(id), nil)
		g.nodes[id] = n
	}
	_, err = s.dec.Token() // the closing brace
	return err
}

func (s *streamReader) arcs(g *graphRead, path string) error {
	g.arcsKind, g.arcs = "", nil
	t, err := s.dec.Token()
	if err != nil || t == nil {
		return err
	}
	if t != json.Delim('{') {
		g.arcsKind = kindOf(t)
		return s.skip(t)
	}
	g.arcs = map[string]*arcRead{}
	for s.dec.More() {
		id, err := s.key()
		if err != nil {
			return err
		}
		t, err := s.dec.Token()
		if err != nil {
			return err
		}
		if t != json.Delim('{') {
			g.arcs[id] = &arcRead{kind: kindOf(t)}
			if err := s.skip(t); err != nil {
				return err
			}
			continue
		}
		ar := &arcRead{}
		at := path + "/" + escapePointerToken(id)
		ar.obj, err = s.object(arcKeys, map[string]func() error{
			"pips": func() error {
				ar.pips = pipsRead{}
				return s.pips(&ar.pips, at+"/pips", true)
			},
		})
		if err != nil {
			return err
		}
		g.arcs[id] = ar
	}
	_, err = s.dec.Token() // the closing brace
	return err
}

// finish emits the nodes' and then the arcs' diagnostics, each in sorted id
// order, and checks every arc against the nodes that survived.
func (g *graphRead) finish(owner string, diags []Diagnostic) (map[string]Vertex, map[string]Arc, []Diagnostic) {
	var nodes map[string]Vertex
	if g.nodesKind != "" {
		diags = append(diags, Diagnostic{Severity: Warning, Path: owner + "/nodes",
			Message: fmt.Sprintf("`nodes` is %s, expected an object — ignored", g.nodesKind)})
	} else if g.nodes != nil {
		nodes = make(map[string]Vertex, len(g.nodes))
		for _, id := range sortedIDs(g.nodes) {
			n := g.nodes[id]
			diags = append(diags, n.diags...)
			if n.ok {
				nodes[id] = n.v
			}
		}
	}
	var arcs map[string]Arc
	if g.arcsKind != "" {
		diags = append(diags, Diagnostic{Severity: Warning, Path: owner + "/arcs",
			Message: fmt.Sprintf("`arcs` is %s, expected an object — ignored", g.arcsKind)})
	} else if g.arcs != nil {
		arcs = make(map[string]Arc, len(g.arcs))
		for _, id := range sortedIDs(g.arcs) {
			ar := g.arcs[id]
			at := owner + "/arcs/" + escapePointerToken(id)
			if ar.kind != "" {
				diags = append(diags, Diagnostic{Severity: Warning, Path: at,
					Message: fmt.Sprintf("arc is %s, expected an object — dropped", ar.kind)})
				continue
			}
			diags = reportUnknownKeys(ar.obj, at, arcKeys, diags)
			a, ok, d := arcFromObject(ar.obj, at, nodes, &ar.pips, diags)
			diags = d
			if ok {
				arcs[id] = a
			}
		}
	}
	return nodes, arcs, diags
}

func sortedIDs[V any](m map[string]V) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// plan reads one element of `plans`, streaming its graph.
func (s *streamReader) plan(at string, diags []Diagnostic) (Plan, bool, []Diagnostic, error) {
	t, err := s.dec.Token()
	if err != nil {
		return Plan{}, false, diags, err
	}
	if t != json.Delim('{') {
		return Plan{}, false, append(diags, Diagnostic{Severity: Warning, Path: at,
			Message: fmt.Sprintf("plan is %s, expected an object — dropped", kindOf(t))}), s.skip(t)
	}
	g := &graphRead{}
	obj, err := s.object(planKeys, map[string]func() error{
		"nodes": func() error { return s.nodes(g, at+"/nodes") },
		"arcs":  func() error { return s.arcs(g, at+"/arcs") },
	})
	if err != nil {
		return Plan{}, false, diags, err
	}
	p, ok, diags := planFromObject(obj, at, g, diags)
	return p, ok, diags, nil
}

// network reads one element of `networks`, streaming its graph and its areas.
func (s *streamReader) network(at string, diags []Diagnostic) (Network, bool, []Diagnostic, error) {
	t, err := s.dec.Token()
	if err != nil {
		return Network{}, false, diags, err
	}
	if t != json.Delim('{') {
		return Network{}, false, append(diags, Diagnostic{Severity: Warning, Path: at,
			Message: fmt.Sprintf("network is %s, expected an object — dropped", kindOf(t))}), s.skip(t)
	}
	g := &graphRead{}
	areas := &areasRead{}
	obj, err := s.object(networkKeys, map[string]func() error{
		"nodes": func() error { return s.nodes(g, at+"/nodes") },
		"arcs":  func() error { return s.arcs(g, at+"/arcs") },
		"areas": func() error {
			*areas = areasRead{}
			return s.networkAreas(areas, at+"/areas")
		},
	})
	if err != nil {
		return Network{}, false, diags, err
	}
	n, diags := networkFromObject(obj, at, g, areas, diags)
	return n, true, diags, nil
}

// areasRead is a network's `areas` as read.
type areasRead struct {
	isArray bool
	items   []Area
	diags   []Diagnostic
}

func (s *streamReader) networkAreas(ar *areasRead, path string) error {
	t, err := s.dec.Token()
	if err != nil {
		return err
	}
	if t != json.Delim('[') {
		return s.skip(t)
	}
	ar.isArray = true
	ar.items = make([]Area, 0)
	for i := 0; s.dec.More(); i++ {
		a, ok, diags, err := s.area(fmt.Sprintf("%s/%d", path, i), true, ar.diags)
		ar.diags = diags
		if err != nil {
			return err
		}
		if ok {
			ar.items = append(ar.items, a)
		}
	}
	_, err = s.dec.Token() // the closing bracket
	return err
}

// ringRead is an area's `polygon` as read: ok is false when the area is
// dropped, and diags says why.
type ringRead struct {
	ring  []Position
	ok    bool
	diags []Diagnostic
}

// topArea reads one element of the top-level `areas`.
func (s *streamReader) topArea(at string, diags []Diagnostic) (Area, bool, []Diagnostic, error) {
	return s.area(at, false, diags)
}

// area reads one element of a top-level or a network's (inNetwork) `areas`,
// streaming its polygon.
func (s *streamReader) area(at string, inNetwork bool, diags []Diagnostic) (Area, bool, []Diagnostic, error) {
	t, err := s.dec.Token()
	if err != nil {
		return Area{}, false, diags, err
	}
	if t != json.Delim('{') {
		return Area{}, false, append(diags, Diagnostic{Severity: Warning, Path: at,
			Message: fmt.Sprintf("item is %s, expected an object — dropped", kindOf(t))}), s.skip(t)
	}
	rr := missingRing(at)
	keys := areaKeys
	if inNetwork {
		keys = networkAreaKeys
	}
	obj, err := s.object(keys, map[string]func() error{
		"polygon": func() error {
			rr = ringRead{}
			return s.ring(&rr, at+"/polygon")
		},
	})
	if err != nil {
		return Area{}, false, diags, err
	}
	a, ok, diags := areaFromObject(obj, at, &rr, inNetwork, diags)
	return a, ok, diags, nil
}

// missingRing is the result for an area with no `polygon`. §6.3 rule 1: an
// absent key has no element of its own, so at is the AREA's path.
func missingRing(at string) ringRead {
	return ringRead{diags: []Diagnostic{{Severity: Warning, Path: at, Code: CodeWrongTypeRequired,
		Message: "polygon is missing — area dropped"}}}
}

// ring reads the single OPEN ring an area's polygon is (SPEC §3.7), one
// position at a time.
//
// Nested rings — the GeoJSON shape an earlier draft of the example promised —
// are reported and the area dropped, never silently flattened to the outer
// ring and never silently merged. A reader that quietly kept ring 1 would draw
// an area whose holes had vanished, with nothing to say so.
//
// ⚠ A CLOSED RING IS REPAIRED, NOT DROPPED. Its last position repeating its
// first is one fact written twice, so dropping the repeat loses nothing; the
// diagnostic is kept so that a writer that closes rings can be seen. The count
// of distinct positions is taken after that, so [A, B, A] is two points and
// its area goes.
func (s *streamReader) ring(rr *ringRead, path string) error {
	t, err := s.dec.Token()
	if err != nil {
		return err
	}
	if t != json.Delim('[') {
		rr.diags = []Diagnostic{{Severity: Warning, Path: path, Code: CodeWrongTypeRequired,
			Message: fmt.Sprintf("polygon is %s, expected an array of positions — area dropped", kindOf(t))}}
		return s.skip(t)
	}
	ring := make([]Position, 0)
	failed := false
	fail := func(d Diagnostic) {
		failed = true
		ring = nil
		rr.diags = append(rr.diags, d)
	}
	for i := 0; s.dec.More(); i++ {
		v, err := s.value()
		if err != nil {
			return err
		}
		if failed {
			continue
		}
		if i == 0 {
			if first, isArr := v.([]any); isArr && len(first) > 0 {
				if _, nested := first[0].([]any); nested {
					fail(Diagnostic{Severity: Warning, Path: path,
						Code: CodePolygonNestedRings, Message: "polygon holds nested rings; this format carries ONE ring and cannot express holes — area dropped"})
					continue
				}
			}
		}
		posArr, isArr := v.([]any)
		if !isArr {
			fail(Diagnostic{Severity: Warning, Path: fmt.Sprintf("%s/%d", path, i),
				Code:    CodePositionUnusable,
				Message: fmt.Sprintf("ring position is %s — area dropped", jsonKind(v))})
			continue
		}
		p, err := decodePosition(posArr, "")
		if err != nil {
			at := fmt.Sprintf("%s/%d", path, i)
			_, err = decodePosition(posArr, at)
			fail(Diagnostic{Severity: Warning, Path: at, Code: CodePositionUnusable, Message: err.Error() + " — area dropped"})
			continue
		}
		ring = append(ring, p)
	}
	if _, err := s.dec.Token(); err != nil { // the closing bracket
		return err
	}
	if failed {
		return nil
	}
	if n := len(ring); n >= 2 && ring[0].Lon == ring[n-1].Lon && ring[0].Lat == ring[n-1].Lat {
		rr.diags = append(rr.diags, Diagnostic{Severity: Warning, Path: fmt.Sprintf("%s/%d", path, n-1), Code: CodeRingClosed,
			Message: "a ring is stored open, and this last position repeats the first — the repeat is dropped"})
		ring = ring[:n-1]
	}
	if d := distinctLonLat(ring); d < 3 {
		rr.diags = append(rr.diags, Diagnostic{Severity: Warning, Path: path, Code: CodeRingTooFewPoints,
			Message: fmt.Sprintf("ring has %d distinct position(s), needs at least 3 — area dropped", d)})
		return nil
	}
	rr.ring, rr.ok = ring, true
	return nil
}

// object reads the members of an object whose opening brace is read. A key in
// streamed is read by its function, and is not in the map returned. Every
// other key in known is decoded into the map; a key in neither is skipped and
// held as nil, because only its name is used, to report it unknown.
//
// ⚠ A KEY THAT APPEARS TWICE IS READ TWICE, AND THE LAST ONE WINS, as it does
// for encoding/json. A streamed key's function must therefore reset what it
// read before.
func (s *streamReader) object(known map[string]bool, streamed map[string]func() error) (map[string]any, error) {
	obj := map[string]any{}
	for s.dec.More() {
		key, err := s.key()
		if err != nil {
			return nil, err
		}
		if read, ok := streamed[key]; ok {
			err = read()
		} else if known[key] {
			obj[key], err = s.value()
		} else {
			obj[key] = nil
			err = s.skipValue()
		}
		if err != nil {
			return nil, err
		}
	}
	_, err := s.dec.Token() // the closing brace
	return obj, err
}

func (s *streamReader) key() (string, error) {
	t, err := s.dec.Token()
	if err != nil {
		return "", err
	}
	k, ok := t.(string)
	if !ok {
		return "", fmt.Errorf("object key is %v", t)
	}
	return k, nil
}

// value reads one whole value as a generic tree.
func (s *streamReader) value() (any, error) {
	var v any
	err := s.dec.Decode(&v)
	return v, err
}

// skipValue reads one value and keeps none of it.
func (s *streamReader) skipValue() error {
	t, err := s.dec.Token()
	if err != nil {
		return err
	}
	return s.skip(t)
}

// skip reads the rest of a value whose first token is t. Every token is still
// read, so a syntax error or a number out of range is still found.
func (s *streamReader) skip(t json.Token) error {
	if t != json.Delim('{') && t != json.Delim('[') {
		return nil
	}
	for depth := 1; depth > 0; {
		t, err := s.dec.Token()
		if err != nil {
			return err
		}
		switch t {
		case json.Delim('{'), json.Delim('['):
			depth++
		case json.Delim('}'), json.Delim(']'):
			depth--
		}
	}
	return nil
}

// end checks that nothing but white space follows the document.
func (s *streamReader) end() error {
	t, err := s.dec.Token()
	if err == io.EOF {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("invalid data after top-level value: %v", t)
}

// kindOf names the JSON kind of a value from its first token.
func kindOf(t json.Token) string {
	switch t {
	case json.Delim('{'):
		return "an object"
	case json.Delim('['):
		return "an array"
	}
	return jsonKind(t)
}
