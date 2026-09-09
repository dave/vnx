import Foundation

/// The projection model of SPEC 3.3.3, which decides which documents are legal.
///
/// Every coordinate — the inline position and every vertex of the arc — is
/// projected by a local equirectangular map about the LATITUDE OF THE INLINE
/// POSITION. One latitude for every coordinate, which keeps the map affine, so
/// a straight polyline segment stays straight and the plane geometry is exact.
///
/// SPEC 3.3.3 states the model rather than leaving it to arithmetic taste
/// because four plausible models read one endpoint of the worked example at
/// 19.81 m, 20.34 m, 25.52 m and 36.47 m. Taking the cosine of each SEGMENT's
/// own start latitude is the near miss, and it is wrong in both directions:
/// `corpus/accept/inline-endpoint-within-tolerance.vnx` is a legal document it
/// refuses, and `corpus/diagnose/inline-endpoint-beyond-tolerance.vnx` is an
/// illegal one it accepts.
enum Projection {
    static let earthRadiusMetres = 6371008.8
    /// SPEC 3.3.3. Hand-authored and third-party coordinates are typed rather
    /// than computed; an export from an existing polyline projects to well
    /// under a metre.
    static let toleranceMetres = 50.0

    /// SPEC 3.3.3: "A position exactly 50 metres from its arc is within
    /// tolerance and is accepted."
    ///
    /// No conformance case can pin this boundary and none should be attempted —
    /// a distance at exactly the threshold is not reachable by a fixture,
    /// because the computed value depends on the model and no two models land
    /// on 50.000. The prose is the permanent statement of the rule, and
    /// `LibraryTests.testTheToleranceBoundaryIsInclusive` is the only place it
    /// can be checked.
    static func isWithinTolerance(_ distanceMetres: Double) -> Bool {
        distanceMetres <= toleranceMetres
    }

    /// The least distance in metres from `point` to the polyline, or nil where
    /// the polyline has no vertices — which has no distance, and which SPEC
    /// 3.3.3 says a reader must diagnose rather than invent a number for.
    static func distance(from point: Position, toPolyline polyline: [Position]) -> Double? {
        if polyline.isEmpty { return nil }
        let cosLat = cos(point.lat * .pi / 180)
        let scaleX = .pi / 180 * earthRadiusMetres * cosLat
        let scaleY = .pi / 180 * earthRadiusMetres

        func project(_ p: Position) -> (x: Double, y: Double) {
            (p.lon * scaleX, p.lat * scaleY)
        }

        let q = project(point)
        if polyline.count == 1 {
            // "A polyline of one vertex is that point."
            let a = project(polyline[0])
            return hypot(q.x - a.x, q.y - a.y)
        }
        var best = Double.infinity
        var a = project(polyline[0])
        for i in 1..<polyline.count {
            let b = project(polyline[i])
            best = min(best, distanceToSegment(q, a, b))
            a = b
        }
        return best
    }

    private static func distanceToSegment(
        _ p: (x: Double, y: Double),
        _ a: (x: Double, y: Double),
        _ b: (x: Double, y: Double)
    ) -> Double {
        let dx = b.x - a.x, dy = b.y - a.y
        let lengthSquared = dx * dx + dy * dy
        if lengthSquared == 0 { return hypot(p.x - a.x, p.y - a.y) }
        var t = ((p.x - a.x) * dx + (p.y - a.y) * dy) / lengthSquared
        t = min(max(t, 0), 1)
        return hypot(p.x - (a.x + t * dx), p.y - (a.y + t * dy))
    }
}
