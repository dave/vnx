// The VNX codec: read and write `.vnx` and `.vnz`, and report diagnostics. See
// ../README.md for what it does not do.

export { VnxRefusal, Diagnostic, DIAGNOSTIC_CODES, PINNED_CODES, REFUSAL_SUBSTRINGS } from './errors.js';
export {
  Position,
  Folder,
  Network,
  Arc,
  Chain,
  Pass,
  Endpoint,
  Route,
  Track,
  Segment,
  Waypoint,
  Area,
  Document,
  AppKey,
} from './model.js';
export { decodeText, decodeValue, isAppKey, indexPositions, READER_MAJOR, READER_MINOR } from './decode.js';
export { encode, emitDocument } from './emit.js';
export { nearestOnLine, EARTH_RADIUS_M } from './geo.js';
export { walkLegs, walkLine, arcLine } from './walk.js';
export { decodeContainer, encodeContainer, writeContainer } from './vnz.js';
