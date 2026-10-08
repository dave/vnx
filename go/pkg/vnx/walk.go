package vnx

import (
	"fmt"
	"sort"
)

// Vertex is a point that carries no time: a node, or an arc pip (SPEC §3.3.1,
// §3.3.2). It is a type of its own rather than a Position so that a time on a
// node or an arc pip cannot be built, and so cannot be written: the reader
// drops such a time with a diagnostic, and nothing downstream has to remember
// to.
type Vertex struct {
	Lon, Lat float64
	Ele      *float64 // nil: elevation unknown
}

// Position is the vertex as a wire position, with no time.
func (v Vertex) Position() Position {
	return Position{Lon: v.Lon, Lat: v.Lat, Ele: v.Ele}
}

// LonLat is a position in a walk: a `from`, `to` or `turn` (SPEC §3.3.3). It
// carries longitude and latitude only; its elevation is the arc line's at the
// point it identifies, so storing one would be a second copy of a fact the arc
// already holds.
type LonLat struct {
	Lon, Lat float64
}

// Position is the walk position as a wire position.
func (p LonLat) Position() Position {
	return Position{Lon: p.Lon, Lat: p.Lat}
}

func (p LonLat) String() string { return p.Position().String() }

// Arc is a line between two different nodes of its owner, a network or a
// plan. Pips are its INTERIOR points only: the line runs from the From node's
// position through Pips to the To node's (SPEC §3.3.2). An arc with no pips is
// the straight line between its nodes.
type Arc struct {
	From string
	To   string
	Pips []Vertex
	Data []Record
}

// Pass is one entry of a walk (SPEC §3.3.3): the arc it covers and where on it
// the walk enters, turns and leaves.
//
// ⚠ FROM AND TO ARE PRESENT ONLY WHERE §3.3.3's RULE PUTS THEM: on the first
// pass, the last pass, and a pass whose From is a position. Everywhere else
// the entry is the node the walk is at and the exit is derived. A nil From or
// To is that absence, not "unknown".
//
// A pass is pinned when Pin is set: the user fixed it with a handle at that
// position. The pin leaves what the pass covers unchanged.
type Pass struct {
	Arc  string
	From *Endpoint
	To   *Endpoint
	Turn *LonLat
	Pin  *LonLat
}

// Endpoint is where a pass enters or leaves its arc: either a node key or a
// position on the arc. Exactly one of the two is set.
type Endpoint struct {
	Node string  // "" when a position
	At   *LonLat // nil when a node key
}

func (e Endpoint) String() string {
	if e.At != nil {
		return e.At.String()
	}
	return e.Node
}

var passKeys = map[string]bool{"arc": true, "from": true, "to": true, "turn": true, "pin": true}

// walkFault is the first rule a walk breaks. key is "" when the diagnostic
// names the pass itself, else the pass key it names.
type walkFault struct {
	pass int
	key  string
	code string
	msg  string
}

func (f *walkFault) error() error {
	at := fmt.Sprintf("pass %d", f.pass)
	if f.key != "" {
		at += " `" + f.key + "`"
	}
	return fmt.Errorf("vnx: %s: %s (%s)", at, f.msg, f.code)
}

// walkCursor carries the node a walk is at from one pass to the next.
//
// ⚠ A PASS CANNOT BE CHECKED FROM ITS OWN KEYS. Its entry is where the
// previous pass left, and where the previous pass left depends on whether it
// had a `to` and a `turn`. So the cursor is threaded through the walk in
// order, and the same stepping serves the reader's check, the encoder's check
// and the writers' geometry — three callers, one answer.
type walkCursor struct {
	last int
	plan bool
	at   string // the node the walk is at; "" before the first pass
}

