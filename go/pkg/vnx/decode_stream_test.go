package vnx

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/iotest"
)

// streamCases are documents written to reach the corners of the streaming
// reader: keys out of the format's order, keys twice, values of the wrong
// kind at every streamed key, and input that is not a document at all.
var streamCases = map[string]string{
	"empty input":                         ``,
	"white space only":                    " \n\t",
	"array root":                          `[1,2]`,
	"array root, truncated":               `[1,2`,
	"string root":                         `"vnx"`,
	"null root":                           `null`,
	"trailing data":                       `{"vnx":"0.11.0"} x`,
	"trailing value":                      `{"vnx":"0.11.0"} {}`,
	"truncated object":                    `{"vnx":"0.11.0","tracks":[{"name":"a"`,
	"no vnx key":                          `{"tracks":[]}`,
	"vnx a number":                        `{"vnx":1}`,
	"vnx null":                            `{"vnx":null}`,
	"vnx twice, bad then good":            `{"vnx":"9.0.0","points":[{"at":[1,2]}],"vnx":"0.11.0"}`,
	"vnx twice, good then bad":            `{"vnx":"0.11.0","points":[{"at":[1,2]}],"vnx":"0.1"}`,
	"version last":                        `{"points":[{"at":[1,2],"zz":1}],"tracks":[{"bogus":1}],"vnx":"0.11.0"}`,
	"newer version":                       `{"vnx":"0.12.0","tracks":[]}`,
	"older version":                       `{"vnx":"0.9.1","tracks":[]}`,
	"newer version, then bad JSON":        `{"vnx":"9.0.0","points":[`,
	"1e400 in an unknown key":             `{"vnx":"0.11.0","points":[{"name":"a","at":[1,2]}],"zzz":[1e400]}`,
	"1e400 in a fifth channel":            `{"vnx":"0.11.0","points":[{"name":"a","at":[1,2,3,4,1e400]}]}`,
	"1e400 in an application key":         `{"vnx":"0.11.0","app.x":1e400}`,
	"1e400 after a bad pip":               `{"vnx":"0.11.0","tracks":[{"segments":[{"pips":[[1,2],"x",[1,1e400]]}]}]}`,
	"1e400 in a skipped root":             `[1e400]`,
	"1e400 in a wrong-kind key":           `{"vnx":"0.11.0","tracks":{"a":1e400}}`,
	"dup tracks":                          `{"vnx":"0.11.0","tracks":[{"name":"a","bogus":1}],"tracks":[{"name":"b"}]}`,
	"dup segments":                        `{"vnx":"0.11.0","tracks":[{"segments":[{"pips":"x"}],"segments":[{"pips":[[1,2]]}]}]}`,
	"dup pips":                            `{"vnx":"0.11.0","tracks":[{"segments":[{"pips":[[1,2]],"pips":null}]}]}`,
	"dup polygon":                         `{"vnx":"0.11.0","areas":[{"polygon":[[0,0],[1,0],[1,1]],"polygon":7}]}`,
	"dup nodes":                           `{"vnx":"0.11.0","networks":[{"nodes":{"a":[1,2]},"arcs":{"x":{"from":"a","to":"b","pips":[]}},"nodes":{"a":[1,2],"b":[3,4]}}]}`,
	"dup arcs":                            `{"vnx":"0.11.0","networks":[{"nodes":{"a":[1,2],"b":[3,4]},"arcs":{"x":7},"arcs":{"y":{"from":"a","to":"b","pips":[]}}}]}`,
	"dup arc id":                          `{"vnx":"0.11.0","networks":[{"nodes":{"a":[1,2],"b":[3,4]},"arcs":{"x":7,"x":{"from":"a","to":"b","pips":[]}}}]}`,
	"dup node id":                         `{"vnx":"0.11.0","networks":[{"nodes":{"a":"bad","a":[1,2,3,4]}}]}`,
	"dup network areas":                   `{"vnx":"0.11.0","networks":[{"areas":[{"polygon":1}],"areas":5}]}`,
	"dup folders":                         `{"vnx":"0.11.0","folders":[{"id":"a"},{"id":"a"}],"folders":[{"id":"b","parent":"c"}]}`,
	"arcs before nodes":                   `{"vnx":"0.11.0","networks":[{"arcs":{"b":{"from":"n1","to":"n9","pips":[[1,2],[3,4]]},"a":{"from":"n1","to":"n2","pips":[[1,2],[3,4]],"q":1}},"nodes":{"n1":[1,2],"n2":[3,4]}}]}`,
	"collections of every wrong kind":     `{"vnx":"0.11.0","folders":{},"points":"x","networks":1,"plans":true,"tracks":null,"areas":{"a":[1]}}`,
	"elements of every wrong kind":        `{"vnx":"0.11.0","folders":[1],"points":[null],"networks":["x"],"plans":[[]],"tracks":[true],"areas":[2]}`,
	"segments kinds":                      `{"vnx":"0.11.0","tracks":[{"segments":null},{"segments":"x"},{"segments":[]},{"segments":[1,null,{},{"pips":null},{"pips":[]},{"pips":{}},{"pips":[[1,2],[200,1]]},{"pips":[[1,2],[1,2,null,5]],"zz":1,"a/b":2}]}]}`,
	"pips kinds":                          `{"vnx":"0.11.0","tracks":[{"segments":[{"pips":[1]},{"pips":[[1]]},{"pips":[["a",1]]},{"pips":[[1,2,"e"]]},{"pips":[[1,2,null,"t"]]},{"pips":[[1,2,null,null,{"x":1}]]}]}]}`,
	"ring kinds":                          `{"vnx":"0.11.0","areas":[{},{"polygon":null},{"polygon":"x"},{"polygon":[]},{"polygon":[[[0,0]]]},{"polygon":[[]]},{"polygon":[[0,0],1]},{"polygon":[[0,0],[1,0],[1,1],[0,0]]},{"polygon":[[0,0],[1,0],[0,0]]},{"polygon":[[0,0],[1,0],[1,91]]},{"polygon":[[0,0],[1,0],[1,1]],"name":5,"zz":1}]}`,
	"graph kinds":                         `{"vnx":"0.11.0","networks":[{"nodes":null,"arcs":null},{"nodes":1,"arcs":[]},{"nodes":{},"arcs":{}},{"nodes":{"a":[1,2,3,4],"b":"x","c":[1],"d":[5,6]},"arcs":{"z":null,"y":{"from":"a","to":"a","pips":[]},"x":{"from":"a","to":"d"},"w":{"from":"a","to":"d","pips":null},"v":{"from":"a","to":"d","pips":[[1,2,3,4]],"data":[{"@type":"a.b"}]},"u":{"from":1,"to":"d","pips":[[1,2]]},"t":{"from":"a","to":"b","pips":[[1,2]]},"s":{"from":"a","to":"d","pips":[[1,2],"bad"]},"r~/":{"from":"a","to":"d","pips":[],"zz":1}},"highlights":[{"walk":[{"arc":"v"}]},{"walk":[{"arc":"t"}]}],"points":[{"at":[1,2]}],"areas":[{"polygon":[[0,0],[1,0],[1,1]]},{"polygon":3}]}]}`,
	"network points and areas not arrays": `{"vnx":"0.11.0","networks":[{"points":{},"areas":"x"}]}`,
	"plans":                               `{"vnx":"0.11.0","plans":[{"walk":[]},{"name":"p","nodes":{"a":[1,2],"b":[3,4]},"arcs":{"x":{"from":"a","to":"b","pips":[[1,2],[3,4]]}},"walk":[{"arc":"x","from":"a","to":"b"}],"zz":1},{"arcs":{"x":{"from":"a","to":"b","pips":[]}},"walk":[{"arc":"x"}],"nodes":{"a":[1,2],"b":[3,4]}}]}`,
	"header kinds":                        `{"vnx":"0.11.0","generator":5,"created":"yesterday","zz":{"deep":[1,[2,{"x":null}]]},"app.a":null,"app.b":[],"a/b":1}`,
	"unicode key folds":                   `{"vnx":"0.11.0","Tracks":[],"tracks":[{"Name":"x","name":"y"}]}`,
}

