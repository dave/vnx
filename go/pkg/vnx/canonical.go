package vnx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Canonical re-renders a VNX document in canonical form (spec §4.5).
//
// Canonical form is NOT required of a document — a reader must accept any
// well-formed VNX. It is what a conforming formatter emits, and what the
// conformance corpus is stored in. Its purpose is byte-stability: without it,
// two exports of the same library differ, diffs become unreadable, and an agent
// editing a document cannot tell its own change from formatting churn.
//
// The rules, all of §4.5:
//
//   - Object keys sort in two groups — scalars lexicographically, then
//     containers lexicographically — classified by the value in THIS object.
//     No key is named by the rule, so nothing needs maintaining as the format
//     grows, and identity lands above payload at every level because identity
//     is nearly always scalar and payload nearly always a container.
//   - Arrays keep their order. It is meaningful everywhere it appears — pips,
//     links, a route's arcs, segments — and must never be reordered.
//   - Numbers render with ECMAScript Number::toString semantics.
//   - UTF-8, no BOM, two-space indent, newline at end of file.
func Canonical(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // keep the original text so we can re-render it ourselves
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrMalformed, err)
	}
	var b bytes.Buffer
	if err := writeCanonical(&b, tree, 0); err != nil {
		return nil, err
	}
	b.WriteByte('\n')
	return b.Bytes(), nil
}

