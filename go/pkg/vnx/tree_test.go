package vnx

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// JSON-tree comparison, which is what the corpus compares now that the bytes of
// a document are not constrained (spec §8.1).
//
// ⚠ IT IS NEVER MODEL-VERSUS-MODEL, and that is the finding the tier rests on.
// A reader that discarded every data record, every application key, every style
// and every `notes` produces a model that compares equal to itself, and passes an
// accept tier built that way green — that is the whole extensibility mechanism,
// all styling and all user prose. Comparing the tree parsed from the ORIGINAL
// FILE against the tree parsed from encode(decode(file)) is what proves a reader
// kept what it read. accept_tier_test.go freezes the proof.

// treeOptions says which normalisations a comparison applies.
//
// ⚠ THE TWO TIERS NORMALISE DIFFERENTLY, AND MUST. The accept tier compares a
// document against a re-encoding of itself, so rounding and the shortest
// spelling of a position are applied to both sides — otherwise it would require
// the file to have been written by this library. The write tier compares one
// writer's output against another's, where rounding and shortening are the very
// rules under test: normalising them there would make the tier prove nothing.
type treeOptions struct {
	// emitBoundary applies §4.1's writer rules — rounding, and dropping a
	// trailing `null` channel — to both sides.
	emitBoundary bool
}

// parseTree parses JSON to a generic tree. Numbers land as float64, so the
// comparison is between values rather than between spellings, which is what
// §4.2 makes the round-trip guarantee.
func parseTree(data []byte) (any, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return v, nil
}

// normaliseDocument walks a decoded VNX document and applies the permitted
// normalisations.
//
// It is SCHEMA-AWARE rather than a heuristic over shapes, because the two
// regions the format declares opaque — a data record's body and a style — may
// hold arrays of numbers that are not positions and must not be rounded, and
// may hold empty containers that a third party means to keep.
func normaliseDocument(v any, opt treeOptions) any {
	obj, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := copyObject(obj)
	normItems(out, "folders", opt, normFolder)
	normItems(out, "waypoints", opt, normWaypoint)
	normItems(out, "areas", opt, normArea)
	normItems(out, "tracks", opt, normTrack)
	normItems(out, "routes", opt, normRoute)
	normItems(out, "networks", opt, normNetwork)
	// Application keys are not touched: SPEC §3.9 exempts them from §8.1's
	// absent-equals-empty normalisation, so an empty value is compared as
	// written and a key dropped for holding one is a difference.
	return out
}

func normFolder(o map[string]any, opt treeOptions) {
	dropEmptyContainer(o, "data")
}

func normWaypoint(o map[string]any, opt treeOptions) {
	if p, ok := o["at"]; ok {
		o["at"] = normPosition(p, opt)
	}
	dropEmptyContainer(o, "style")
	dropEmptyContainer(o, "data")
}

func normArea(o map[string]any, opt treeOptions) {
	normPositionList(o, "polygon", opt)
	dropEmptyContainer(o, "style")
	dropEmptyContainer(o, "data")
}

func normTrack(o map[string]any, opt treeOptions) {
	normItems(o, "segments", opt, func(s map[string]any, opt treeOptions) {
		normPositionList(s, "pips", opt)
	})
	dropEmptyContainer(o, "style")
	dropEmptyContainer(o, "data")
}

func normRoute(o map[string]any, opt treeOptions) {
	normGraph(o, opt)
	normWalk(o, opt)
	dropEmptyContainer(o, "style")
	dropEmptyContainer(o, "data")
}

func normArc(o map[string]any, opt treeOptions) {
	normPositionList(o, "pips", opt)
	dropEmptyContainer(o, "data")
}

// normGraph normalises the `nodes` and `arcs` a network and a route share
// (SPEC §3.4: a route owns a graph of the network's shape).
func normGraph(o map[string]any, opt treeOptions) {
	if nodes, ok := o["nodes"].(map[string]any); ok {
		out := copyObject(nodes)
		for k, v := range out {
			out[k] = normPosition(v, opt)
		}
		o["nodes"] = out
	}
	if arcs, ok := o["arcs"].(map[string]any); ok {
		out := copyObject(arcs)
		for k, v := range out {
			if a, ok := v.(map[string]any); ok {
				a = copyObject(a)
				normArc(a, opt)
				out[k] = a
			}
		}
		o["arcs"] = out
	}
	dropEmptyContainer(o, "nodes")
	dropEmptyContainer(o, "arcs")
}

// normWalk normalises a walk's positions. `from` and `to` are a node key or a
// position (§3.3.3); only the second is a position, and normPosition leaves a
// string alone. `turn` is always a position.
//
// ⚠ AN EMPTY WALK IS KEPT: `walk` is REQUIRED (§3.10), so `"walk": []` and an
// absent walk are different documents, and folding one into the other would
// hide a writer that drops the key.
func normWalk(o map[string]any, opt treeOptions) {
	arr, ok := o["walk"].([]any)
	if !ok {
		return
	}
	out := make([]any, 0, len(arr))
	for _, item := range arr {
		p, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		p = copyObject(p)
		for _, k := range []string{"from", "to", "turn"} {
			if v, ok := p[k]; ok {
				p[k] = normPosition(v, opt)
			}
		}
		out = append(out, p)
	}
	o["walk"] = out
}

func normNetwork(o map[string]any, opt treeOptions) {
	normGraph(o, opt)
	normItems(o, "highlights", opt, normHighlight)
	normItems(o, "points", opt, normWaypoint)
	normItems(o, "areas", opt, normArea)
	dropEmptyContainer(o, "data")
}

