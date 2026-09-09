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
/// Taking the cosine of each segment's own start latitude instead reads the
/// corpus's north-south fixtures 8.5% further away; on an east-west arc the
/// two agree to within 0.06%, which is why the corpus fixtures run
/// north-south.
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

    /// The two corpus fixtures, which are the only cases that exercise the
    /// tolerance at any distance. Both arcs run north-south over three
    /// degrees so that the cosine REFERENCE decides the answer.
    #[test]
    fn the_corpus_fixtures_land_where_their_names_say() {
        let arc = [p(-3.0, 57.0), p(-3.0, 60.0)];

        let within = distance_to_polyline(&p(-2.99919305, 59.9), &arc).expect("a distance");
        assert!(
            (within - 45.0).abs() < 0.5,
            "accept/inline-endpoint-within-tolerance is {within} m, not about 45 m"
        );
        assert!(within <= TOLERANCE_M);

        let beyond = distance_to_polyline(&p(-2.999013728, 59.9), &arc).expect("a distance");
        assert!(
            (beyond - 55.0).abs() < 0.5,
            "diagnose/inline-endpoint-beyond-tolerance is {beyond} m, not about 55 m"
        );
        assert!(beyond > TOLERANCE_M);
    }

    /// The wrong cosine reference, measured.
    ///
    /// SPEC 3.3.3 takes the cosine of the QUERY POINT's latitude and holds it
    /// constant. A model that instead takes each segment's start latitude
    /// reads the corpus's two north-south fixtures 8.6% further away — and
    /// this test records that the corpus pair does NOT, in fact, discriminate
    /// between the two models, because neither fixture crosses the 50 m
    /// threshold when it is inflated by 8.6%:
    ///
    /// ```text
    ///                  SPEC 3.3.3   segment-start cosine   verdict changes?
    ///   accept, 45 m      45.0 m           48.9 m                 no
    ///   diagnose, 55 m    55.0 m           59.7 m                 no
    /// ```
    ///
    /// A fixture that DID discriminate would sit between 50/1.086 = 46.1 m and
    /// 50 m — 48 m, say — where the correct model accepts and the wrong one
    /// diagnoses. Reported to the corpus's owner; this test is the measurement
    /// rather than a guard, and it goes red if either number moves.
    #[test]
    fn the_wrong_cosine_reference_does_not_change_either_corpus_verdict() {
        let arc = [p(-3.0, 57.0), p(-3.0, 60.0)];

        // The wrong model, written out: the cosine of the segment's START.
        let wrong = |query: &Position| {
            let cos = 57.0_f64.to_radians().cos();
            let x = |lon: f64| lon.to_radians() * EARTH_RADIUS_M * cos;
            (x(query.lon()) - x(-3.0)).abs()
        };

        for (name, query, correct_m) in [
            ("accept/inline-endpoint-within-tolerance", p(-2.99919305, 59.9), 45.0),
            ("diagnose/inline-endpoint-beyond-tolerance", p(-2.999013728, 59.9), 55.0),
        ] {
            let correct = distance_to_polyline(&query, &arc).expect("a distance");
            let inflated = wrong(&query);
            assert!((correct - correct_m).abs() < 0.5, "{name}: {correct} m");
            assert!(
                (inflated / correct - 1.086).abs() < 0.005,
                "{name}: the wrong model inflates by {}",
                inflated / correct
            );
            assert_eq!(
                correct > TOLERANCE_M,
                inflated > TOLERANCE_M,
                "{name}: the two models DO disagree here — the corpus gap this \
                 test records has been closed, so revisit the report"
            );
        }
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
