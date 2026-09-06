package vnx

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"testing"
	"time"
)

// The memory harness (spec 4b §5.4, plan T3, reused by T11).
//
// It reports PEAK HEAP ÷ OUTPUT BYTES at three pip counts. A single number at
// one size proves nothing — the current implementation would also produce a
// number — so the property is the ratio, measured at 20k, 200k and 2M.
//
// ⚠ TWO PEAKS, AND REPORTING ONE OF THEM IS HOW THIS WORK LOOKS LIKE A SUCCESS
// OR A FAILURE SPURIOUSLY. They differ by about 34%:
//
//   - peak LIVE SET — what survives a forced runtime.GC() at the sample. This
//     is the property the streaming design actually changes. Baseline 80 MB at
//     200k pips.
//   - peak HeapAlloc under default GOGC — what a real process pays, and what
//     max RSS confirms. Baseline 121 MB at 200k pips.
//
// ⚠ GOGC IS NOT THE FIX AND MUST NOT BE USED AS ONE. GOGC=10 cuts the default
// peak to 83.8 MB with no code change at all, and costs ~30% encode time. An
// arm that quietly runs under a different GOGC than its comparator measures the
// collector rather than the change, so the harness PINS it and prints what it
// pinned.
//
// ⚠ RUN IN A FRESH `git worktree add --detach`, never the shared tree. A
// shared-tree measurement has been wrong here before by two orders of
// magnitude — another lane's uncommitted edit is compiled into your binary and
// nothing says so.
//
// It is gated on VNX_MEMORY because 2M pips costs well over a gigabyte and
// several minutes. The gate is an env var rather than a build tag deliberately:
// the file still COMPILES on every run, so it cannot rot unnoticed.
//
// The time column exists because nothing else in the plan would catch a
// rewrite that is materially slower, and the spec treats time as decisive — it
// rejects GOGC=10 partly for costing ~30%.

const memoryGateEnv = "VNX_MEMORY"

// memSubject is what gets measured. T11 adds an EncodeTo subject beside this
// one WITHOUT restructuring the harness — which is the whole reason T3 builds
// it now rather than T11 building it against a rewritten encoder.
//
// ⚠ T11: NAME THE ENTRY POINT. After T7, Encode is a bytes.Buffer wrapper over
// EncodeTo, and its peak is Document + final buffer + the buffer's growth
// transient ≈ 3× output — over the 2× bound with nothing wrong. The gate is
// EncodeTo streaming to a discarding writer; the Encode figure is reported
// beside it for honesty, not as the criterion.
type memSubject struct {
	name string
	run  func(doc *Document) (outBytes int, err error)
}

func subjectEncode() memSubject {
	return memSubject{
		name: "Encode",
		run: func(doc *Document) (int, error) {
			b, err := Encode(doc)
			return len(b), err
		},
	}
}

// subjectEncodeTo is THE GATE (T11). It streams to a writer that counts and
// discards, which is what the design actually changed: no output is retained
// anywhere, so peak is the Document plus one item's tree and nothing else.
//
// ⚠ THE Encode SUBJECT IS NOT THE GATE AND MUST NOT BE READ AS ONE. Encode is
// a bytes.Buffer over EncodeTo, so its peak is necessarily Document + the
// finished output + the buffer's growth transient during the last copy — about
// 3x output, over the 2x bound with nothing whatsoever wrong. It is measured
// and printed beside the gate for honesty, because quietly dropping the number
// that looks bad is how a measurement stops being one.
func subjectEncodeTo() memSubject {
	return memSubject{
		name: gateSubject,
		run: func(doc *Document) (int, error) {
			var c countingDiscard
			err := EncodeTo(&c, doc)
			return int(c.n), err
		},
	}
}

// gateSubject names the arm the acceptance criterion is asserted against, so
// the assertion cannot drift onto a different subject by a renamed string.
const gateSubject = "EncodeTo"