// step checks pass i against §3.3.3's rules that span more than the pass's own
// values — valid-walk rules 2 to 6 plus a plan's — in §6.2's order, and moves
// the cursor to where the pass leaves. It returns the pass's entry and exit.
func (c *walkCursor) step(i int, p Pass, arc Arc) (entry, exit Endpoint, f *walkFault) {
	fault := func(key, code, format string, args ...any) (Endpoint, Endpoint, *walkFault) {
		return Endpoint{}, Endpoint{}, &walkFault{pass: i, key: key, code: code, msg: fmt.Sprintf(format, args...)}
	}
	if c.plan {
		switch {
		case p.From != nil && p.From.At != nil:
			return fault("from", CodePlanPinOrPosition, "a plan's walk runs node to node, and this `from` is a position")
		case p.To != nil && p.To.At != nil:
			return fault("to", CodePlanPinOrPosition, "a plan's walk runs node to node, and this `to` is a position")
		case p.Turn != nil:
			return fault("turn", CodePlanPinOrPosition, "a plan's walk carries no turn")
		case p.Pin != nil:
			return fault("pin", CodePlanPinOrPosition, "a plan's walk carries no pin")
		}
	}
	if p.Turn != nil && p.Pin != nil {
		return fault("", CodePinnedTurn, "a pass with a turn carries no pin: the turn already fixes it")
	}
	// The rule for `from` and `to`: first `from`, then `to`.
	switch {
	case i == 0 && p.From == nil:
		return fault("", CodeWalkSpelling, "the first pass must carry `from`")
	case i > 0 && p.From != nil:
		return fault("from", CodeWalkSpelling, "only the first pass carries `from`")
	}
	wantTo := i == c.last || (p.From != nil && p.From.At != nil)
	switch {
	case wantTo && p.To == nil:
		if i == c.last {
			return fault("", CodeWalkSpelling, "the last pass must carry `to`")
		}
		return fault("", CodeWalkSpelling, "a pass whose `from` is a position must carry `to`")
	case !wantTo && p.To != nil:
		return fault("to", CodeWalkSpelling, "`to` appears only on the last pass, or on a pass whose `from` is a position")
	}
	// Connection.
	isEnd := func(node string) bool { return node == arc.From || node == arc.To }
	if p.From != nil {
		if p.From.At == nil && !isEnd(p.From.Node) {
			return fault("from", CodeWalkNotConnected, "`from` node %q is not an end of arc %q", p.From.Node, p.Arc)
		}
		entry = *p.From
	} else {
		if !isEnd(c.at) {
			return fault("", CodeWalkNotConnected, "the walk is at node %q, which is not an end of arc %q", c.at, p.Arc)
		}
		entry = Endpoint{Node: c.at}
	}
	switch {
	case p.To != nil && p.To.At == nil && !isEnd(p.To.Node):
		return fault("to", CodeWalkNotConnected, "`to` node %q is not an end of arc %q", p.To.Node, p.Arc)
	case p.To != nil && p.To.At != nil && i != c.last:
		return fault("to", CodeWalkNotConnected, "only the last pass leaves at a position")
	case p.To != nil:
		exit = *p.To
	case p.Turn != nil:
		// entry is a node here: a position `from` requires a `to`, which
		// the spelling check above has already enforced.
		exit = Endpoint{Node: entry.Node}
	case entry.Node == arc.From:
		exit = Endpoint{Node: arc.To}
	default:
		exit = Endpoint{Node: arc.From}
	}
	c.at = exit.Node
	return entry, exit, nil
}

