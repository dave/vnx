// The Go reference implementation of VNX.
//
// ⚠ THE MODULE IS github.com/dave/vnx/go, NOT github.com/dave/vnx. It lives in
// a subdirectory because five other languages are equal citizens of this
// repository (spec §3), and a module in a subdirectory takes that directory as
// its version-tag prefix: the tags are `go/v0.1.0`, and a bare `v0.1.0` is a
// tag the go tool ignores.
//
// ⚠ THE REQUIRE BLOCK IS NOT A LIBRARY DEPENDENCY, and it must not become one.
// pkg/vnx and its three writer packages import the standard library and nothing
// else — four TestImportsAreStdlibOnly guards read every source and test file
// to keep it that way. jsonschema/v6 is reached only by internal/vnxschema, a
// test-only package that validates the JSON Schema against the corpus. Being
// under internal/ it is unimportable from outside, and under module graph
// pruning it never enters a consumer's build. It is here rather than in a
// module of its own so `go test ./...` still runs it; a nested module is how a
// check like that quietly stops running.
module github.com/dave/vnx/go

go 1.25.6

require github.com/santhosh-tekuri/jsonschema/v6 v6.0.3

require golang.org/x/text v0.30.0 // indirect
