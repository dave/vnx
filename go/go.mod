// The Go reference implementation of VNX.
//
// ⚠ THE MODULE IS github.com/dave/vnx/go, NOT github.com/dave/vnx. It lives in
// a subdirectory because five other languages are equal citizens of this
// repository (spec §3), and a module in a subdirectory takes that directory as
// its version-tag prefix: the tags are `go/v0.1.0`, and a bare `v0.1.0` is a
// tag the go tool ignores.
//
// ⚠ THIS MODULE HAS NO DEPENDENCIES, and it must not gain one. pkg/vnx and its
// three writer packages import the standard library and nothing else — four
// TestImportsAreStdlibOnly guards read every source and test file to keep it
// that way.
module github.com/dave/vnx/go

go 1.25.6
