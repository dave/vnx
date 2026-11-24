package merge

import "fmt"

// apiError wraps err for the public API: "merge: <op>: <err>". The exported
// functions add it once, at the boundary; the errors from inside the merge
// carry no prefix. It is a separate function so the small wrappers stay
// inlinable.
func apiError(op string, err error) error {
	return fmt.Errorf("merge: %s: %w", op, err)
}
