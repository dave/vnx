//! DEFLATE decompression ([RFC 1951]), written out because the crate takes no
//! dependencies.
//!
//! SPEC 7 allows a VNZ entry to be stored or deflated, and every entry in the
//! corpus's four archives is deflated, so a reader without this cannot open a
//! container at all.
//!
//! Decompression only. This crate writes containers with the stored method,
//! which SPEC 7 permits, so no compressor is needed.

/// Why a deflate stream would not decompress.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct InflateError(pub String);

impl std::fmt::Display for InflateError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "the deflate stream is corrupt: {}", self.0)
    }
}

fn err<T>(what: &str) -> Result<T, InflateError> {
    Err(InflateError(what.to_string()))
}

/// The most this will reserve up front, however large a size the archive
/// declares.
const MAX_PREALLOCATION: usize = 1 << 20;

/// How much to reserve before any byte has been decompressed.
///
/// **A declared size is an assertion by whoever wrote the file, not a fact.** A
/// twenty-byte archive may claim two gigabytes, and reserving that much on its
/// word is a denial of service that arrives as an allocation failure rather than
/// as a refusal — in Rust an abort, which no `Result` and no `catch_unwind`ing
/// caller can intercept, and which `#![forbid(unsafe_code)]` does nothing about.
/// The Kotlin seat hit exactly this as an `OutOfMemoryError` escaping every
/// handler it had.
///
/// So the declared size is a HINT here and a BOUND in the decompression loop:
/// the output grows as bytes actually arrive, and stops the moment it exceeds
/// what the archive claimed.
fn initial_capacity(expected: usize) -> usize {
    expected.min(MAX_PREALLOCATION)
}

/// Decompress a raw deflate stream. `expected` is the uncompressed size the
/// archive declared, used as an allocation hint and as a bound: a stream that
/// produces more than it declared is corrupt, and treating it as valid is how
/// a decompression bomb gets its room (SPEC 9).
pub fn inflate(input: &[u8], expected: usize) -> Result<Vec<u8>, InflateError> {
    let mut bits = BitReader::new(input);
    let mut out: Vec<u8> = Vec::with_capacity(initial_capacity(expected));

    loop {
        let is_final = bits.take(1)? == 1;
        match bits.take(2)? {
            0 => stored_block(&mut bits, &mut out)?,
            1 => {
                let (lengths, distances) = fixed_tables();
                huffman_block(&mut bits, &mut out, &lengths, &distances, expected)?;
            }
            2 => {
                let (lengths, distances) = dynamic_tables(&mut bits)?;
                huffman_block(&mut bits, &mut out, &lengths, &distances, expected)?;
            }
            _ => return err("reserved block type 3"),
        }
        if out.len() > expected {
            return err("the stream produced more bytes than the archive declared");
        }
        if is_final {
            break;
        }
    }
    if out.len() != expected {
        return Err(InflateError(format!(
            "the stream produced {} bytes and the archive declared {expected}",
            out.len()
        )));
    }
    Ok(out)
}

// ---------------------------------------------------------------------------

struct BitReader<'a> {
    input: &'a [u8],
    /// Byte position of the next byte to load.
    at: usize,
    /// Bits held but not yet consumed, low bit first.
    hold: u64,
    count: u32,
}

impl<'a> BitReader<'a> {
    fn new(input: &'a [u8]) -> Self {
        BitReader {
            input,
            at: 0,
            hold: 0,
            count: 0,
        }
    }

    /// Read `n` bits, least-significant bit first, as RFC 1951 section 3.1.1
    /// requires for everything except a Huffman code.
    fn take(&mut self, n: u32) -> Result<u32, InflateError> {
        debug_assert!(n <= 32);
        while self.count < n {
            let Some(byte) = self.input.get(self.at) else {
                return err("the stream ended mid-symbol");
            };
            self.hold |= (*byte as u64) << self.count;
            self.at += 1;
            self.count += 8;
        }
        let value = (self.hold & ((1u64 << n) - 1)) as u32;
        self.hold >>= n;
        self.count -= n;
        Ok(value)
    }

    /// Drop the rest of the current byte, as a stored block requires.
    fn align(&mut self) {
        let drop = self.count % 8;
        self.hold >>= drop;
        self.count -= drop;
    }

    fn take_byte(&mut self) -> Result<u8, InflateError> {
        Ok(self.take(8)? as u8)
    }
}

fn stored_block(bits: &mut BitReader<'_>, out: &mut Vec<u8>) -> Result<(), InflateError> {
    bits.align();
    let len = bits.take(16)? as usize;
    let nlen = bits.take(16)? as usize;
    if len != (!nlen) & 0xffff {
        return err("a stored block's length and its complement disagree");
    }
    for _ in 0..len {
        out.push(bits.take_byte()?);
    }
    Ok(())
}

