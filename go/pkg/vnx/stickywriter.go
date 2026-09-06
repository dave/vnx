package vnx

import "io"

// StickyWriter latches the first write error and turns every later write into
// a no-op, so a streaming writer can stay readable without checking a return
// on every fragment and can make ONE check at the end.
//
// ⚠ IT IS NOT A CONVENIENCE. Until spec 4b the canonical writers wrote to a
// *bytes.Buffer, whose Write cannot fail, so ignoring the returns was safe.
// They now write to an io.Writer, and on a FILE an ignored short write means
// the producer returns nil, the sink Syncs, sets `committed`, and renames a
// TRUNCATED file into place as a successful export. Data corruption presenting
// as a clean write. Every writer that uses this therefore ends by returning
// [StickyWriter.Err].
//
// ⚠ IT IS EXPORTED, AND THAT IS A DELIBERATE ADDITION TO A PACKAGE THAT SHIPS
// PUBLICLY (spec 5b T3, 2026-09-02). It lived in canonical.go as an unexported
// `errWriter`, which spec 5b §4.1b then told three SUBPACKAGES — pkg/vnx/gpx,
// /kml, /geojson — to reuse. They cannot see an unexported identifier, so the
// mandate did not build as written: a compile probe fails with
// `undefined: vnx.errWriter`. Exporting it once, here, is what stops three
// writer packages hand-rolling three answers to the same short-write hazard.
// canonical.go uses THIS type; there is exactly one implementation.
type StickyWriter struct {
	w   io.Writer
	err error
}

// Stick wraps w once.
//
// ⚠ AN ALREADY-WRAPPED WRITER IS RETURNED AS ITSELF. The canonical writers
// recurse and each re-entry calls Stick; a second wrapper would latch its
// errors into an object the level above never reads, so the error would be
// held by nobody and the walk would finish "clean". The same applies to a
// writer package that passes its StickyWriter into a helper.
func Stick(w io.Writer) *StickyWriter {
	if e, ok := w.(*StickyWriter); ok {
		return e
	}
	return &StickyWriter{w: w}
}

// Write is io.Writer. After the first failure it writes nothing and returns
// the latched error.
func (e *StickyWriter) Write(p []byte) (int, error) {
	if e.err != nil {
		return 0, e.err
	}
	n, err := e.w.Write(p)
	// A short write with no error is still a lost byte. io.Writer permits it
	// only alongside an error, but a wrapper that trusted that promise would
	// hand a truncated document to the sink and call it a success.
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		e.err = err
	}
	return n, err
}

// WriteString writes s. It returns nothing on purpose: the whole point of this
// type is that a caller does not check per-fragment, and a return value here
// would invite one caller in ten to check it and the other nine to look
// careless. [StickyWriter.Err] is the check.
func (e *StickyWriter) WriteString(s string) { _, _ = e.Write([]byte(s)) }

// WriteBytes writes p, with the same contract as [StickyWriter.WriteString].
func (e *StickyWriter) WriteBytes(p []byte) { _, _ = e.Write(p) }

// Err is the first error this writer met, or nil.
//
// ⚠ A WRITER MUST RETURN THIS. Returning nil while Err is non-nil is exactly
// the truncated-file-as-success failure the type exists to prevent.
func (e *StickyWriter) Err() error { return e.err }
