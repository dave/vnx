package vnx

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// The framing layer.
//
// ⚠ IT IS PUNCTUATION AND NOTHING ELSE. Every VALUE below — every string, every
// number, every key, every item — goes through encoding/json. What this file
// writes is braces, brackets, commas and indentation, so that a document can be
// emitted without ever holding more than one item at a time. There is no string
// escaping, no number formatting and no key sorting here, and none may be added:
// a second answer to any of those is how two encoders in one library start
// disagreeing.
//
// Peak memory is O(the largest single item), not O(the document): the document-
// and network-level scaffolding is written directly, and each item is built,
// rendered and dropped before the next one starts. [Encode] is a bytes.Buffer
// over this function and necessarily holds the whole output.

// EncodeTo streams a Document to w.
func EncodeTo(w io.Writer, doc *Document) error {
	if doc == nil {
		return fmt.Errorf("vnx: nothing to encode")
	}
	b := Stick(w)
	if err := streamDocument(b, doc); err != nil {
		return err
	}
	b.WriteString("\n")
	return b.Err()
}

func indent(depth int) string { return strings.Repeat("  ", depth) }

// object writes one JSON object, a member at a time.
type object struct {
	b       *StickyWriter
	depth   int
	members int
}

func openObject(b *StickyWriter, depth int) *object {
	b.WriteString("{")
	return &object{b: b, depth: depth}
}

// key writes a member's name and separator, leaving the caller to write the
// value at depth+1.
func (o *object) key(k string) error {
	if o.members > 0 {
		o.b.WriteString(",")
	}
	o.members++
	o.b.WriteString("\n" + indent(o.depth+1))
	// The key goes through the same renderer as a value, so a string is escaped
	// one way in this package rather than two.
	quoted, err := marshalIndent(k, "")
	if err != nil {
		return err
	}
	o.b.WriteBytes(quoted)
	o.b.WriteString(": ")
	return o.b.Err()
}

// value writes a whole member, rendering v with encoding/json.
func (o *object) value(k string, v any) error {
	if err := o.key(k); err != nil {
		return err
	}
	out, err := marshalIndent(v, indent(o.depth+1))
	if err != nil {
		return err
	}
	o.b.WriteBytes(out)
	return o.b.Err()
}

func (o *object) close() {
	if o.members == 0 {
		o.b.WriteString("}")
		return
	}
	o.b.WriteString("\n" + indent(o.depth) + "}")
}

// streamArray writes an array of values, building ONE at a time and dropping it
// before the next.
func streamArray(b *StickyWriter, depth, n int, item func(i int) (any, error)) error {
	if n == 0 {
		b.WriteString("[]")
		return b.Err()
	}
	b.WriteString("[")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n" + indent(depth+1))
		v, err := item(i)
		if err != nil {
			return err
		}
		out, err := marshalIndent(v, indent(depth+1))
		if err != nil {
			return err
		}
		b.WriteBytes(out)
	}
	b.WriteString("\n" + indent(depth) + "]")
	return b.Err()
}

// streamKeyedObject writes an object whose members come from a map, one value at
// a time.
//
// ⚠ THE KEYS ARE COLLECTED AND SORTED, NEVER ITERATED. Go map iteration is
// randomised per process and per range, so an encoder that iterated them would
// emit a different file on every run with no error and no panic. encoding/json
// sorts a map's keys for the same reason; this does it by hand because it never
// holds the map's values together.
func streamKeyedObject(b *StickyWriter, depth int, keys []string, value func(k string) (any, error)) error {
	sort.Strings(keys)
	o := openObject(b, depth)
	for _, k := range keys {
		v, err := value(k)
		if err != nil {
			return err
		}
		if err := o.value(k, v); err != nil {
			return err
		}
	}
	o.close()
	return b.Err()
}

func streamDocument(b *StickyWriter, doc *Document) error {
	version := doc.Version
	if version == "" {
		version = Version
	}
	o := openObject(b, 0)
	if err := o.value("vnx", version); err != nil {
		return err
	}
	// An empty header field is OMITTED. Writing "" would turn "not known" into
	// a value.
	for _, h := range []struct{ key, value string }{
		{"profile", doc.Profile},
		{"generator", doc.Generator},
		{"created", doc.Created},
	} {
		if h.value == "" {
			continue
		}
		if err := o.value(h.key, h.value); err != nil {
			return err
		}
	}
	for _, c := range documentCollections {
		if c.count(doc) == 0 {
			continue
		}
		if err := o.key(c.key); err != nil {
			return err
		}
		if err := c.emit(b, doc, o.depth+1); err != nil {
			return err
		}
	}
	o.close()
	return b.Err()
}

// documentCollection is one of the document's container keys.
//
// ⚠ THE TABLE IS GUARDED, NOT TRUSTED. `field` names the [Document] field it
// covers, and TestEncodeToCoversEveryCollectionTheDocumentDeclares reflects over
// the struct and fails if a slice field has no entry — so an eighth collection
// added to the model and forgotten here cannot ship as a document that silently
// omits it.
type documentCollection struct {
	field string
	key   string
	count func(*Document) int
	emit  func(b *StickyWriter, doc *Document, depth int) error
}