/// A canonical Huffman decoding table, held as the counts-and-symbols form
/// RFC 1951 section 3.2.2 describes.
struct Huffman {
    /// `counts[n]` is how many codes have length `n`.
    counts: [u16; 16],
    /// Symbols ordered by code length, then by symbol value.
    symbols: Vec<u16>,
}

impl Huffman {
    fn new(lengths: &[u8]) -> Result<Huffman, InflateError> {
        let mut counts = [0u16; 16];
        for &length in lengths {
            counts[length as usize] += 1;
        }
        counts[0] = 0;

        // A code set must be complete or a single symbol; anything else is an
        // over- or under-subscribed set and the stream is corrupt.
        let mut left = 1i32;
        for count in counts.iter().skip(1) {
            left <<= 1;
            left -= *count as i32;
            if left < 0 {
                return err("an over-subscribed Huffman code set");
            }
        }

        let mut offsets = [0u16; 16];
        for n in 1..15 {
            offsets[n + 1] = offsets[n] + counts[n];
        }
        let mut symbols = vec![0u16; lengths.len()];
        for (symbol, &length) in lengths.iter().enumerate() {
            if length != 0 {
                symbols[offsets[length as usize] as usize] = symbol as u16;
                offsets[length as usize] += 1;
            }
        }
        Ok(Huffman { counts, symbols })
    }

    /// Decode one symbol. A Huffman code is packed most-significant bit first,
    /// which is the one place RFC 1951 reverses its own bit order.
    fn decode(&self, bits: &mut BitReader<'_>) -> Result<u16, InflateError> {
        let mut code = 0i32;
        let mut first = 0i32;
        let mut index = 0i32;
        for length in 1..16 {
            code |= bits.take(1)? as i32;
            let count = self.counts[length] as i32;
            if code - first < count {
                return Ok(self.symbols[(index + (code - first)) as usize]);
            }
            index += count;
            first = (first + count) << 1;
            code <<= 1;
        }
        err("a Huffman code ran past 15 bits")
    }
}

/// RFC 1951 section 3.2.6: the fixed code tables.
fn fixed_tables() -> (Huffman, Huffman) {
    let mut literal = [0u8; 288];
    for (symbol, slot) in literal.iter_mut().enumerate() {
        *slot = match symbol {
            0..=143 => 8,
            144..=255 => 9,
            256..=279 => 7,
            _ => 8,
        };
    }
    let distance = [5u8; 30];
    (
        Huffman::new(&literal).expect("the fixed literal table is well-formed"),
        Huffman::new(&distance).expect("the fixed distance table is well-formed"),
    )
}

/// RFC 1951 section 3.2.7: the code lengths are themselves Huffman-coded.
fn dynamic_tables(bits: &mut BitReader<'_>) -> Result<(Huffman, Huffman), InflateError> {
    let hlit = bits.take(5)? as usize + 257;
    let hdist = bits.take(5)? as usize + 1;
    let hclen = bits.take(4)? as usize + 4;
    if hlit > 286 || hdist > 30 {
        return err("a dynamic block declares more codes than RFC 1951 allows");
    }

    const ORDER: [usize; 19] = [
        16, 17, 18, 0, 8, 7, 9, 6, 10, 5, 11, 4, 12, 3, 13, 2, 14, 1, 15,
    ];
    let mut code_lengths = [0u8; 19];
    for &slot in ORDER.iter().take(hclen) {
        code_lengths[slot] = bits.take(3)? as u8;
    }
    let code_table = Huffman::new(&code_lengths)?;

    let mut lengths = vec![0u8; hlit + hdist];
    let mut i = 0;
    while i < lengths.len() {
        let symbol = code_table.decode(bits)?;
        match symbol {
            0..=15 => {
                lengths[i] = symbol as u8;
                i += 1;
            }
            16 => {
                if i == 0 {
                    return err("a repeat code with nothing to repeat");
                }
                let previous = lengths[i - 1];
                let repeat = 3 + bits.take(2)? as usize;
                for _ in 0..repeat {
                    if i == lengths.len() {
                        return err("a repeat code ran past the code-length list");
                    }
                    lengths[i] = previous;
                    i += 1;
                }
            }
            17 | 18 => {
                let repeat = if symbol == 17 {
                    3 + bits.take(3)? as usize
                } else {
                    11 + bits.take(7)? as usize
                };
                if i + repeat > lengths.len() {
                    return err("a zero-run ran past the code-length list");
                }
                i += repeat;
            }
            _ => return err("an unknown code-length symbol"),
        }
    }

    let literal = Huffman::new(&lengths[..hlit])?;
    let distance = Huffman::new(&lengths[hlit..])?;
    Ok((literal, distance))
}

