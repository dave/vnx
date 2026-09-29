import Foundation

/// The first rule a walk breaks (SPEC 3.3.3), named by pass and by key.
///
/// `key` is nil where the diagnostic names the pass itself, and otherwise the
/// pass key it names. SPEC 6.3's second rule decides which: a key the rule for
/// `from` and `to` forbids names that key, and a key it requires names the
/// pass that lacks it.
struct WalkFault: Error, Equatable {
  let pass: Int
  let key: String?
  let cause: Cause
  let message: String
}

/// Carries the node a walk is at from one pass to the next.
///
/// ⚠ A PASS CANNOT BE CHECKED FROM ITS OWN KEYS. Its entry is where the
/// previous pass left, and where the previous pass left depends on whether it
/// had a `to` and a `turn`. So the cursor is threaded through the walk in
/// order, and one stepping serves the reader's check and the writer's check —
/// two callers, one answer.
struct WalkCursor {
  /// The index of the last pass.
  let last: Int
  /// A route's walk runs node to node (SPEC 3.4).
  let route: Bool
  /// The node the walk is at. Nil before the first pass.
  private(set) var at: String?

  init(last: Int, route: Bool) {
    self.last = last
    self.route = route
  }

  /// Checks pass `i` against the rules of SPEC 3.3.3 that span more than the
  /// pass's own values — valid-walk rules 2 to 6, and a route's rule — in
  /// SPEC 6.2's order (steps 5 to 8), and moves the cursor to where the pass
  /// leaves. Returns the pass's entry and exit.
  mutating func step(_ i: Int, _ p: Pass, arc: Arc) throws -> (entry: Endpoint, exit: Endpoint) {
    func fault(_ key: String?, _ cause: Cause, _ message: String) -> WalkFault {
      WalkFault(pass: i, key: key, cause: cause, message: message)
    }
    // 5. In a route: no position, no turn and no pin, in the key order
    // `from`, `to`, `turn`, `pinned`.
    if route {
      if case .position = p.from {
        throw fault(
          "from", .routePinOrPosition,
          "a route's walk runs node to node, and this `from` is a position")
      }
      if case .position = p.to {
        throw fault(
          "to", .routePinOrPosition, "a route's walk runs node to node, and this `to` is a position"
        )
      }
      if p.turn != nil {
        throw fault("turn", .routePinOrPosition, "a route's walk carries no turn")
      }
      if p.pinned {
        throw fault("pinned", .routePinOrPosition, "a route's walk carries no pin")
      }
    }
    // 6. A turn already fixes a pass.
    if p.turn != nil && p.pinned {
      throw fault(nil, .pinnedTurn, "a pass with a turn is never pinned: the turn already fixes it")
    }
    // 7. The rule for `from` and `to`: first `from`, then `to`.
    if i == 0 && p.from == nil {
      throw fault(nil, .walkSpelling, "the first pass must carry `from`")
    }
    if i > 0 && p.from != nil {
      throw fault("from", .walkSpelling, "only the first pass carries `from`")
    }
    var fromIsPosition = false
    if case .position = p.from { fromIsPosition = true }
    let wantsTo = i == last || fromIsPosition
    if wantsTo && p.to == nil {
      throw fault(
        nil, .walkSpelling,
        i == last
          ? "the last pass must carry `to`"
          : "a pass whose `from` is a position must carry `to`")
    }
    if !wantsTo && p.to != nil {
      throw fault(
        "to", .walkSpelling,
        "`to` appears only on the last pass, or on a pass whose `from` is a position")
    }
    // 8. The walk is connected.
    // Node keys compare by Unicode scalar, as every key in this format does:
    // Swift's `String ==` is canonical equivalence, which would make two
    // different keys spelled NFC and NFD one node.
    func isEnd(_ node: String?) -> Bool {
      guard let node else { return false }
      return JSONKey(node) == JSONKey(arc.from) || JSONKey(node) == JSONKey(arc.to)
    }
    let entry: Endpoint
    if let from = p.from {
      if case .node(let node) = from, !isEnd(node) {
        throw fault(
          "from", .walkNotConnected, "`from` node \"\(node)\" is not an end of arc \"\(p.arc)\"")
      }
      entry = from
    } else {
      guard let node = at, isEnd(node) else {
        throw fault(
          nil, .walkNotConnected,
          "the walk is at node \"\(at ?? "")\", which is not an end of arc \"\(p.arc)\"")
      }
      entry = .node(node)
    }
    let exit: Endpoint
    switch p.to {
    case .node(let node)?:
      guard isEnd(node) else {
        throw fault(
          "to", .walkNotConnected, "`to` node \"\(node)\" is not an end of arc \"\(p.arc)\"")
      }
      exit = .node(node)
    case .position(let position)?:
      guard i == last else {
        throw fault("to", .walkNotConnected, "only the last pass leaves at a position")
      }
      exit = .position(position)
    case nil:
      // No `to`, so the entry is a node: a position `from` requires a
      // `to`, which the spelling check above has already enforced.
      guard case .node(let node) = entry else {
        preconditionFailure("a pass entering at a position with no `to` passed the spelling check")
      }
      if p.turn != nil {
        // Out to the turn and back: it leaves by the node it entered by.
        exit = .node(node)
      } else {
        // By the other end. No arc is a self-loop, so that is one node.
        exit = .node(JSONKey(node) == JSONKey(arc.from) ? arc.to : arc.from)
      }
    }
    if case .node(let node) = exit { at = node } else { at = nil }
    return (entry, exit)
  }
}