// peakRatioBound is spec 4b §5.4's acceptance: peak heap / output bytes under
// 2x, for BOTH peaks, at every size. The old materialising encoder sat at
// 9.8-10.9x (HeapAlloc) and 6.0-6.5x (live set), measured on the same box on
// 2026-09-02 at 62642d167.
const peakRatioBound = 2.0

// goHeapMinimum is the collector's minimum heap goal — 4 MiB, scaled by
// GOGC/100, which is 4 MiB at the pinned GOGC=100.
//
// ⚠ IT IS AN ALLOWANCE ON THE DEFAULT-GOGC ARM ONLY, AND IT IS HERE BECAUSE
// THE CRITERION AS WRITTEN IS UNMEASURABLE AT THE SMALLEST SIZE. Below roughly
// 2 MB of output, 2x output is less than the heap the runtime lets a program
// reach before it collects at all, so peak HeapAlloc stops being a fact about
// the encoder. Measured at 20,000 pips on 2026-09-02: output 1.3 MB, and
// EncodeTo's peak HeapAlloc 4.0-4.2 MB — which is the 4 MiB goal, not a cost
// the encoder pays. The live-set arm has no such floor and is asserted
// STRICTLY at every size.
//
// ⚠ AND THE ALLOWANCE DOES NOT MAKE THE GATE VACUOUS, WHICH WAS CHECKED RATHER
// THAN HOPED. At 20,000 pips the bound with it is 2*1.3 + 4.2 = 6.8 MB;
// EncodeTo measures 4.2 and the OLD materialising encoder measures 13.7, so the
// gate still separates them by a factor of two at the size where it is weakest.
// It is a constant, so it vanishes as output grows: at 2M pips it is 4 MB
// against a 248 MB bound.
const goHeapMinimum = 4 << 20

// countingDiscard is an io.Writer that keeps nothing. Writing to a file would
// measure the filesystem as well; writing to io.Discard would not tell us how
// many bytes came out, and an output size we did not measure is a denominator
// we would be assuming.
type countingDiscard struct{ n int64 }

func (c *countingDiscard) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

type memResult struct {
	size      int // pips
	subject   string
	outBytes  int
	docBytes  int64  // <= 0 means "smaller than this measurement can resolve"
	peakAlloc uint64 // peak HeapAlloc, default GOGC
	peakLive  uint64 // peak live set, forced GC at the sample
	encode    time.Duration
}

func TestEncodeMemoryBaseline(t *testing.T) {
	if os.Getenv(memoryGateEnv) == "" {
		t.Skipf("set %s=1 to run the memory harness (it allocates gigabytes)", memoryGateEnv)
	}

	// Pin the collector across every arm and say what was pinned. Whatever the
	// environment asked for is overridden, not respected: an inherited GOGC is
	// exactly the silent 34% this measurement cannot afford.
	const pinnedGOGC = 100
	prev := debug.SetGCPercent(pinnedGOGC)
	defer debug.SetGCPercent(prev)
	t.Logf("GOGC pinned to %d for every arm (inherited GOGC=%q, previous SetGCPercent=%d)",
		pinnedGOGC, os.Getenv("GOGC"), prev)
	t.Logf("GOMAXPROCS=%d, %s/%s, %s", runtime.GOMAXPROCS(0), runtime.GOOS, runtime.GOARCH, runtime.Version())
	t.Logf("load average at start: %s", loadAverage())

	sizes := memorySizes(t)
	// The gate first in each round, so that if the machine degrades partway
	// through a rep the arm being judged is the one measured earliest in it.
	subjects := []memSubject{subjectEncodeTo(), subjectEncode()}
	reps := memoryReps(t)

	// ⚠ INTERLEAVE. Round-robin over (size, subject) so that a machine that
	// gets busier partway through spreads its noise across every arm instead of
	// landing entirely on the last one. Per measurement-validity.md, and the
	// spread published below is what says whether it worked.
	var results []memResult
	for r := 0; r < reps; r++ {
		for _, size := range sizes {
			for _, s := range subjects {
				results = append(results, measureOnce(t, s, size))
			}
		}
	}

	reportMemory(t, results, reps)
	assertPeakRatio(t, results)
	t.Logf("load average at end: %s", loadAverage())
}