// decodeWalk reads a highlight's or a plan's REQUIRED `walk` and checks it in
// §6.2's order. ok is false when the owner is to be dropped: a missing or
// wrong-typed `walk`, or the first pass that breaks a rule, which is reported
// alone — nothing after it in the walk is examined.
//
// ⚠ A WALK IS USED WHOLE OR NOT AT ALL (SPEC §6.2). Dropping a pass would hand
// the caller a different walk that reads as valid — two arcs joined that do not
// meet, a direction reversed by a lost turn, a start moved to a node — so any
// fault drops the highlight, and the diagnostic names the pass and the rule.
func decodeWalk(obj map[string]any, owner string, arcs map[string]Arc, plan bool, diags []Diagnostic) ([]Pass, bool, []Diagnostic) {
	path := owner + "/walk"
	arr, isArr := obj["walk"].([]any)
	if !isArr {
		what := "is missing"
		if _, present := obj["walk"]; present {
			what = fmt.Sprintf("is %s, expected an array", jsonKind(obj["walk"]))
		}
		return nil, false, append(diags, Diagnostic{Severity: Warning, Path: path,
			Code: CodeWrongTypeRequired, Message: fmt.Sprintf("`walk` %s — dropped", what)})
	}
	walk := make([]Pass, 0, len(arr))
	cur := walkCursor{last: len(arr) - 1, plan: plan}
	drop := func(f walkFault) ([]Pass, bool, []Diagnostic) {
		at := fmt.Sprintf("%s/%d", path, f.pass)
		if f.key != "" {
			at += "/" + f.key
		}
		return nil, false, append(diags, Diagnostic{Severity: Warning, Path: at, Code: f.code,
			Message: f.msg + " — dropped"})
	}
	for i, item := range arr {
		at := fmt.Sprintf("%s/%d", path, i)
		// 1. The pass is an object naming an arc of its owner.
		pObj, isObj := item.(map[string]any)
		if !isObj {
			return drop(walkFault{pass: i, code: CodePassArcAbsent,
				msg: fmt.Sprintf("pass is %s, expected an object", jsonKind(item))})
		}
		arcID, _ := pObj["arc"].(string)
		arc, known := arcs[arcID]
		if !known {
			return drop(walkFault{pass: i, code: CodePassArcAbsent,
				msg: fmt.Sprintf("pass names arc %v, which is not here", describeArc(pObj["arc"]))})
		}
		p := Pass{Arc: arcID}
		// 2. Unrecognised keys.
		diags = reportUnknownKeys(pObj, at, passKeys, diags)
		// 3. `from`, `to`, `turn` and `pin` each parse.
		for _, k := range []string{"from", "to", "turn", "pin"} {
			v, present := pObj[k]
			if !present {
				continue
			}
			var ep Endpoint
			onlyPosition := k == "turn" || k == "pin"
			if s, isStr := v.(string); isStr && !onlyPosition {
				ep.Node = s
			} else {
				posArr, isArr := v.([]any)
				if !isArr {
					want := "a node key or a position"
					if onlyPosition {
						want = "a position"
					}
					return drop(walkFault{pass: i, key: k, code: CodePositionUnusable,
						msg: fmt.Sprintf("`%s` is %s, expected %s", k, jsonKind(v), want)})
				}
				pos, err := decodePosition(posArr, at+"/"+k)
				if err != nil {
					return drop(walkFault{pass: i, key: k, code: CodePositionUnusable, msg: err.Error()})
				}
				if pos.Ele != nil || pos.Time != nil {
					diags = append(diags, Diagnostic{Severity: Warning, Path: at + "/" + k, Code: CodeExtraChannel,
						Message: "a walk position carries longitude and latitude only — its other channels are dropped"})
				}
				ep.At = &LonLat{Lon: pos.Lon, Lat: pos.Lat}
			}
			switch k {
			case "from":
				p.From = &ep
			case "to":
				p.To = &ep
			case "turn":
				p.Turn = ep.At
			case "pin":
				p.Pin = ep.At
			}
		}
		// 4 to 7: the plan rule, pin and turn, spelling, connection.
		if _, _, f := cur.step(i, p, arc); f != nil {
			return drop(*f)
		}
		walk = append(walk, p)
	}
	return walk, true, diags
}

// describeArc renders a pass's `arc` value for a message: a string quoted, and
// anything else by its JSON kind, so an absent key reads as absent.
func describeArc(v any) string {
	switch t := v.(type) {
	case nil:
		return "(none)"
	case string:
		return fmt.Sprintf("%q", t)
	}
	return jsonKind(v)
}

// checkWalk is the encoder's check: every pass names an arc of its owner, and
// the walk follows §3.3.3's rules. A writer never emits a walk its own reader
// would drop.
func checkWalk(walk []Pass, arcs map[string]Arc, plan bool) error {
	cur := walkCursor{last: len(walk) - 1, plan: plan}
	for i, p := range walk {
		arc, known := arcs[p.Arc]
		if !known {
			return (&walkFault{pass: i, code: CodePassArcAbsent, msg: fmt.Sprintf("pass names arc %q, which is not here", p.Arc)}).error()
		}
		for _, e := range []struct {
			key string
			ep  *Endpoint
		}{{"from", p.From}, {"to", p.To}} {
			if e.ep != nil && (e.ep.At == nil) == (e.ep.Node == "") {
				return (&walkFault{pass: i, key: e.key, code: CodePositionUnusable,
					msg: "an endpoint must be exactly one of a node key and a position"}).error()
			}
		}
		if _, _, f := cur.step(i, p, arc); f != nil {
			return f.error()
		}
	}
	return nil
}

// checkArcs is the encoder's check on an owner's arcs: two different nodes,
// both present. It names the offending arc.
func checkArcs(nodes map[string]Vertex, arcs map[string]Arc) error {
	for _, id := range sortedArcIDs(arcs) {
		a := arcs[id]
		if a.From == "" || a.To == "" {
			return fmt.Errorf("vnx: arc %q must name both its nodes, got from=%q to=%q", id, a.From, a.To)
		}
		if a.From == a.To {
			return fmt.Errorf("vnx: arc %q runs from node %q to itself, and a self-loop cannot be written", id, a.From)
		}
		for _, n := range []string{a.From, a.To} {
			if _, ok := nodes[n]; !ok {
				return fmt.Errorf("vnx: arc %q names node %q, which is not in its owner's nodes", id, n)
			}
		}
	}
	return nil
}

func sortedArcIDs(arcs map[string]Arc) []string {
	ids := make([]string, 0, len(arcs))
	for id := range arcs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
