//! The rules of a walk (SPEC 3.3.3, 3.4), shared by the reader and the writer.
//!
//! ⚠ A PASS CANNOT BE CHECKED FROM ITS OWN KEYS. Its entry is where the
//! previous pass left, and where the previous pass left depends on whether it
//! had a `to` and a `turn`. So a [`Cursor`] is carried through the walk in
//! order, and one stepping function serves the reader's check and the
//! writer's check. Two copies of these rules would drift apart.

use crate::diag::{Cause, Path};
use crate::error::EncodeError;
use crate::model::{Arc, Endpoint, Pass, Vertex};

/// The first rule a walk breaks, and where.
#[derive(Debug, Clone, PartialEq)]
pub(crate) struct WalkFault {
    /// The index of the pass.
    pub pass: usize,
    /// The pass key the diagnostic names, or `None` when it names the pass.
    pub key: Option<&'static str>,
    pub cause: Cause,
}

impl WalkFault {
    /// The JSON Pointer the fault names, under the owner's `walk` pointer.
    pub fn path(&self, walk: &Path) -> Path {
        let at = walk.index(self.pass);
        match self.key {
            Some(key) => at.key(key),
            None => at,
        }
    }
}

/// Carries the node a walk is at from one pass to the next.
#[derive(Debug)]
pub(crate) struct Cursor {
    /// The index of the last pass.
    last: usize,
    plan: bool,
    /// The node the walk is at. Empty before the first pass, which always has
    /// a `from` once the spelling check has passed.
    at: String,
}

impl Cursor {
    /// A cursor for a walk of `len` passes. `plan` applies SPEC 3.4's extra
    /// rule: a plan's walk runs node to node, with no turn and no pin.
    pub fn new(len: usize, plan: bool) -> Self {
        Cursor {
            last: len.saturating_sub(1),
            plan,
            at: String::new(),
        }
    }

    /// Check pass `i` against the rules that span more than the pass's own
    /// values, in SPEC 6.2's order (steps 4 to 7), and move to where the pass
    /// leaves. Returns the pass's entry and exit.
    pub fn step(
        &mut self,
        i: usize,
        p: &Pass,
        arc: &Arc,
    ) -> Result<(Endpoint, Endpoint), WalkFault> {
        let fault = |key: Option<&'static str>, cause: Cause| WalkFault {
            pass: i,
            key,
            cause,
        };

