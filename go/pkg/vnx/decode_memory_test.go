package vnx

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// decodePeakBound is the most the streaming reader may hold at its peak, as a
// multiple of the file. For pips the Document it returns is about the size of
// the file, and the slice being filled briefly holds its old and its grown
// array together: measured at 1.54x to 2.02x. The tree reader also held a
// generic tree of the pips, and measured 5.85x to 6.25x on the same files.
const decodePeakBound = 3.0

// hugeTrackPips is the size of the one track the peak tests read. It is large
// enough that the tree reader's peak is tens of megabytes above the bound.
const hugeTrackPips = 400_000

// writeHugeTrack writes a document holding one track of n timed pips, and
// returns its path and size.
func writeHugeTrack(t *testing.T, n int) (string, int64) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "huge.vnx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriter(f)
	fmt.Fprintf(w, `{"vnx":%q,"tracks":[{"name":"t","segments":[{"pips":[`, Version)
	for i := 0; i < n; i++ {
		if i > 0 {
			w.WriteByte(',')
		}
		fmt.Fprintf(w, "[%.6f,%.6f,%.1f,%d]", -3+float64(i)*1e-7, 57+float64(i)*1e-7, 300+float64(i%1000)/10, 1693567890123+int64(i)*1000)
	}
	w.WriteString(`]}]}]}`)
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, info.Size()
}

// peakLiveAbove runs decode and returns the peak live heap above the live heap
// before it, sampled with a collection before each reading. Sampling can miss
// a narrow peak, so it is a lower bound; the tree reader's tree is live for
// the whole of its walk, so it cannot be missed.
func peakLiveAbove(t *testing.T, decode func() *Document) uint64 {
	t.Helper()
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	stop := startPeakSampler(time.Millisecond, true)
	doc := decode()
	peak := stop()
	if len(doc.Tracks) != 1 || len(doc.Tracks[0].Segments) != 1 || len(doc.Tracks[0].Segments[0].Pips) != hugeTrackPips {
		t.Fatalf("the document did not read back whole")
	}
	runtime.KeepAlive(doc)
	if peak < base.HeapAlloc {
		return 0
	}
	return peak - base.HeapAlloc
}

// TestDecodeReaderOneHugeTrack_PeakHeap: a file with one huge track is read
// with no more than about the Document resident, not a tree of the file.
func TestDecodeReaderOneHugeTrack_PeakHeap(t *testing.T) {
	path, size := writeHugeTrack(t, hugeTrackPips)
	peak := peakLiveAbove(t, func() *Document {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		doc, _, err := DecodeReader(bufio.NewReader(f))
		if err != nil {
			t.Fatal(err)
		}
		return doc
	})
	t.Logf("file %.1f MB, peak live %.1f MB above the start (%.2fx the file)", float64(size)/1e6, float64(peak)/1e6, float64(peak)/float64(size))
	if float64(peak) > decodePeakBound*float64(size) {
		t.Errorf("peak live %.1f MB is %.2fx the %.1f MB file; the bound is %.1fx",
			float64(peak)/1e6, float64(peak)/float64(size), float64(size)/1e6, decodePeakBound)
	}
}

// TestDecodeContainerStreamsRootEntry_PeakHeap: the root document of a .vnz is
// streamed out of the archive, so neither the inflated entry nor a tree of it
// is resident.
func TestDecodeContainerStreamsRootEntry_PeakHeap(t *testing.T) {
	path, size := writeHugeTrack(t, hugeTrackPips)
	vnz := filepath.Join(t.TempDir(), "huge.vnz")
	out, err := os.Create(vnz)
	if err != nil {
		t.Fatal(err)
	}
	err = WriteContainer(out, RootDocumentName, func(w io.Writer) error {
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		_, err = bufio.NewReader(in).WriteTo(w)
		return err
	})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		t.Fatal(err)
	}
	peak := peakLiveAbove(t, func() *Document {
		f, err := os.Open(vnz)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			t.Fatal(err)
		}
		c, _, err := DecodeContainer(f, info.Size())
		if err != nil {
			t.Fatal(err)
		}
		return c.Document
	})
	t.Logf("document %.1f MB, peak live %.1f MB above the start (%.2fx the document)", float64(size)/1e6, float64(peak)/1e6, float64(peak)/float64(size))
	if float64(peak) > decodePeakBound*float64(size) {
		t.Errorf("peak live %.1f MB is %.2fx the %.1f MB document; the bound is %.1fx",
			float64(peak)/1e6, float64(peak)/float64(size), float64(size)/1e6, decodePeakBound)
	}
}
