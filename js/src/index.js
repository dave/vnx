// The VNX codec: read and write `.vnx` and `.vnz`, and report diagnostics. See
// ../README.md for what it does not do.

export { VnxRefusal, Diagnostic, DIAGNOSTIC_CODES, PINNED_CODES, REFUSAL_SUBSTRINGS } from './errors.js';
export {
  Position, Folder, Network, NetworkArc, Chain, Link, Endpoint,
  Route, RouteArc, Track, Segment, Waypoint, Area, Document,
} from './model.js';
export { decodeText, decodeValue, indexPositions, READER_MAJOR, READER_MINOR } from './decode.js';
export { encode, emitDocument } from './emit.js';
export { distanceToPolylineM, PROJECTION_TOLERANCE_M, EARTH_RADIUS_M } from './geo.js';
export { decodeContainer, encodeContainer, writeContainer } from './vnz.js';