        // 4. A plan's walk carries no position, turn or pin.
        if self.plan {
            let plan_fault = |key: &'static str, detail: &str| {
                fault(
                    Some(key),
                    Cause::PlanPinOrPosition {
                        detail: detail.to_string(),
                    },
                )
            };
            if matches!(p.from, Some(Endpoint::At(_))) {
                return Err(plan_fault(
                    "from",
                    "a plan's walk runs node to node, and this `from` is a position",
                ));
            }
            if matches!(p.to, Some(Endpoint::At(_))) {
                return Err(plan_fault(
                    "to",
                    "a plan's walk runs node to node, and this `to` is a position",
                ));
            }
            if p.turn.is_some() {
                return Err(plan_fault("turn", "a plan's walk carries no turn"));
            }
            if p.pin.is_some() {
                return Err(plan_fault("pin", "a plan's walk carries no pin"));
            }
        }

        // 5. A turn already fixes a pass, so it carries no pin.
        if p.turn.is_some() && p.pin.is_some() {
            return Err(fault(None, Cause::PinnedTurn));
        }

        // 6. The rule for `from` and `to`: first `from`, then `to`.
        let spelling = |key: Option<&'static str>, detail: &str| {
            fault(
                key,
                Cause::WalkSpelling {
                    detail: detail.to_string(),
                },
            )
        };
        if i == 0 && p.from.is_none() {
            return Err(spelling(None, "the first pass must carry `from`"));
        }
        if i > 0 && p.from.is_some() {
            return Err(spelling(Some("from"), "only the first pass carries `from`"));
        }
        let from_is_position = matches!(p.from, Some(Endpoint::At(_)));
        let want_to = i == self.last || from_is_position;
        if want_to && p.to.is_none() {
            return Err(spelling(
                None,
                if i == self.last {
                    "the last pass must carry `to`"
                } else {
                    "a pass whose `from` is a position must carry `to`"
                },
            ));
        }
        if !want_to && p.to.is_some() {
            return Err(spelling(
                Some("to"),
                "`to` appears only on the last pass, or on a pass whose `from` is a position",
            ));
        }

        // 7. The walk is connected.
        let not_connected = |key: Option<&'static str>, detail: String| {
            fault(key, Cause::WalkNotConnected { detail })
        };
        let is_end = |node: &str| node == arc.from || node == arc.to;
        let entry = match &p.from {
            Some(Endpoint::Node(node)) if !is_end(node) => {
                return Err(not_connected(
                    Some("from"),
                    format!("`from` node `{node}` is not an end of arc `{}`", p.arc),
                ));
            }
            Some(from) => from.clone(),
            None => {
                if !is_end(&self.at) {
                    return Err(not_connected(
                        None,
                        format!(
                            "the walk is at node `{}`, which is not an end of arc `{}`",
                            self.at, p.arc
                        ),
                    ));
                }
                Endpoint::Node(self.at.clone())
            }
        };
        let exit = match (&p.to, &p.turn, &entry) {
            (Some(Endpoint::Node(node)), _, _) if !is_end(node) => {
                return Err(not_connected(
                    Some("to"),
                    format!("`to` node `{node}` is not an end of arc `{}`", p.arc),
                ));
            }
            (Some(Endpoint::At(_)), _, _) if i != self.last => {
                return Err(not_connected(
                    Some("to"),
                    "only the last pass leaves at a position".to_string(),
                ));
            }
            (Some(to), _, _) => to.clone(),
            // A pass with a turn and no `to` leaves by the node it entered by.
            // Its entry is a node: a position `from` requires a `to`, which the
            // spelling check above has enforced.
            (None, Some(_), entry) => entry.clone(),
            (None, None, Endpoint::Node(node)) if *node == arc.from => {
                Endpoint::Node(arc.to.clone())
            }
            (None, None, _) => Endpoint::Node(arc.from.clone()),
        };
        // Only the last pass can leave at a position, so a pass after this one
        // always finds a node here.
        if let Endpoint::Node(node) = &exit {
            self.at = node.clone();
        }
        Ok((entry, exit))
    }
}

/// The writer's check on a walk: every pass names an arc of its owner, and the
/// walk follows SPEC 3.3.3 (and, for a plan, SPEC 3.4). A writer never emits a
/// walk its own reader would drop.
pub(crate) fn check_walk(
    walk: &[Pass],
    arcs: &Option<Vec<(String, Arc)>>,
    plan: bool,
    walk_path: &Path,
    owner: &str,
) -> Result<(), EncodeError> {
    let mut cursor = Cursor::new(walk.len(), plan);
    for (i, pass) in walk.iter().enumerate() {
        let arc = arcs
            .iter()
            .flatten()
            .find(|(k, _)| *k == pass.arc)
            .map(|(_, a)| a);
        let fault = match arc {
            None => WalkFault {
                pass: i,
                key: None,
                cause: Cause::PassArcAbsent {
                    detail: format!("the pass names arc `{}`, which is not here", pass.arc),
                },
            },
            Some(arc) => match cursor.step(i, pass, arc) {
                Ok(_) => continue,
                Err(fault) => fault,
            },
        };
        return Err(EncodeError::Walk {
            owner: owner.to_string(),
            path: fault.path(walk_path).as_str().to_string(),
            cause: fault.cause,
        });
    }
    Ok(())
}

/// The writer's check on an owner's arcs: each joins two different nodes, and
/// both are in the owner's `nodes` (SPEC 3.3.2).
pub(crate) fn check_arcs(
    nodes: &Option<Vec<(String, Vertex)>>,
    arcs: &Option<Vec<(String, Arc)>>,
    arcs_path: &Path,
) -> Result<(), EncodeError> {
    let known = |key: &str| nodes.iter().flatten().any(|(k, _)| k == key);
    for (key, arc) in arcs.iter().flatten() {
        let refuse = |detail: String| {
            Err(EncodeError::Arc {
                path: arcs_path.key(key).as_str().to_string(),
                detail,
            })
        };
        if arc.from == arc.to {
            return refuse(format!(
                "the arc runs from node `{}` to itself, and a self-loop cannot be written",
                arc.from
            ));
        }
        for node in [&arc.from, &arc.to] {
            if !known(node) {
                return refuse(format!(
                    "the arc names node `{node}`, which is not in its owner's nodes"
                ));
            }
        }
    }
    Ok(())
}
