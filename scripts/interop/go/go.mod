// A module of its own, the same bargain corpus/go.mod makes: the go tool will
// not run a program that is in no module, and this driver must not become a
// command inside github.com/dave/vnx/go. That module ships a dependency-free
// library whose in-module import set is enumerated by two tests, and a driver
// nobody consumes has no business inside it.
//
// The `replace` points at the library in this same checkout, so the matrix
// measures the working tree rather than a published version.
module github.com/dave/vnx/scripts/interop

go 1.25.6

require github.com/dave/vnx/go v0.0.0

replace github.com/dave/vnx/go => ../../../go
