package vnx

import "fmt"

// TypeKey is the discriminator key on a data record.
//
// It carries a sigil, and it is the only key in the whole format that does.
// The sigil marks the key as structural rather than payload, and — the reason
// it was restored after an earlier draft dropped it — it leaves the plain name
// `type` free for any vocabulary entry that wants a field called that.
//
// The spelling is JSON-LD's, and this format is deliberately not JSON-LD. Dave
// ruled the borrowing harmless: nothing here claims to be linked data, no
// @context is emitted, and @type without @context is not valid JSON-LD anyway,
// so a processor pointed at a VNX document fails visibly rather than quietly
// misbehaving.
const TypeKey = "@type"

// ValueKey is the field name "value". VNX gives it no meaning: a record body
// is opaque apart from `@type` (SPEC §5.1), and each type chooses its own
// field names. It is kept for callers that spell the name in one place.
const ValueKey = "value"

// Record is one data record: a reverse-DNS type plus an opaque body.
//
// Body holds every key except @type, EXACTLY as it arrived. That is the
// extension mechanism's entire value — a third party's record must survive a
// read-modify-write untouched, or nobody can build on the format. It is
// preserved at VALUE level, not byte level: no language preserves number text,
// and requiring it would make the conformance corpus unpassable and, in Rust,
// trip a known serde_json defect in exactly the shape open records need.
type Record struct {
	Type string
	Body map[string]any
}

// Value reads the body's "value" field, if the record has one. VNX gives that
// name no meaning; a type chooses its own field names.
func (r Record) Value() (any, bool) {
	v, ok := r.Body["value"]
	return v, ok
}

func (r Record) String() string { return fmt.Sprintf("%s%v", r.Type, r.Body) }

// decodeRecords reads a `data` array.
//
// `data` is a BAG, not a map. Duplicates of one type are fully supported and
// ORDER IS PRESERVED, because what a reader does with several records of one
// type is the reader's business — three tags are three tags, and the format
// deliberately declines to legislate the rest. Collapsing duplicates here would
// quietly make that decision for every caller.
//
// A record whose type is unknown is INERT: no error, no diagnostic, on any host
// including one the reader did not expect. That is what lets the vocabulary
// grow without breaking readers that predate it.
func decodeRecords(arr []any, path string, diags []Diagnostic) ([]Record, []Diagnostic) {
	out := make([]Record, 0, len(arr))
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("data record is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		typ, ok := obj[TypeKey].(string)
		if !ok || typ == "" {
			// Without a discriminator nothing can dispatch on the record, so it
			// is an unusable element: dropped, reported, read continues (§6.2).
			diags = append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodeWrongTypeRequired,
				Message: fmt.Sprintf("data record has no usable %q — dropped", TypeKey)})
			continue
		}
		body := make(map[string]any, len(obj)-1)
		for k, v := range obj {
			if k != TypeKey {
				body[k] = v
			}
		}
		out = append(out, Record{Type: typ, Body: body})
	}
	return out, diags
}

// encodeRecords renders records back to a generic JSON tree. Every body key is
// written back verbatim, which is what makes the round-trip guarantee real
// rather than aspirational.
func encodeRecords(recs []Record) []any {
	out := make([]any, 0, len(recs))
	for _, r := range recs {
		obj := make(map[string]any, len(r.Body)+1)
		for k, v := range r.Body {
			obj[k] = v
		}
		obj[TypeKey] = r.Type
		out = append(out, obj)
	}
	return out
}

// dataField reads an item's optional `data` array. Absence is normal.
func dataField(obj map[string]any, path string, diags []Diagnostic) ([]Record, []Diagnostic) {
	v, present := obj["data"]
	if !present {
		return nil, diags
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, append(diags, Diagnostic{Severity: Warning, Path: path + "/data",
			Message: fmt.Sprintf("`data` is %s, expected an array — ignored", jsonKind(v))})
	}
	return decodeRecords(arr, path+"/data", diags)
}