// writeCanonical renders one value.
//
// ⚠ IT MUST HANDLE NATIVE Go NUMBERS, NOT ONLY json.Number. Before spec 4b
// every value reaching here had been through json.Marshal and back, which
// turned every number into a json.Number; the per-item trees the streaming
// encoder builds hold `float64`, `int64` (Position.Time, position.go:29) and
// `int` (PointStyle.IconSize / .Importance, item.go:17,21) natively. Without
// the cases below the first timed pip fails with "cannot canonicalise int64",
// and an icon size with "cannot canonicalise int".
//
// The native cases route through writeNumber via the number's decimal TEXT,
// which is byte-for-byte what json.Marshal produced on the old path — so the
// negative-zero rule, the ECMAScript rendering and even the float64 rounding
// of an int64 beyond 2^53 are all reproduced rather than reimplemented.
//
// ⚠ `default:` STAYS A LOUD ERROR. Any other type in a tree is a bug in the
// builder above it. Widening this to "marshal whatever arrives" would convert
// a caught mistake into silently wrong bytes in a published format.
func writeCanonical(w io.Writer, v any, depth int) error {
	b := Stick(w)
	switch t := v.(type) {
	case map[string]any:
		return writeObject(b, t, depth)
	case []any:
		return writeArray(b, t, depth)
	case nil:
		b.WriteString("null")
	case bool:
		if t {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		b.WriteString(canonicalString(t))
	case json.Number:
		return writeNumber(b, t)
	case float64:
		return writeNumber(b, json.Number(fmt.Sprint(t)))
	case int64:
		return writeNumber(b, json.Number(strconv.FormatInt(t, 10)))
	case int:
		return writeNumber(b, json.Number(strconv.Itoa(t)))
	default:
		return fmt.Errorf("vnx: cannot canonicalise %T", v)
	}
	return b.err
}

// writeNumber renders with ECMAScript semantics, which is what Go's
// encoding/json emits for a float64 and what JSON.stringify emits in a browser.
//
// ⚠ PYTHON IS THE OUTLIER, measured 2026-08-31: `json.dumps` gives 340.0,
// 1e-06 and 1e-07 where Go and JS give 340, 0.000001 and 1e-7. The Python
// reference implementation therefore needs a number-formatting shim, and the
// corpus carries all four cases. An earlier draft of the spec claimed the
// defaults agreed; they do not.
func writeNumber(w io.Writer, n json.Number) error {
	b := Stick(w)
	f, err := n.Float64()
	if err != nil {
		return fmt.Errorf("vnx: %q is not a number: %w", n, err)
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Errorf("vnx: non-finite numbers are not permitted (%s)", n)
	}
	// ⚠ NEGATIVE ZERO RENDERS AS `0`, and Go does not do this for us.
	// `json.Marshal(math.Copysign(0, -1))` emits `-0`; ECMAScript's
	// Number::toString — which §4.5 pins the format to — emits `0`, and
	// `JSON.stringify(-0)` agrees. This is the ONE case where Go's encoder
	// diverges from the reference, and it matters for exactly the reason the
	// canonical form exists: -0 and 0 are the same value, so two writers
	// disagreeing about the sign is byte-incompatibility bought for no
	// information at all. Found 2026-09-01 by writing a corpus case for number
	// rendering, not by reading the encoder.
	if f == 0 {
		f = 0
	}
	out, err := json.Marshal(f)
	if err != nil {
		return err
	}
	b.WriteBytes(out)
	return b.err
}

func writeObject(w io.Writer, obj map[string]any, depth int) error {
	b := Stick(w)
	if len(obj) == 0 {
		b.WriteString("{}")
		return b.err
	}
	var scalars, containers []string
	for k, v := range obj {
		if isContainer(v) {
			containers = append(containers, k)
		} else {
			scalars = append(scalars, k)
		}
	}
	sort.Strings(scalars)
	sort.Strings(containers)

	b.WriteString("{\n")
	inner := depth + 1
	for i, k := range append(scalars, containers...) {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(indent(inner))
		b.WriteString(canonicalString(k))
		b.WriteString(": ")
		if err := writeCanonical(b, obj[k], inner); err != nil {
			return err
		}
	}
	b.WriteString("\n" + indent(depth) + "}")
	return b.err
}

func writeArray(w io.Writer, arr []any, depth int) error {
	b := Stick(w)
	if len(arr) == 0 {
		b.WriteString("[]")
		return b.err
	}
	// An array holding only numbers and nulls goes on ONE line.
	//
	// That is every position in the format — [lon, lat, ele, time] — and one
	// position per line rather than six is the difference between a day's track
	// being 10,000 lines and being 50,000. Measured on the seeded library: 1,277
	// lines and 29,073 bytes becomes 642 lines and 20,948.
	//
	// The rule names no key, exactly like the ordering rule above it, and
	// classifies purely by what the value holds. So a number array inside an
	// opaque `data` record gets the same treatment without this code having to
	// know anything about the record — which is the point, since it is not
	// allowed to.
	if allScalarNumbers(arr) {
		return writeInlineArray(b, arr)
	}
	b.WriteString("[\n")
	inner := depth + 1
	for i, v := range arr {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString(indent(inner))
		if err := writeCanonical(b, v, inner); err != nil {
			return err
		}
	}
	b.WriteString("\n" + indent(depth) + "]")
	return b.err
}

// allScalarNumbers reports whether every element is a number or null — the
// shape of a position, and of nothing else this format defines.
//
// `null` counts, and has to: an interior absence is spelled null, and a
// position that lost its inline rendering the moment an elevation went missing
// would make the file's shape depend on its data.
// ⚠ THE NATIVE NUMBER TYPES ARE LISTED FOR THE SAME REASON writeCanonical
// LISTS THEM. A json.Number-only test made every position built by a per-item
// tree fall to the multi-line branch — still valid JSON, and a diff the size of
// the file. The set here must stay in step with writeCanonical's number cases;
// TestInlineArrayRuleCoversEveryNumberTypeTheWriterAccepts asserts it does.
func allScalarNumbers(arr []any) bool {
	for _, v := range arr {
		switch v.(type) {
		case json.Number, float64, int64, int, nil:
		default:
			return false
		}
	}
	return true
}

func writeInlineArray(w io.Writer, arr []any) error {
	b := Stick(w)
	b.WriteString("[")
	for i, v := range arr {
		if i > 0 {
			b.WriteString(", ")
		}
		if err := writeCanonical(b, v, 0); err != nil {
			return err
		}
	}
	b.WriteString("]")
	return b.err
}

// isContainer classifies by the VALUE in this object, not by the key name —
// which is what lets the ordering rule name no keys at all.
func isContainer(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

func indent(depth int) string { return strings.Repeat("  ", depth) }
