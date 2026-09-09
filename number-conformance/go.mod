// This module exists for one reason: the generator below is a Go program, and
// outside a module the go tool will not run it at all. Same reasoning as
// corpus/go.mod.
//
// The GENERATED FILE is the artefact. It is language-neutral, and running this
// generator is not a prerequisite for using it — which is the whole point, and
// the reason the table is committed rather than computed at test time.
//
// Stdlib only, and nothing should ever be added to it.
module github.com/dave/vnx/number-conformance

go 1.21