// assertPeakRatio is spec 4b's success criterion 4, and it is asserted rather
// than eyeballed off the table above.
//
// ⚠ IT JUDGES ONE SUBJECT, NAMED. Measuring Encode here would fail a correct
// implementation — see subjectEncodeTo's comment — so the gate is EncodeTo and
// the Encode rows are informational.
//
// ⚠ AND IT IS "UNDER 2x", NOT "FLAT". Phase 1 established that the OLD
// implementation is already flat across sizes (10.9x / 9.3x / 10.0x), so
// flatness alone distinguishes nothing. Flatness is reported; the bound is
// what fails.
func assertPeakRatio(t *testing.T, results []memResult) {
	t.Helper()
	seen := 0
	for _, r := range results {
		if r.subject != gateSubject {
			continue
		}
		seen++
		out := float64(r.outBytes)
		if out == 0 {
			t.Fatalf("%s at %d pips produced 0 bytes; every ratio below divides by it",
				r.subject, r.size)
		}
		if allowed := peakRatioBound*out + goHeapMinimum; float64(r.peakAlloc) >= allowed {
			t.Errorf("%s at %s pips: peak HeapAlloc %.1f MB is %.2fx the %.1f MB output, over the "+
				"%.1fx bound (+%.1f MB for the collector's minimum heap goal, = %.1f MB allowed)",
				r.subject, thousands(r.size), float64(r.peakAlloc)/1e6, float64(r.peakAlloc)/out,
				out/1e6, peakRatioBound, float64(goHeapMinimum)/1e6, allowed/1e6)
		}
		// STRICT — no allowance. The forced collection reads the live set,
		// which the collector's goal does not floor.
		if ratio := float64(r.peakLive) / out; ratio >= peakRatioBound {
			t.Errorf("%s at %s pips: peak live set %.1f MB is %.2fx the %.1f MB output, over the %.1fx bound",
				r.subject, thousands(r.size), float64(r.peakLive)/1e6, ratio, out/1e6, peakRatioBound)
		}
	}
	if seen == 0 {
		t.Fatalf("no %s results to judge; the acceptance criterion was not evaluated at all", gateSubject)
	}
}

func measureOnce(t *testing.T, s memSubject, pips int) memResult {
	t.Helper()

	// The Document's own live cost, so the "Document is ~8% of peak" claim is
	// measured rather than inherited. Build it inside a settled heap and diff.
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	doc := memoryDocument(pips)
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	// ⚠ SIGNED, AND THAT IS NOT PEDANTRY. HeapAlloc is a uint64, and a
	// collection between the two readings can leave `after` BELOW `before` — at
	// 200 pips this printed the Document as 18,446,744,073,709.6 MB, a number
	// with the shape of a measurement and none of the content. A difference
	// that comes out at or below zero means the Document is smaller than this
	// instrument can resolve, and the report says so rather than showing a
	// figure.
	docBytes := int64(after.HeapAlloc) - int64(before.HeapAlloc)

	// Arm A: peak HeapAlloc under the pinned GOGC, and the time. No forced GC,
	// because forcing one distorts both.
	stopA := startPeakSampler(200*time.Microsecond, false)
	start := time.Now()
	outBytes, err := s.run(doc)
	elapsed := time.Since(start)
	peakAlloc := stopA()
	if err != nil {
		t.Fatalf("%s at %d pips: %v", s.name, pips, err)
	}

	// Arm B: peak LIVE set. The sampler forces a collection before reading, so
	// floating garbage is excluded — this is a liveness fact where arm A is a
	// GC-policy fact.
	//
	// ⚠ The forced collections make this arm SLOW, so its grid is coarse and it
	// can miss a narrow peak. It is a lower bound on peak live, not an exact
	// one, and the time column deliberately comes from arm A.
	runtime.GC()
	stopB := startPeakSampler(20*time.Millisecond, true)
	if _, err := s.run(doc); err != nil {
		t.Fatalf("%s at %d pips (live arm): %v", s.name, pips, err)
	}
	peakLive := stopB()

	runtime.KeepAlive(doc)
	return memResult{
		size: pips, subject: s.name, outBytes: outBytes, docBytes: docBytes,
		peakAlloc: peakAlloc, peakLive: peakLive, encode: elapsed,
	}
}

