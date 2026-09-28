package vnx

import (
	"fmt"
	"io"
	"sort"
)

// The framing layer.
//
// ⚠ IT IS PUNCTUATION AND NOTHING ELSE. Every VALUE below — every string, every
// number, every key, every item — goes through encoding/json. What this file
// writes is braces, brackets and commas, so that a document can be emitted
// without ever holding more than one item at a time. There is no string
// escaping, no number formatting and no key sorting here, and none may be
// added: a second answer to any of those is how two encoders in one library
// start disagreeing.
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
	// The one byte of whitespace the document carries, and it is a file
	// convention rather than layout: a text file ends in a newline.
	b.WriteString("\n")
	return b.Err()
}

// object writes one JSON object, a member at a time.
type object struct {
	b       *StickyWriter
	members int
}

func openObject(b *StickyWriter) *object {
	b.WriteString("{")
	return &object{b: b}
}

// key writes a member's name and separator, leaving the caller to write the
// value.
func (o *object) key(k string) error {
	if o.members > 0 {
		o.b.WriteString(",")
	}
	o.members++
	// The key goes through the same renderer as a value, so a string is escaped
	// one way in this package rather than two.
	quoted, err := marshalCompact(k)
	if err != nil {
		return err
	}
	o.b.WriteBytes(quoted)
	o.b.WriteString(":")
	return o.b.Err()
}

// value writes a whole member, rendering v with encoding/json.
func (o *object) value(k string, v any) error {
	if err := o.key(k); err != nil {
		return err
	}
	out, err := marshalCompact(v)
	if err != nil {
		return err
	}
	o.b.WriteBytes(out)
	return o.b.Err()
}

func (o *object) close() { o.b.WriteString("}") }

// streamArray writes an array of values, building ONE at a time and dropping it
// before the next.
func streamArray(b *StickyWriter, n int, item func(i int) (any, error)) error {
	b.WriteString("[")
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		v, err := item(i)
		if err != nil {
			return err
		}
		out, err := marshalCompact(v)
		if err != nil {
			return err
		}
		b.WriteBytes(out)
	}
	b.WriteString("]")
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
func streamKeyedObject(b *StickyWriter, keys []string, value func(k string) (any, error)) error {
	sort.Strings(keys)
	o := openObject(b)
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
	o := openObject(b)
	if err := o.value("vnx", version); err != nil {
		return err
	}
	// An empty header field is OMITTED. Writing "" would turn "not known" into
	// a value. `created` is written verbatim, and a caller-built value outside
	// §4.3's grammar is an error rather than a document the format forbids.
	if doc.Created != "" {
		if err := doc.Created.Valid(); err != nil {
			return fmt.Errorf("created: %w", err)
		}
	}
	for _, h := range []struct{ key, value string }{
		{"generator", doc.Generator},
		{"created", string(doc.Created)},
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
		if err := c.emit(b, doc); err != nil {
			return err
		}
	}
	if err := streamAppKeys(o, doc.AppKeys); err != nil {
		return err
	}
	o.close()
	return b.Err()
}

// streamAppKeys writes the application keys (SPEC §3.9), each value handed to
// encoding/json exactly as it is held. A nil Value is written as `null`: the
// key is present, and §3.9 keeps a null a null.
//
// ⚠ A KEY WITH NO FULL STOP IS REFUSED, NOT WRITTEN. A reader would take it
// for an unrecognised key and drop it, or for one of the format's own keys and
// misread it. A key written twice is refused too: which of two equal members
// a reader keeps is its JSON parser's choice (SPEC §6.4), so the second would
// be lost or would silently win.
func streamAppKeys(o *object, keys []AppKey) error {
	seen := make(map[string]bool, len(keys))
	for _, k := range keys {
		if !IsAppKey(k.Key) {
			return fmt.Errorf("vnx: application key %q contains no full stop (SPEC §3.9)", k.Key)
		}
		if seen[k.Key] {
			return fmt.Errorf("vnx: application key %q appears twice", k.Key)
		}
		seen[k.Key] = true
		if err := o.value(k.Key, k.Value); err != nil {
			return err
		}
	}
	return nil
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
	emit  func(b *StickyWriter, doc *Document) error
}

// In the order [Document] declares them, which is the order they are written.
var documentCollections = []documentCollection{
	{"Folders", "folders", func(d *Document) int { return len(d.Folders) },
		func(b *StickyWriter, d *Document) error {
			return streamArray(b, len(d.Folders), func(i int) (any, error) { return folderWire(d.Folders[i]) })
		}},
	{"Waypoints", "waypoints", func(d *Document) int { return len(d.Waypoints) },
		func(b *StickyWriter, d *Document) error {
			return streamArray(b, len(d.Waypoints), func(i int) (any, error) { return waypointWire(d.Waypoints[i], false) })
		}},
	{"Areas", "areas", func(d *Document) int { return len(d.Areas) },
		func(b *StickyWriter, d *Document) error {
			return streamArray(b, len(d.Areas), func(i int) (any, error) { return areaWire(d.Areas[i], false) })
		}},
	{"Tracks", "tracks", func(d *Document) int { return len(d.Tracks) },
		func(b *StickyWriter, d *Document) error {
			return streamArray(b, len(d.Tracks), func(i int) (any, error) { return trackWire(d.Tracks[i]) })
		}},
	{"Routes", "routes", func(d *Document) int { return len(d.Routes) },
		func(b *StickyWriter, d *Document) error {
			return streamArray(b, len(d.Routes), func(i int) (any, error) { return routeWire(d.Routes[i]) })
		}},
	{"Networks", "networks", func(d *Document) int { return len(d.Networks) },
		func(b *StickyWriter, d *Document) error {
			return streamNetworks(b, d.Networks)
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

func streamNetworks(b *StickyWriter, ns []Network) error {
	b.WriteString("[")
	for i := range ns {
		if i > 0 {
			b.WriteString(",")
		}
		if err := streamNetwork(b, ns[i]); err != nil {
			return err
		}
	}
	b.WriteString("]")
	return b.Err()
}

// streamNetwork writes one network. It is the one object with the pips inside
// it, so this is where streaming earns its keep: an arc is built, written and
// dropped before the next one starts.
//
// Members follow the order [Network] declares them.
func streamNetwork(b *StickyWriter, n Network) error {
	o := openObject(b)
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
		err := streamKeyedObject(b, keys, func(k string) (any, error) { return positionWire(n.Nodes[k]) })
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
		err := streamKeyedObject(b, keys, func(k string) (any, error) { return arcWire(n.Arcs[k], true) })
		if err != nil {
			return err
		}
	}
	if len(n.Chains) > 0 {
		if err := o.key("chains"); err != nil {
			return err
		}
		err := streamArray(b, len(n.Chains), func(i int) (any, error) { return chainWire(n.Chains[i]) })
		if err != nil {
			return err
		}
	}
	if len(n.Points) > 0 {
		if err := o.key("points"); err != nil {
			return err
		}
		err := streamArray(b, len(n.Points), func(i int) (any, error) { return waypointWire(n.Points[i], true) })
		if err != nil {
			return err
		}
	}
	if len(n.Areas) > 0 {
		if err := o.key("areas"); err != nil {
			return err
		}
		err := streamArray(b, len(n.Areas), func(i int) (any, error) { return areaWire(n.Areas[i], true) })
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
		if err := streamArray(b, len(recs), func(i int) (any, error) { return recs[i], nil }); err != nil {
			return err
		}
	}
	o.close()
	return b.Err()
}
