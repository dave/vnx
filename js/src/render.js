// Canonical form (SPEC.md 4.5): a JSON value in, canonical bytes out.
//
// This is the RE-RENDER half of SPEC.md 8.1. It applies no rounding and no
// shortening; both belong to the write path (see write.js).

import { compareCodePoints } from './codepoint.js';
import { renderNumber } from './number.js';

const INDENT = '  ';

/**
 * @param {unknown} value a plain JSON value
 * @returns {string} canonical text, ending in a newline
 */
export function renderCanonical(value) {
  return renderValue(value, 0) + '\n';
}

function renderValue(v, depth) {
  if (v === null) return 'null';
  switch (typeof v) {
    case 'boolean':
      return v ? 'true' : 'false';
    case 'number':
      return renderNumber(v);
    case 'string':
      return renderString(v);
    case 'object':
      return Array.isArray(v) ? renderArray(v, depth) : renderObject(v, depth);
    default:
      throw new TypeError(`cannot render a ${typeof v}`);
  }
}

// SPEC.md 4.5.2. An array holding only numbers and `null` goes on one line;
// `null` counts as a number for this rule, so a position does not change shape
// when an elevation goes missing.
function isInlineArray(a) {
  for (const el of a) {
    if (el !== null && typeof el !== 'number') return false;
  }
  return true;
}

function renderArray(a, depth) {
  if (a.length === 0) return '[]';
  if (isInlineArray(a)) {
    return '[' + a.map((el) => (el === null ? 'null' : renderNumber(el))).join(', ') + ']';
  }
  const inner = INDENT.repeat(depth + 1);
  const parts = a.map((el) => inner + renderValue(el, depth + 1));
  return '[\n' + parts.join(',\n') + '\n' + INDENT.repeat(depth) + ']';
}

// SPEC.md 4.5.1. Two groups, each in code-point order: scalars first, then
// containers. Classification is by the value present in THIS object; no key is
// named by the rule.
function renderObject(o, depth) {
  const keys = Object.keys(o);
  if (keys.length === 0) return '{}';
  const scalars = [];
  const containers = [];
  for (const k of keys) {
    const v = o[k];
    (v !== null && typeof v === 'object' ? containers : scalars).push(k);
  }
  scalars.sort(compareCodePoints);
  containers.sort(compareCodePoints);
  const ordered = scalars.concat(containers);
  const inner = INDENT.repeat(depth + 1);
  const parts = ordered.map(
    (k) => inner + renderString(k) + ': ' + renderValue(o[k], depth + 1),
  );
  return '{\n' + parts.join(',\n') + '\n' + INDENT.repeat(depth) + '}';
}

// SPEC.md 4.5.5. Escape only what RFC 8259 requires: `"`, `\`, and the control
// characters below U+0020. Everything else is written raw, including non-ASCII,
// `<`, `>`, `&`, U+2028 and U+2029.
const SHORT_ESCAPES = {
  '\b': '\\b',
  '\t': '\\t',
  '\n': '\\n',
  '\f': '\\f',
  '\r': '\\r',
  '"': '\\"',
  '\\': '\\\\',
};

export function renderString(s) {
  let out = '"';
  for (const ch of s) {
    const short = SHORT_ESCAPES[ch];
    if (short !== undefined) {
      out += short;
    } else if (ch < ' ') {
      out += '\\u' + ch.codePointAt(0).toString(16).padStart(4, '0');
    } else {
      out += ch;
    }
  }
  return out + '"';
}