// startPeakSampler polls HeapAlloc until the returned function is called, which
// stops it and returns the maximum seen. With forceGC it collects first, so the
// reading is the live set rather than live-plus-floating-garbage.
//
// ⚠ IT DOES NOT RETURN UNTIL ITS FIRST SAMPLE IS TAKEN, and that handshake is
// load-bearing rather than tidy. Without it the goroutine can fail to be
// scheduled AT ALL before a short subject finishes and closes stop — in which
// case the sampler reports a peak of ZERO, which is indistinguishable from a
// subject that allocated nothing. Observed 2 times in 60 runs of
// TestMemoryHarnessProbesAreLive on 2026-09-02, and the guard caught it only
// because it asserts a floor; the measurement table has no such floor and would
// have printed the zero as a ratio.
func startPeakSampler(interval time.Duration, forceGC bool) func() uint64 {
	stop := make(chan struct{})
	done := make(chan uint64, 1)
	ready := make(chan struct{})
	go func() {
		var peak uint64
		var ms runtime.MemStats
		sample := func() {
			if forceGC {
				runtime.GC()
			}
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > peak {
				peak = ms.HeapAlloc
			}
		}
		sample()
		close(ready)
		for {
			select {
			case <-stop:
				// One last reading, so a peak reached in the final interval
				// before the subject returned is not simply missed.
				sample()
				done <- peak
				return
			default:
			}
			time.Sleep(interval)
			sample()
		}
	}()
	<-ready
	return func() uint64 {
		close(stop)
		return <-done
	}
}

func reportMemory(t *testing.T, results []memResult, reps int) {
	t.Helper()
	type key struct {
		size    int
		subject string
	}
	grouped := map[key][]memResult{}
	var order []key
	for _, r := range results {
		k := key{r.size, r.subject}
		if _, seen := grouped[k]; !seen {
			order = append(order, k)
		}
		grouped[k] = append(grouped[k], r)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].size != order[j].size {
			return order[i].size < order[j].size
		}
		return order[i].subject < order[j].subject
	})

	t.Logf("%d reps per arm, interleaved. Spread is max-min WITHIN an arm — "+
		"if it approaches the delta you are comparing, the machine was measured and not the change.", reps)
	for _, k := range order {
		rs := grouped[k]
		alloc := make([]float64, len(rs))
		live := make([]float64, len(rs))
		secs := make([]float64, len(rs))
		for i, r := range rs {
			alloc[i] = float64(r.peakAlloc)
			live[i] = float64(r.peakLive)
			secs[i] = r.encode.Seconds()
		}
		out := float64(rs[0].outBytes)
		doc := float64(rs[0].docBytes)
		aMed, aSpread, aMax := medianSpreadMax(alloc)
		lMed, lSpread, lMax := medianSpreadMax(live)
		tMed, tSpread, _ := medianSpreadMax(secs)

		// ⚠ MEDIAN AND MAX, and for a PEAK the max is the better estimate. A
		// polling sampler can only ever MISS a peak, never invent one, so a
		// low reading is an instrument miss and a high one is evidence. The
		// median is printed for the spread it belongs to; the ratio quoted
		// against the target is the max.
		t.Logf("─── %s at %s pips ───", k.subject, thousands(k.size))
		t.Logf("  output            %8.1f MB", out/1e6)
		if rs[0].docBytes > 0 {
			t.Logf("  Document (live)   %8.1f MB  = %.1f%% of peak HeapAlloc", doc/1e6, 100*doc/aMax)
		} else {
			t.Logf("  Document (live)   below what this instrument resolves (diff %d bytes); "+
				"a collection landed between the two readings", rs[0].docBytes)
		}
		t.Logf("  peak HeapAlloc    med %7.1f MB  max %7.1f MB  (spread %.1f over %d)  ratio %.1fx output",
			aMed/1e6, aMax/1e6, aSpread/1e6, len(rs), aMax/out)
		t.Logf("  peak live set     med %7.1f MB  max %7.1f MB  (spread %.1f over %d)  ratio %.1fx output",
			lMed/1e6, lMax/1e6, lSpread/1e6, len(rs), lMax/out)
		t.Logf("  encode time       %8.3f s   (spread %.3f s over %d)", tMed, tSpread, len(rs))
	}
}

