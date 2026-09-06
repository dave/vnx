package vnx

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"testing"
)

// The determinism test (spec 4b §5.2, plan T2).
//
// This is the instrument for the one hazard the streaming decomposition does
// NOT remove. Two hand-rolled orderings survive it — the document's own key
// order, and Network.Nodes / Network.Arcs (both map[string]…, plan §0) — and a
// streaming encoder that emits map entries in iteration order produces
// DIFFERENT BYTES ON EVERY RUN, with no error and no panic. It is
// non-determinism, not breakage, and only a byte comparison catches it.
//
// ⚠ THE TWO ARMS ARE DIFFERENT EXPERIMENTS AND `-count=20` IS NEITHER.
// Go's map hash seed is drawn once per PROCESS, so twenty in-process encodes
// share one seed and cannot see a seed-dependent bug; `-count=20` is still one
// process. The cross-process arm re-executes this test binary twenty times, so
// it draws twenty seeds. Both are needed:
//
//   - in-process catches per-iteration randomisation (Go also picks a random
//     start bucket on every `range` over a map, so an unsorted writer diverges
//     within one process too);
//   - cross-process catches anything that is stable within a seed and varies
//     between them — which is exactly what a golden captured on one machine
//     would bless forever.
//
// The cross-process arm carries its own liveness assertion: the children report
// a raw map iteration order alongside the digest, and the parent FAILS if all
// twenty orders agree. Twenty identical digests from twenty identical seeds
// would prove nothing, and that failure mode is invisible without the check.

const determinismChildEnv = "VNX_DETERMINISM_CHILD"

// determinismDocument is the subject of both arms.
//
// ⚠ ACCEPTANCE: it must carry a network with at least 8 nodes and 8 arcs. A
// one-entry map is trivially ordered and would make this whole file vacuous —
// there is exactly one permutation of one key, so an unsorted writer passes.
func determinismDocument(t *testing.T) *Document {
	t.Helper()
	doc := oracleEverything()
	if len(doc.Networks) == 0 {
		t.Fatal("the determinism document has no network; map ordering is then untested")
	}
	n := doc.Networks[0]
	if len(n.Nodes) < 8 || len(n.Arcs) < 8 {
		t.Fatalf("network has %d nodes and %d arcs, need at least 8 of each — "+
			"a small map is trivially ordered and this test would be vacuous",
			len(n.Nodes), len(n.Arcs))
	}
	if len(doc.Rulesets) == 0 {
		t.Fatal("the determinism document has no rulesets block; the verbatim map path is then untested")
	}
	return doc
}

// Arm one: twenty encodes, one process, one map seed.
func TestEncodeIsDeterministicWithinOneProcess(t *testing.T) {
	doc := determinismDocument(t)
	first, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 20; i++ {
		got, err := Encode(doc)
		if err != nil {
			t.Fatalf("encode %d: %v", i, err)
		}
		if !bytes.Equal(got, first) {
			d := firstDiff(got, first)
			t.Fatalf("encode %d differs from encode 0 at byte %d — map iteration order is reaching the output\n got: %s\nfirst: %s",
				i, d, excerpt(got, d), excerpt(first, d))
		}
	}
}

// Arm two: twenty processes, twenty map seeds.
func TestEncodeIsDeterministicAcrossProcesses(t *testing.T) {
	if os.Getenv(determinismChildEnv) != "" {
		t.Skip("this is the child process")
	}
	doc := determinismDocument(t)
	want, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := fmt.Sprintf("%x", sha256.Sum256(want))

	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("cannot find this test binary to re-execute it: %v", err)
	}

	const runs = 20
	orders := map[string]int{}
	for i := 0; i < runs; i++ {
		cmd := exec.Command(exe, "-test.run=^TestDeterminismChildReportsItsEncoding$", "-test.v=true")
		cmd.Env = append(os.Environ(), determinismChildEnv+"=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("child %d failed: %v\n%s", i, err, out)
		}
		digest, order := parseChildReport(t, i, out)
		if digest != wantDigest {
			t.Fatalf("child %d encoded to %s, the parent to %s — the output depends on the map seed",
				i, digest, wantDigest)
		}
		orders[order]++
	}

	// The liveness assertion. If every child drew the same iteration order then
	// the arm never varied the thing it exists to vary, and twenty agreeing
	// digests are worth nothing.
	if len(orders) < 2 {
		t.Fatalf("all %d children reported the SAME map iteration order (%v) — "+
			"the seed did not vary, so this arm proved nothing", runs, orders)
	}
	t.Logf("%d runs, %d distinct map iteration orders, one digest: %s", runs, len(orders), wantDigest)
}

func parseChildReport(t *testing.T, i int, out []byte) (digest, order string) {
	t.Helper()
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "vnx-digest: "); ok {
			digest = v
		}
		if v, ok := strings.CutPrefix(line, "map-order: "); ok {
			order = v
		}
	}
	if digest == "" || order == "" {
		t.Fatalf("child %d reported no digest or no map order — the probe did not run:\n%s", i, out)
	}
	return digest, order
}

// TestDeterminismChildReportsItsEncoding is the child half of arm two. It is a
// helper process, not a test in its own right, and skips unless the parent
// asked for it.
func TestDeterminismChildReportsItsEncoding(t *testing.T) {
	if os.Getenv(determinismChildEnv) == "" {
		t.Skip("helper process for TestEncodeIsDeterministicAcrossProcesses")
	}
	doc := determinismDocument(t)
	out, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("vnx-digest: %x\n", sha256.Sum256(out))

	// The seed probe: a RAW range over a fresh map of the same shape the
	// document carries, unsorted, so the parent can see whether this process
	// drew a different hash seed from its siblings.
	n := oracleNetwork()
	keys := make([]string, 0, len(n.Nodes))
	for k := range n.Nodes {
		keys = append(keys, k)
	}
	fmt.Printf("map-order: %s\n", strings.Join(keys, ","))

	// And the same keys sorted, so a reader of a failure can tell an unstable
	// probe from an unstable encoder.
	sorted := append([]string(nil), keys...)
	sort.Strings(sorted)
	fmt.Printf("map-order-sorted: %s\n", strings.Join(sorted, ","))
}
