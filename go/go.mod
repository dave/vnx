// The Go reference implementation of VNX.
//
// ⚠ THE MODULE IS github.com/dave/vnx/go, NOT github.com/dave/vnx. It lives in
// a subdirectory because five other languages are equal citizens of this
// repository (spec §3), and a module in a subdirectory takes that directory as
// its version-tag prefix: the tags are `go/v0.1.0`, and a bare `v0.1.0` is a
// tag the go tool ignores.
//
// ⚠ THE SHIPPED LIBRARY HAS NO DEPENDENCIES, and it must not gain one. pkg/vnx
// and its three writer packages import the standard library and nothing else —
// four TestImportsAreStdlibOnly guards read every source and test file to keep
// it that way. pkg/detmath (math functions that give the same bits on every
// architecture) is stdlib-only too, under its own guard.
//
// The require block below is ONE package's, and that package is internal/. A
// JSON Schema validator is third-party, and internal/vnxschema is the only
// thing in this module that uses it: being under internal/ it is unimportable
// from outside the module, and under module graph pruning jsonschema never
// enters a consumer's build, so anyone depending on github.com/dave/vnx/go
// still gets a dependency-free library. It is a package rather than a nested
// module so `go test ./...` keeps running it; a nested module is how a check
// like this quietly stops being run.
//
// TestTheSchemaValidatorIsTheOnlyThirdPartyImport, in that package, is what
// keeps the exemption from widening — it reads every .go file in the module
// and fails if a third-party import appears anywhere else.
module github.com/dave/vnx/go

go 1.25.6

require github.com/santhosh-tekuri/jsonschema/v6 v6.0.3

require golang.org/x/text v0.14.0 // indirect