func normHighlight(o map[string]any, opt treeOptions) {
	normWalk(o, opt)
	dropEmptyContainer(o, "style")
	dropEmptyContainer(o, "data")
}

// normItems normalises an array of objects under `key`, and drops the key when
// the array is empty.
func normItems(o map[string]any, key string, opt treeOptions, each func(map[string]any, treeOptions)) {
	arr, ok := o[key].([]any)
	if !ok {
		return
	}
	if len(arr) == 0 {
		delete(o, key)
		return
	}
	out := make([]any, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		obj = copyObject(obj)
		each(obj, opt)
		out = append(out, obj)
	}
	o[key] = out
}

func normPositionList(o map[string]any, key string, opt treeOptions) {
	arr, ok := o[key].([]any)
	if !ok {
		return
	}
	if len(arr) == 0 {
		delete(o, key)
		return
	}
	out := make([]any, 0, len(arr))
	for _, p := range arr {
		out = append(out, normPosition(p, opt))
	}
	o[key] = out
}

// normPosition applies what §8.1 lets a comparison ignore about a position:
// rounding, and a trailing `null`. Both are emit-boundary rules and are applied
// only for the accept tier; see treeOptions.
//
// ⚠ A CHANNEL THIS VERSION DOES NOT NAME IS NOT NORMALISED AWAY, and no corpus
// file carries one. §4.1's preservation rule is gone, so a fixture asking a
// writer to reproduce a fifth channel would be asking for a rule the format
// does not state — the fixture is what changes, not the comparison.
func normPosition(v any, opt treeOptions) any {
	arr, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, 0, len(arr))
	for i, c := range arr {
		n, isNumber := c.(float64)
		if !isNumber || !opt.emitBoundary {
			out = append(out, c)
			continue
		}
		switch i {
		case 0, 1:
			out = append(out, RoundCoordinate(n))
		case 2:
			out = append(out, RoundElevation(n))
		default:
			out = append(out, n)
		}
	}
	if opt.emitBoundary {
		for len(out) > 2 && out[len(out)-1] == nil {
			out = out[:len(out)-1]
		}
	}
	return out
}

// dropEmptyContainer applies §3.1: an absent collection and an empty one carry
// the same content, and a reader MUST treat them alike. So the comparison does
// too, and `omitempty` stays conforming in every language that has it.
func dropEmptyContainer(o map[string]any, key string) {
	switch v := o[key].(type) {
	case []any:
		if len(v) == 0 {
			delete(o, key)
		}
	case map[string]any:
		if len(v) == 0 {
			delete(o, key)
		}
	}
}

func copyObject(o map[string]any) map[string]any {
	out := make(map[string]any, len(o))
	for k, v := range o {
		out[k] = v
	}
	return out
}

// treeDiff returns the first place two trees differ, in JSON Pointer form, or
// "" when they are equal.
//
// It reports a PATH rather than a boolean because a failing corpus case has to
// say which rule broke to be actionable.
func treeDiff(got, want any, path string) string {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Sprintf("%s: got %s, want an object", pathOrRoot(path), describe(got))
		}
		keys := make([]string, 0, len(w))
		for k := range w {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			gv, present := g[k]
			if !present {
				return fmt.Sprintf("%s: key %q is missing", pathOrRoot(path), k)
			}
			if d := treeDiff(gv, w[k], path+"/"+escapePointer(k)); d != "" {
				return d
			}
		}
		extra := make([]string, 0)
		for k := range g {
			if _, present := w[k]; !present {
				extra = append(extra, k)
			}
		}
		sort.Strings(extra)
		if len(extra) > 0 {
			return fmt.Sprintf("%s: unexpected key(s) %v", pathOrRoot(path), extra)
		}
	case []any:
		g, ok := got.([]any)
		if !ok {
			return fmt.Sprintf("%s: got %s, want an array", pathOrRoot(path), describe(got))
		}
		if len(g) != len(w) {
			return fmt.Sprintf("%s: got %d element(s), want %d", pathOrRoot(path), len(g), len(w))
		}
		for i := range w {
			if d := treeDiff(g[i], w[i], fmt.Sprintf("%s/%d", path, i)); d != "" {
				return d
			}
		}
	case float64:
		g, ok := got.(float64)
		// A number compares by VALUE. -0 and 0 are the same value and compare
		// equal here, exactly as they do in Go.
		if !ok || g != w {
			if !ok && math.IsNaN(w) {
				return fmt.Sprintf("%s: want is NaN, which is not a JSON value", pathOrRoot(path))
			}
			return fmt.Sprintf("%s: got %v, want %v", pathOrRoot(path), got, w)
		}
	default:
		if got != want {
			return fmt.Sprintf("%s: got %v, want %v", pathOrRoot(path), describe(got), describe(want))
		}
	}
	return ""
}

func pathOrRoot(p string) string {
	if p == "" {
		return "the document"
	}
	return p
}

func escapePointer(k string) string {
	return strings.ReplaceAll(strings.ReplaceAll(k, "~", "~0"), "/", "~1")
}

func describe(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return fmt.Sprintf("%q", t)
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	}
	return fmt.Sprintf("%v", v)
}

// compareDocuments parses two documents and returns the first difference
// between their normalised trees.
func compareDocuments(got, want []byte, opt treeOptions) (string, error) {
	g, err := parseTree(got)
	if err != nil {
		return "", fmt.Errorf("the encoded document is not JSON: %w", err)
	}
	w, err := parseTree(want)
	if err != nil {
		return "", fmt.Errorf("the expected document is not JSON: %w", err)
	}
	return treeDiff(normaliseDocument(g, opt), normaliseDocument(w, opt), ""), nil
}