/// RFC 1951 section 3.2.5: the length and distance base tables.
const LENGTH_BASE: [u16; 29] = [
    3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 23, 27, 31, 35, 43, 51, 59, 67, 83, 99, 115, 131,
    163, 195, 227, 258,
];
const LENGTH_EXTRA: [u32; 29] = [
    0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 1, 1, 2, 2, 2, 2, 3, 3, 3, 3, 4, 4, 4, 4, 5, 5, 5, 5, 0,
];
const DISTANCE_BASE: [u16; 30] = [
    1, 2, 3, 4, 5, 7, 9, 13, 17, 25, 33, 49, 65, 97, 129, 193, 257, 385, 513, 769, 1025, 1537,
    2049, 3073, 4097, 6145, 8193, 12289, 16385, 24577,
];
const DISTANCE_EXTRA: [u32; 30] = [
    0, 0, 0, 0, 1, 1, 2, 2, 3, 3, 4, 4, 5, 5, 6, 6, 7, 7, 8, 8, 9, 9, 10, 10, 11, 11, 12, 12, 13,
    13,
];

fn huffman_block(
    bits: &mut BitReader<'_>,
    out: &mut Vec<u8>,
    literals: &Huffman,
    distances: &Huffman,
    limit: usize,
) -> Result<(), InflateError> {
    loop {
        let symbol = literals.decode(bits)?;
        match symbol {
            0..=255 => out.push(symbol as u8),
            256 => return Ok(()),
            257..=285 => {
                let index = symbol as usize - 257;
                let length =
                    LENGTH_BASE[index] as usize + bits.take(LENGTH_EXTRA[index])? as usize;
                let distance_symbol = distances.decode(bits)? as usize;
                if distance_symbol >= 30 {
                    return err("a distance symbol outside the table");
                }
                let distance = DISTANCE_BASE[distance_symbol] as usize
                    + bits.take(DISTANCE_EXTRA[distance_symbol])? as usize;
                if distance > out.len() {
                    return err("a back-reference points before the start of the output");
                }
                // Copied one byte at a time on purpose: an overlapping copy,
                // where distance is less than length, is how RFC 1951 spells a
                // run, and a block copy would get it wrong.
                let start = out.len() - distance;
                for k in 0..length {
                    let byte = out[start + k];
                    out.push(byte);
                }
            }
            _ => return err("a literal/length symbol outside the table"),
        }
        if out.len() > limit {
            return err("the stream produced more bytes than the archive declared");
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// A stored block carrying "hello", hand-assembled: final flag 1, type 00,
    /// aligned, then LEN, ~LEN and the bytes.
    #[test]
    fn a_stored_block() {
        let mut stream = vec![0x01, 0x05, 0x00, 0xfa, 0xff];
        stream.extend_from_slice(b"hello");
        assert_eq!(inflate(&stream, 5).expect("inflates"), b"hello");
    }

    #[test]
    fn a_truncated_stream_is_an_error_not_a_short_answer() {
        let stream = [0x01, 0x05, 0x00, 0xfa, 0xff, b'h'];
        assert!(inflate(&stream, 5).is_err());
    }

    /// A declared size is never reserved on the writer's word.
    #[test]
    fn a_declared_size_is_a_hint_and_not_a_reservation() {
        assert_eq!(initial_capacity(0), 0);
        assert_eq!(initial_capacity(64), 64);
        assert_eq!(initial_capacity(MAX_PREALLOCATION), MAX_PREALLOCATION);
        // The cases that matter: a header claiming far more than it can hold.
        assert_eq!(initial_capacity(2 << 30), MAX_PREALLOCATION);
        assert_eq!(initial_capacity(usize::MAX), MAX_PREALLOCATION);
    }

    #[test]
    fn a_declared_size_that_disagrees_is_an_error() {
        let mut stream = vec![0x01, 0x05, 0x00, 0xfa, 0xff];
        stream.extend_from_slice(b"hello");
        assert!(inflate(&stream, 4).is_err());
        assert!(inflate(&stream, 6).is_err());
    }

    #[test]
    fn rubbish_does_not_panic() {
        for seed in 0u8..255 {
            let noise: Vec<u8> = (0..64).map(|i| seed.wrapping_mul(i as u8 + 31)).collect();
            let _ = inflate(&noise, 1024);
        }
    }
}