func medianSpreadMax(v []float64) (median, spread, max float64) {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2], s[len(s)-1] - s[0], s[len(s)-1]
}

// memoryDocument builds the shape spec §1.1 measured: 100 tracks of timed,
// elevated pips. The pip count is what varies, so the ratio is a function of
// scale and not of shape.
func memoryDocument(pips int) *Document {
	const tracks = 100
	per := pips / tracks
	d := &Document{Generator: "memory-harness", Created: "2026-09-02T00:00:00Z"}
	d.Tracks = make([]Track, 0, tracks)
	base := int64(1778569391000)
	for k := 0; k < tracks; k++ {
		seg := Segment{Pips: make([]Position, per)}
		for j := 0; j < per; j++ {
			ele := 300 + float64(j%700) + float64(j%13)/8
			ts := base + int64(k)*86_400_000 + int64(j)*1000
			seg.Pips[j] = Position{
				Lon:  -3.6 + float64(j%10000)/1e5 + float64(k)/1e3,
				Lat:  57.1 + float64(j%9000)/1e5,
				Ele:  &ele,
				Time: &ts,
			}
		}
		d.Tracks = append(d.Tracks, Track{
			Name:     fmt.Sprintf("day %d", k),
			Segments: []Segment{seg},
		})
	}
	return d
}

func memorySizes(t *testing.T) []int {
	if v := os.Getenv("VNX_MEMORY_SIZES"); v != "" {
		var out []int
		for _, s := range splitCommas(v) {
			n, err := strconv.Atoi(s)
			if err != nil {
				t.Fatalf("VNX_MEMORY_SIZES: %q is not a number", s)
			}
			out = append(out, n)
		}
		return out
	}
	return []int{20_000, 200_000, 2_000_000}
}

func memoryReps(t *testing.T) int {
	v := os.Getenv("VNX_MEMORY_REPS")
	if v == "" {
		return 3
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		t.Fatalf("VNX_MEMORY_REPS: %q is not a positive number", v)
	}
	return n
}