// In the order [Document] declares them, which is the order they are written.
var documentCollections = []documentCollection{
	{"Folders", "folders", func(d *Document) int { return len(d.Folders) },
		func(b *StickyWriter, d *Document, depth int) error {
			return streamArray(b, depth, len(d.Folders), func(i int) (any, error) { return folderWire(d.Folders[i]) })
		}},
	{"Waypoints", "waypoints", func(d *Document) int { return len(d.Waypoints) },
		func(b *StickyWriter, d *Document, depth int) error {
			return streamArray(b, depth, len(d.Waypoints), func(i int) (any, error) { return waypointWire(d.Waypoints[i], false) })
		}},
	{"Areas", "areas", func(d *Document) int { return len(d.Areas) },
		func(b *StickyWriter, d *Document, depth int) error {
			return streamArray(b, depth, len(d.Areas), func(i int) (any, error) { return areaWire(d.Areas[i], false) })
		}},
	{"Tracks", "tracks", func(d *Document) int { return len(d.Tracks) },
		func(b *StickyWriter, d *Document, depth int) error {
			return streamArray(b, depth, len(d.Tracks), func(i int) (any, error) { return trackWire(d.Tracks[i]) })
		}},
	{"Routes", "routes", func(d *Document) int { return len(d.Routes) },
		func(b *StickyWriter, d *Document, depth int) error {
			return streamArray(b, depth, len(d.Routes), func(i int) (any, error) { return routeWire(d.Routes[i]) })
		}},
	{"Networks", "networks", func(d *Document) int { return len(d.Networks) },
		func(b *StickyWriter, d *Document, depth int) error {
			return streamNetworks(b, d.Networks, depth)
		}},
	{"Rulesets", "rulesets", func(d *Document) int { return len(d.Rulesets) },
		// Verbatim and uninspected (vnx.go): a ruleset is an arbitrary value
		// this package refuses to understand, so it is handed to encoding/json
		// exactly as it was read.
		func(b *StickyWriter, d *Document, depth int) error {
			return streamArray(b, depth, len(d.Rulesets), func(i int) (any, error) { return d.Rulesets[i], nil })
		}},
}

// folderWire is the per-item builder for a folder. It lives here rather than in
// encode.go because nothing else needs it.
func folderWire(f Folder) (wireFolder, error) {
	data, err := dataBodies(f.Data)
	if err != nil {
		return wireFolder{}, err
	}
	return wireFolder{ID: f.ID, Name: f.Name, Notes: f.Notes, Parent: f.Parent, Hidden: f.Hidden, Data: data}, nil
}

func streamNetworks(b *StickyWriter, ns []Network, depth int) error {
	if len(ns) == 0 {
		b.WriteString("[]")
		return b.Err()
	}
	b.WriteString("[")
	for i := range ns {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n" + indent(depth+1))
		if err := streamNetwork(b, ns[i], depth+1); err != nil {
			return err
		}
	}
	b.WriteString("\n" + indent(depth) + "]")
	return b.Err()
}

// streamNetwork writes one network. It is the one object with the pips inside
// it, so this is where streaming earns its keep: an arc is built, written and
// dropped before the next one starts.
//
// Members follow the order [Network] declares them.
func streamNetwork(b *StickyWriter, n Network, depth int) error {
	o := openObject(b, depth)
	for _, s := range []struct{ key, value string }{
		{"name", n.Name},
		{"notes", n.Notes},
		{"folder", n.Folder},
	} {
		if s.value == "" {
			continue
		}
		if err := o.value(s.key, s.value); err != nil {
			return err
		}
	}
	if n.Hidden {
		if err := o.value("hidden", true); err != nil {
			return err
		}
	}
	if len(n.Nodes) > 0 {
		if err := o.key("nodes"); err != nil {
			return err
		}
		keys := make([]string, 0, len(n.Nodes))
		for k := range n.Nodes {
			keys = append(keys, k)
		}
		err := streamKeyedObject(b, o.depth+1, keys, func(k string) (any, error) { return positionWire(n.Nodes[k]) })
		if err != nil {
			return err
		}
	}
	if len(n.Arcs) > 0 {
		if err := o.key("arcs"); err != nil {
			return err
		}
		keys := make([]string, 0, len(n.Arcs))
		for k := range n.Arcs {
			keys = append(keys, k)
		}
		err := streamKeyedObject(b, o.depth+1, keys, func(k string) (any, error) { return arcWire(n.Arcs[k], true) })
		if err != nil {
			return err
		}
	}
	if len(n.Chains) > 0 {
		if err := o.key("chains"); err != nil {
			return err
		}
		err := streamArray(b, o.depth+1, len(n.Chains), func(i int) (any, error) { return chainWire(n.Chains[i]) })
		if err != nil {
			return err
		}
	}
	if len(n.Points) > 0 {
		if err := o.key("points"); err != nil {
			return err
		}
		err := streamArray(b, o.depth+1, len(n.Points), func(i int) (any, error) { return waypointWire(n.Points[i], true) })
		if err != nil {
			return err
		}
	}
	if len(n.Areas) > 0 {
		if err := o.key("areas"); err != nil {
			return err
		}
		err := streamArray(b, o.depth+1, len(n.Areas), func(i int) (any, error) { return areaWire(n.Areas[i], true) })
		if err != nil {
			return err
		}
	}
	if len(n.Data) > 0 {
		recs, err := dataBodies(n.Data)
		if err != nil {
			return err
		}
		if err := o.key("data"); err != nil {
			return err
		}
		if err := streamArray(b, o.depth+1, len(recs), func(i int) (any, error) { return recs[i], nil }); err != nil {
			return err
		}
	}
	o.close()
	return b.Err()
}