// TestDecodeReaderMatchesTreeDecode is the reader's whole contract: for every
// document — the corpus, the fixtures, the cases above, and each of those
// rewritten with its keys shuffled, repeated and added to — the streaming
// reader returns what the tree reader returns. The same Document, the same
// diagnostics in the same order, and the same refusal.
func TestDecodeReaderMatchesTreeDecode(t *testing.T) {
	inputs := map[string][]byte{}
	for name, doc := range streamCases {
		inputs["case: "+name] = []byte(doc)
	}
	for _, root := range []string{corpusDir, "testdata"} {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".vnx" {
				return err
			}
			data, err := os.ReadFile(path)
			inputs[path] = data
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	files := 0
	for name, data := range inputs {
		compareDecoders(t, name, data)
		var tree any
		if json.Unmarshal(data, &tree) != nil {
			continue
		}
		files++
		for seed := int64(1); seed <= 3; seed++ {
			for _, mode := range []rewriteMode{shuffleKeys, repeatKeys, addUnknownKeys} {
				var b bytes.Buffer
				rewrite(&b, tree, rand.New(rand.NewSource(seed)), mode, true)
				compareDecoders(t, fmt.Sprintf("%s (%s, seed %d)", name, mode, seed), b.Bytes())
			}
		}
	}
	if files < 60 {
		t.Fatalf("only %d documents parsed as JSON; the corpus was not found and this test proves little", files)
	}
}

// compareDecoders decodes data with both readers, and through a reader that
// returns one byte at a time, and fails on any difference.
func compareDecoders(t *testing.T, name string, data []byte) {
	t.Helper()
	wantDoc, wantDiags, wantErr := decodeTree(data)
	for _, via := range []struct {
		name string
		r    io.Reader
	}{
		{"Decode", nil},
		{"one byte at a time", iotest.OneByteReader(bytes.NewReader(data))},
	} {
		var doc *Document
		var diags []Diagnostic
		var err error
		if via.r == nil {
			doc, diags, err = Decode(data)
		} else {
			doc, diags, err = DecodeReader(via.r)
		}
		if got, want := refusalKind(err), refusalKind(wantErr); got != want {
			t.Errorf("%s, %s: error %v, the tree reader's %v", name, via.name, err, wantErr)
			continue
		}
		if wantErr != nil && !errors.Is(wantErr, ErrMalformed) && err.Error() != wantErr.Error() {
			t.Errorf("%s, %s: error %q, the tree reader's %q", name, via.name, err, wantErr)
		}
		if !reflect.DeepEqual(doc, wantDoc) {
			t.Errorf("%s, %s: the documents differ\n got: %#v\nwant: %#v", name, via.name, doc, wantDoc)
		}
		if !reflect.DeepEqual(diags, wantDiags) {
			t.Errorf("%s, %s: the diagnostics differ\n got: %v\nwant: %v", name, via.name, diags, wantDiags)
		}
	}
}

func refusalKind(err error) string {
	for _, e := range []error{ErrMalformed, ErrNotVNX, ErrNewerVersion, ErrOlderVersion, ErrInvalidVersion, ErrContainer} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	if err != nil {
		return "other: " + err.Error()
	}
	return "none"
}

type rewriteMode string

const (
	shuffleKeys    rewriteMode = "keys shuffled"
	repeatKeys     rewriteMode = "keys repeated"
	addUnknownKeys rewriteMode = "unknown keys added"
)

// rewrite writes v as JSON with each object's keys in a random order. In
// repeatKeys mode some keys are written twice, first with a decoy value — a
// sibling's value, or a value of another kind — so that the last one must win
// and the decoy's diagnostics must go. In addUnknownKeys mode some objects
// gain a key no table knows. The top level always puts `vnx` last.
func rewrite(b *bytes.Buffer, v any, rng *rand.Rand, mode rewriteMode, top bool) {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		rng.Shuffle(len(keys), func(i, j int) { keys[i], keys[j] = keys[j], keys[i] })
		if top {
			for i, k := range keys {
				if k == "vnx" {
					keys = append(append(keys[:i:i], keys[i+1:]...), "vnx")
					break
				}
			}
		}
		b.WriteByte('{')
		first := true
		member := func(k string, val any) {
			if !first {
				b.WriteByte(',')
			}
			first = false
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			rewrite(b, val, rng, mode, false)
		}
		if mode == addUnknownKeys && rng.Intn(3) == 0 {
			member([]string{"zz-unknown", "a/b~c", "x.y"}[rng.Intn(3)], []any{1.5, map[string]any{"q": nil}})
		}
		for _, k := range keys {
			if mode == repeatKeys && rng.Intn(3) == 0 {
				decoys := []any{"decoy", 7.0, nil, []any{"x"}, map[string]any{"pips": "x"}}
				for _, other := range keys {
					decoys = append(decoys, v[other])
				}
				member(k, decoys[rng.Intn(len(decoys))])
			}
			member(k, v[k])
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range v {
			if i > 0 {
				b.WriteByte(',')
			}
			rewrite(b, e, rng, mode, false)
		}
		b.WriteByte(']')
	default:
		vb, err := json.Marshal(v)
		if err != nil {
			panic(err)
		}
		b.Write(vb)
	}
}

// TestDecodeContainerMatchesTreeDecode compares every corpus archive with the
// tree reader over the root entry read whole, as DecodeContainer did before
// it streamed.
func TestDecodeContainerMatchesTreeDecode(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(corpusDir, "*", "*.vnz"))
	if err != nil || len(paths) < 5 {
		t.Fatalf("found %d archives (%v); the corpus was not found", len(paths), err)
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		r := bytes.NewReader(data)
		c, diags, err := DecodeContainer(r, int64(len(data)))

		wantDoc, wantDiags, wantErr := treeDecodeRoot(r, int64(len(data)))
		if refusalKind(err) != refusalKind(wantErr) {
			t.Errorf("%s: error %v, the tree reader's %v", path, err, wantErr)
			continue
		}
		if wantErr != nil {
			if !errors.Is(wantErr, ErrMalformed) && err.Error() != wantErr.Error() {
				t.Errorf("%s: error %q, the tree reader's %q", path, err, wantErr)
			}
			continue
		}
		if !reflect.DeepEqual(c.Document, wantDoc) {
			t.Errorf("%s: the documents differ", path)
		}
		// The attachments' diagnostics follow the document's, unchanged.
		if len(diags) < len(wantDiags) || (len(wantDiags) > 0 && !reflect.DeepEqual(diags[:len(wantDiags)], wantDiags)) {
			t.Errorf("%s: the diagnostics differ\n got: %v\nwant: %v first", path, diags, wantDiags)
		}
	}
}

