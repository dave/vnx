package vnx

import "testing"

func TestFoldersRoundTrip(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","folders":[
	  {"id":"cairngorms","name":"Cairngorms"},
	  {"id":"approaches","name":"Approaches","parent":"cairngorms"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 {
		t.Fatalf("clean folders produced %v", diags)
	}
	if len(doc.Folders) != 2 {
		t.Fatalf("got %d folders", len(doc.Folders))
	}
	if doc.Folders[1].Parent != "cairngorms" || doc.Folders[0].Parent != "" {
		t.Fatalf("parents wrong: %+v", doc.Folders)
	}
}

// A parent that names nothing is an unusable VALUE, so §6.2 drops the offending
// element — the reference, not the folder. Dropping the folder would take its
// contents with it; keeping it at root loses only its position.
func TestDanglingParentIsDiagnosedAndFolderKeptAtRoot(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","folders":[
	  {"id":"orphan","name":"Orphan","parent":"nowhere"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Folders) != 1 {
		t.Fatalf("the folder must survive, got %+v", doc.Folders)
	}
	if doc.Folders[0].Parent != "" {
		t.Fatalf("dangling parent must be cleared, got %q", doc.Folders[0].Parent)
	}
	if len(diags) != 1 || diags[0].Path != "/folders/0/parent" {
		t.Fatalf("want one diagnostic at /folders/0/parent, got %v", diags)
	}
}

// A cycle cannot be walked, so it must be broken — but MINIMALLY. Breaking
// a->b->a by clearing one link leaves b under a and a at the root: a valid tree
// that still carries one of the two relationships the document asserted.
// Clearing both would be over-correction, discarding a fact that was true.
//
// So the test asserts the INVARIANT (no chain cycles, nothing dropped, the
// repair is reported) rather than one particular repair.
func TestCycleIsBrokenMinimallyAndDiagnosed(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","folders":[
	  {"id":"a","name":"A","parent":"b"},
	  {"id":"b","name":"B","parent":"a"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Folders) != 2 {
		t.Fatalf("no folder may be dropped, got %+v", doc.Folders)
	}
	index := map[string]int{}
	for i, f := range doc.Folders {
		index[f.ID] = i
	}
	for i, f := range doc.Folders {
		seen := map[int]bool{i: true}
		for cur := i; doc.Folders[cur].Parent != ""; {
			next, ok := index[doc.Folders[cur].Parent]
			if !ok {
				t.Fatalf("folder %q points at %q, which is not present", doc.Folders[cur].ID, doc.Folders[cur].Parent)
			}
			if seen[next] {
				t.Fatalf("folder %q is still in a cycle", f.ID)
			}
			seen[next] = true
			cur = next
		}
	}
	// And exactly one link was cut, not both.
	cleared := 0
	for _, f := range doc.Folders {
		if f.Parent == "" {
			cleared++
		}
	}
	if cleared != 1 {
		t.Fatalf("want exactly one parent cleared (minimal repair), got %d", cleared)
	}
	if len(diags) != 1 {
		t.Fatalf("want one diagnostic, got %v", diags)
	}
}

func TestDuplicateFolderIDIsDiagnosed(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","folders":[
	  {"id":"same","name":"First"},{"id":"same","name":"Second"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Folders) != 1 || doc.Folders[0].Name != "First" {
		t.Fatalf("the first wins and the second is dropped, got %+v", doc.Folders)
	}
	if len(diags) != 1 || diags[0].Path != "/folders/1" {
		t.Fatalf("got %v", diags)
	}
}

func TestFolderWithoutIDIsDropped(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.1.0","folders":[{"name":"No id"},{"id":"ok","name":"Fine"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Folders) != 1 || doc.Folders[0].ID != "ok" {
		t.Fatalf("got %+v", doc.Folders)
	}
	if len(diags) != 1 || diags[0].Path != "/folders/0" {
		t.Fatalf("got %v", diags)
	}
}
