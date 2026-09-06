// The corpus is language-neutral — six implementations run these same files —
// and this go.mod does not change that. It exists for one reason: tools/makevnz
// is a Go program, and outside a module the go tool will not run it at all.
//
// Until 2026-09-06 the corpus lived inside the Viatrail module, so
// `go run ./tools/makevnz` worked by inheritance and nobody had to think about
// it. At the repository root it inherits nothing, and the failure would have
// been the quiet kind: the README's rebuild instruction stops working, the
// .vnz archives stop being reproducible, and nothing goes red because no test
// runs the generator.
//
// It sits here rather than in tools/ so the README's command keeps working
// unchanged — makevnz writes its output relative to the working directory, and
// that directory has to be the corpus root.
//
// Stdlib only, and nothing should ever be added to it.
module github.com/dave/vnx/corpus

go 1.25.6