// treeDecodeRoot is the root-document half of DecodeContainer as it was: the
// entry read whole, then the tree reader. The checks on the archive before it
// are unchanged and are not repeated here.
func treeDecodeRoot(r io.ReaderAt, size int64) (*Document, []Diagnostic, error) {
	entries, err := readZipDirectory(r, size)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	for _, e := range entries {
		if err := checkEntryPath(e.name); err != nil {
			return nil, nil, err
		}
		if seen[strings.ToLower(e.name)] {
			return nil, nil, fmt.Errorf("%w: duplicate entry %q", ErrContainer, e.name)
		}
		seen[strings.ToLower(e.name)] = true
	}
	root := pickRootDocument(entries)
	if root == nil {
		return nil, nil, fmt.Errorf("%w: no .vnx document in the archive", ErrContainer)
	}
	data, err := readZipEntry(r, *root)
	if err != nil {
		return nil, nil, err
	}
	return decodeTree(data)
}

// failingReader returns its bytes and then err, where a file read fails or a
// context is cancelled part way through a document.
type failingReader struct {
	data []byte
	err  error
}

func (f *failingReader) Read(p []byte) (int, error) {
	if len(f.data) == 0 {
		return 0, f.err
	}
	n := copy(p, f.data)
	f.data = f.data[n:]
	return n, nil
}

