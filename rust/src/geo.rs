//! The one piece of arithmetic in the format (SPEC 3.3.3).
//!
//! An inline chain endpoint must lie on its arc, within 50 m. SPEC 3.3.3
//! states the distance model rather than leaving it to an implementation,
//! because the choice decides which documents are legal: four plausible models
//! read one endpoint of the worked example at 19.81 m, 20.34 m, 25.52 m and
//! 36.47 m, and Web Mercator — the largest of the four — is the one an
//! implementation carrying a tile pipeline reaches for first.

use crate::model::Position;

/// The Earth radius SPEC 3.3.3 names.
const EARTH_RADIUS_M: f64 = 6_371_008.8;

/// The projection tolerance, in metres.
pub const TOLERANCE_M: f64 = 50.0;

/// The least distance in metres from `point` to the polyline `pips`.
///
/// Every coordinate is projected by a local equirectangular map about the
/// latitude of the QUERY POINT, held constant for every vertex. That keeps the
/// map affine, so a straight polyline segment stays straight and the plane
/// geometry is exact.
///
/// Taking the cosine of each segment's own start latitude instead is the
/// mistake the corpus's two tolerance fixtures exist to catch. Against the
/// pair at HEAD — meridian arcs spanning thirteen degrees, running in opposite
/// directions — that model reads 67.4 m where this one reads 45, and 36.8 m
/// where this one reads 55. It therefore refuses a legal document in the first
/// file and accepts an illegal one in the second.
///
/// Those numbers are not restated in any test here. `tools/redproof.py`
/// mutates this function to the wrong cosine and requires the LIVE corpus to
/// go red on exactly those two cases, so the claim is measured against the
/// fixtures rather than against a copy of their coordinates. An earlier
/// version of this file hard-coded a superseded three-degree pair and asserted,
/// green, a statement that was false of the corpus it named.
///
/// `None` when the polyline has no vertices: SPEC 3.3.3 requires a reader to
/// record a diagnostic and drop the link rather than invent a number.
pub fn distance_to_polyline(point: &Position, pips: &[Position]) -> Option<f64> {
    if pips.is_empty() {
        return None;
    }
    let cos_lat = point.lat().to_radians().cos();
    let project = |p: &Position| -> (f64, f64) {
        (
            p.lon().to_radians() * EARTH_RADIUS_M * cos_lat,
            p.lat().to_radians() * EARTH_RADIUS_M,
        )
    };
    let q = project(point);

    // A polyline of one vertex is that point.
    if pips.len() == 1 {
        let v = project(&pips[0]);
        return Some(hypot(q.0 - v.0, q.1 - v.1));
    }

    let mut best = f64::INFINITY;
    for pair in pips.windows(2) {
        let a = project(&pair[0]);
        let b = project(&pair[1]);
        let d = point_to_segment(q, a, b);
        if d < best {
            best = d;
        }
    }
    Some(best)
}

/// The distance from `q` to the nearest point ON the segment `a`–`b`, not to
/// the infinite line through them.
fn point_to_segment(q: (f64, f64), a: (f64, f64), b: (f64, f64)) -> f64 {
    let (dx, dy) = (b.0 - a.0, b.1 - a.1);
    let length_squared = dx * dx + dy * dy;
    if length_squared == 0.0 {
        // A zero-length segment is its endpoint. Repeated vertices are
        // ordinary in imported data.
        return hypot(q.0 - a.0, q.1 - a.1);
    }
    let t = (((q.0 - a.0) * dx + (q.1 - a.1) * dy) / length_squared).clamp(0.0, 1.0);
    hypot(q.0 - (a.0 + t * dx), q.1 - (a.1 + t * dy))
}

fn hypot(x: f64, y: f64) -> f64 {
    // `f64::hypot` avoids the intermediate overflow that squaring can cause.
    x.hypot(y)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn p(lon: f64, lat: f64) -> Position {
        Position::lon_lat(lon, lat)
    }

    /// SPEC 3.3.3's tolerance INCLUDES its boundary: "within that tolerance"
    /// accepts exactly 50.000 m. Ruled 2026-09-09.
    ///
    /// No fixture can ever pin this. A distance at exactly the threshold is
    /// not reachable by a corpus case, because the computed value depends on
    /// the model and no two models land on 50.000 exactly. The prose is the
    /// only statement of it, permanently, so this unit test is the only guard.
    #[test]
    fn exactly_the_tolerance_is_within_it() {
        // Constructed, not measured: a query point due east of a meridian arc
        // at the latitude the projection uses, so the distance is the x term
        // alone and can be solved for exactly 50 m.
        let lat = 60.0_f64;
        let metres_per_degree = EARTH_RADIUS_M * std::f64::consts::PI / 180.0 * lat.to_radians().cos();
        let arc = [p(-3.0, 55.0), p(-3.0, 65.0)];

        let at = |m: f64| p(-3.0 + m / metres_per_degree, lat);
        let just_inside = distance_to_polyline(&at(49.9), &arc).expect("a distance");
        let on_it = distance_to_polyline(&at(50.0), &arc).expect("a distance");
        let just_outside = distance_to_polyline(&at(50.1), &arc).expect("a distance");

        assert!((on_it - 50.0).abs() < 1e-6, "the constructed point is {on_it} m out");
        assert!(just_inside <= TOLERANCE_M);
        assert!(
            on_it <= TOLERANCE_M,
            "exactly the tolerance must be accepted, and {on_it} was refused"
        );
        assert!(just_outside > TOLERANCE_M);
    }

    #[test]
    fn the_worked_examples_inline_endpoint_is_about_20_m_out() {
        // corpus/accept/example-network.vnx, chain 0 link 1, on arc-2.
        let arc = [
            p(-3.6605, 57.1042),
            p(-3.654, 57.1009),
            p(-3.6488, 57.0977),
        ];
        let d = distance_to_polyline(&p(-3.6521, 57.1), &arc).expect("a distance");
        assert!((d - 19.81).abs() < 1.0, "{d} m");
    }

    #[test]
    fn a_polyline_of_one_vertex_is_that_point_and_of_none_has_no_distance() {
        assert!(distance_to_polyline(&p(-3.0, 57.0), &[]).is_none());
        let d = distance_to_polyline(&p(-3.0, 57.0), &[p(-3.0, 57.0)]).expect("a distance");
        assert_eq!(d, 0.0);
    }

    /// The nearest point is on the segment, not on the infinite line through
    /// it. Off the end of an arc, the answer is the distance to the endpoint.
    #[test]
    fn distance_is_to_the_segment_not_to_its_line() {
        let arc = [p(-3.0, 57.0), p(-3.0, 57.001)];
        let far_past_the_end = distance_to_polyline(&p(-3.0, 58.0), &arc).expect("a distance");
        assert!(
            far_past_the_end > 100_000.0,
            "{far_past_the_end} m — the query projected onto the extended line"
        );
    }
}