func splitCommas(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// loadAverage reads the machine's load, which is the context a lone number
// omits. Unavailable is reported as such rather than as zero.
func loadAverage() string {
	b, err := os.ReadFile("/proc/loadavg")
	if err == nil {
		return string(b[:min(len(b), 24)])
	}
	// darwin has no /proc; sysctl is the route and this package may not shell
	// out, so say so rather than inventing a figure.
	return "unavailable on this platform — record it from the shell beside the run"
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}

// The harness must not silently measure nothing. A document that failed to
// build, or a sampler that never fired, would otherwise report a clean zero.
func TestMemoryHarnessProbesAreLive(t *testing.T) {
	doc := memoryDocument(2_000)
	got := 0
	for _, tr := range doc.Tracks {
		for _, s := range tr.Segments {
			got += len(s.Pips)
		}
	}
	if got != 2_000 {
		t.Fatalf("memoryDocument(2000) built %d pips", got)
	}
	out, err := Encode(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 2_000*20 {
		t.Fatalf("2000 timed, elevated pips encoded to %d bytes; that is too small to be the document", len(out))
	}

	// The sampler must actually observe an allocation. A sampler that returned
	// zero would make every peak in the table a clean, wrong zero. And the two
	// columns must measure DIFFERENT things, or reporting both is theatre.
	//
	// ⚠ BOTH CLAIMS ARE MADE OVER THE SAME FIXTURE, SAMPLED TWICE. A retained
	// baseline holds the collector's goal high enough that a burst of dead
	// garbage does not trigger a collection; the identical burst is then
	// sampled with the forcing sampler and without it. The ONLY difference
	// between the two readings is the forced collection, which is exactly the
	// difference the table's two columns claim to have.
	//
	// ⚠ THIS SHAPE REPLACED ONE THAT WAS FLAKY AND, SEPARATELY, INSENSITIVE.
	// The old guard allocated 64 MB retained, then 64 MB dropped, and asserted
	// live < peak. It failed about 1 in 30 full-package runs because the
	// retained 64 MB was still on the heap during the second arm, so the
	// verdict depended on when a collection landed. Worse, disabling
	// startPeakSampler's runtime.GC() outright left it GREEN: with a tiny live
	// set the collector keeps HeapAlloc low whether or not the sampler forces
	// anything, so it never tested the flag it was named for. Found by
	// red-proofing, 2026-09-02.
	const baselineMB = 128
	const garbageMB = 64

	// Pin the collector here too. An inherited GOGC could collect inside the
	// unforced arm and erase the very difference being asserted.
	prevGC := debug.SetGCPercent(100)
	defer debug.SetGCPercent(prevGC)

	baseline := make([][]byte, 0, baselineMB)
	for i := 0; i < baselineMB; i++ {
		baseline = append(baseline, make([]byte, 1<<20))
	}
	// Settle, so the next collection's goal is set against the full baseline.
	runtime.GC()

	churn := func() {
		for i := 0; i < garbageMB; i++ {
			_ = make([]byte, 1<<20) //nolint:staticcheck // deliberately dropped
		}
	}

	stopAlloc := startPeakSampler(50*time.Microsecond, false)
	churn()
	allocPeak := stopAlloc()

	runtime.GC()
	stopLive := startPeakSampler(2*time.Millisecond, true)
	churn()
	livePeak := stopLive()
	runtime.KeepAlive(baseline)

	if allocPeak < baselineMB<<20 {
		t.Fatalf("the sampler saw a peak of %d MB while %d MB was retained in front of it — "+
			"it is not sampling", allocPeak>>20, baselineMB)
	}
	// ⚠ A QUARTER OF THE GARBAGE IS THE BOUND, NOT ALL OF IT OR HALF OF IT.
	// The unforced sampler can only ever MISS a peak, never invent one, so a
	// fast burst is sometimes only partly seen: over 60 runs the healthy gap
	// was usually ~60 MB and twice came in at 29-30. Disabling the forced
	// collection — the defect this guard is for — puts it at 5 MB. A quarter
	// sits well clear of both, which is what a stable AND meaningful bound
	// looks like; half was neither, and failed 2 runs in 40.
	if gap := int64(allocPeak) - int64(livePeak); gap < (garbageMB/4)<<20 {
		t.Fatalf("peak HeapAlloc %d MB and peak live set %d MB differ by %d MB over the SAME %d MB "+
			"of dead garbage; the forced collection is not excluding it, so the two instruments are "+
			"not distinguishable and §5.4's two-peak requirement is unmet",
			allocPeak>>20, livePeak>>20, gap>>20, garbageMB)
	}
}