// TestDecodeReaderPassesReaderErrorsThrough: an error from the reader is the
// error returned, not ErrMalformed, wherever in the document it comes —
// including after the last brace, where the reader is still read to its end.
func TestDecodeReaderPassesReaderErrorsThrough(t *testing.T) {
	doc := `{"vnx":"0.11.0","tracks":[{"segments":[{"pips":[[1,2],[3,4]]}]}]}`
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, cause := range []error{errors.New("disk on fire"), ctx.Err()} {
		for _, cut := range []int{0, 1, len(doc) / 2, len(doc) - 1, len(doc)} {
			_, _, err := DecodeReader(&failingReader{data: []byte(doc[:cut]), err: cause})
			if !errors.Is(err, cause) || errors.Is(err, ErrMalformed) {
				t.Errorf("reader failing with %q after %d bytes: got %v", cause, cut, err)
			}
		}
	}
}

// TestDecodeContainerChecksTheRootEntryWhateverItsJSON: a root entry that
// fails its CRC is a container fault, even when its JSON is malformed or
// refused before the end, as it was when the entry was read whole first.
func TestDecodeContainerChecksTheRootEntryWhateverItsJSON(t *testing.T) {
	// The padding puts the end of the entry well past where a malformed
	// document stops being read.
	pad := strings.Repeat(" ", 1<<20)
	for _, doc := range []string{`{"vnx":` + "x" + pad, `[1,2] x` + pad, `{"vnx":"9.0.0"}` + pad, `{"vnx":"0.11.0"}`} {
		var b bytes.Buffer
		err := WriteContainer(&b, RootDocumentName, func(w io.Writer) error {
			_, err := io.WriteString(w, doc)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		var crc [4]byte
		binary.LittleEndian.PutUint32(crc[:], crc32.ChecksumIEEE([]byte(doc)))
		archive := b.Bytes()
		if bytes.Count(archive, crc[:]) == 0 {
			t.Fatalf("%.20s: the CRC is not in the archive, so nothing was damaged", doc)
		}
		bad := binary.LittleEndian.Uint32(crc[:]) ^ 1
		var badCRC [4]byte
		binary.LittleEndian.PutUint32(badCRC[:], bad)
		archive = bytes.ReplaceAll(archive, crc[:], badCRC[:])
		_, _, err = DecodeContainer(bytes.NewReader(archive), int64(len(archive)))
		if !errors.Is(err, ErrContainer) {
			t.Errorf("%.20s with a bad CRC: got %v, want ErrContainer", doc, err)
		}
	}
}
