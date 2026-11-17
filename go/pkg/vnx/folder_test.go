package vnx

import "testing"

func TestFoldersRoundTrip(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","folders":[
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
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","folders":[
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
	if len(diags) != 1 || diags[0].Path != "/folders/0/parent" || diags[0].Code != CodeFolderAbsent {
		t.Fatalf("want one %s diagnostic at /folders/0/parent, got %+v", CodeFolderAbsent, diags)
	}
}

// A cycle cannot be walked, so it must be broken — but MINIMALLY. Breaking
// a->b->a by clearing one link leaves b under a and a at the root: a valid tree
// that still carries one of the two relationships the document asserted.
// Clearing both would be over-correction, discarding a fact that was true.
//
// The test asserts the INVARIANT (no chain cycles, nothing dropped, the repair
// is reported), and then the repair §3.2 names: the first folder of the loop in
// document order loses its `parent`.
func TestCycleIsBrokenMinimallyAndDiagnosed(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","folders":[
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
	if len(diags) != 1 || diags[0].Path != "/folders/0/parent" || diags[0].Code != CodeFolderParentLoop {
		t.Fatalf("want one %s at /folders/0/parent, got %v", CodeFolderParentLoop, diags)
	}
	if doc.Folders[0].Parent != "" || doc.Folders[1].Parent != "a" {
		t.Fatalf("the first folder of the loop must sit at the root and the second stay under it, got %+v", doc.Folders)
	}
}

func TestDuplicateFolderIDIsDiagnosed(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","folders":[
	  {"id":"same","name":"First"},{"id":"same","name":"Second"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Folders) != 1 || doc.Folders[0].Name != "First" {
		t.Fatalf("the first wins and the second is dropped, got %+v", doc.Folders)
	}
	if len(diags) != 1 || diags[0].Path != "/folders/1/id" || diags[0].Code != CodeFolderDuplicateID {
		t.Fatalf("got %v", diags)
	}
}

func TestFolderWithoutIDIsDropped(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0","folders":[{"name":"No id"},{"id":"ok","name":"Fine"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Folders) != 1 || doc.Folders[0].ID != "ok" {
		t.Fatalf("got %+v", doc.Folders)
	}
	if len(diags) != 1 || diags[0].Path != "/folders/0" || diags[0].Code != CodeWrongTypeRequired {
		t.Fatalf("got %v", diags)
	}
}

// An item's `folder` naming no folder the document holds takes §6.2's remedy:
// the key is treated as absent, the item is KEPT at the top level, and
// `folder-absent` is reported at the item's own `folder` key. Every kind of
// top-level item that carries `folder` is covered, and so are the cases where
// "holds" is not "the file mentions":
//
//   - `nokey` is a folder with no `id`, and `twice` is the second of two
//     folders with one id — both dropped. A folder with no id cannot be named,
//     so the item naming `twice`'s survivor is fine; a parent naming a folder
//     that was dropped for another reason is folder-absent.
//   - `folders` comes LAST, so a reader that checks items as it reads them
//     would see no folders at all and report the good item too.
//   - the first point is dropped (no `at`), so the bad point's path must be
//     its index in the FILE, /points/2, not its index among the kept points.
func TestItemFolderNamingNoFolderIsDiagnosedAndItemKeptAtTopLevel(t *testing.T) {
	doc, diags, err := Decode([]byte(`{"vnx":"0.11.0",
	  "points":[{"name":"dropped"},{"at":[1,2],"folder":"real"},{"at":[1,2],"folder":"ghost"}],
	  "networks":[{"folder":"ghost"}],
	  "plans":[{"walk":[],"folder":"ghost"}],
	  "tracks":[{"segments":[],"folder":"ghost"}],
	  "areas":[{"polygon":[[0,0],[1,0],[1,1]],"folder":"ghost"}],
	  "folders":[
	    {"id":"real"},
	    {"id":"real","name":"twice"},
	    {"name":"nokey","parent":"real"},
	    {"id":"child","parent":"ghost"}
	  ]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"/folders/3/parent":  true,
		"/points/2/folder":   true,
		"/networks/0/folder": true,
		"/plans/0/folder":    true,
		"/tracks/0/folder":   true,
		"/areas/0/folder":    true,
	}
	for _, d := range diags {
		if d.Code != CodeFolderAbsent {
			continue
		}
		if !want[d.Path] {
			t.Errorf("unexpected %s at %s: %s", CodeFolderAbsent, d.Path, d.Message)
		}
		delete(want, d.Path)
	}
	for path := range want {
		t.Errorf("no %s at %s; diagnostics: %+v", CodeFolderAbsent, path, diags)
	}

	// Every item is kept, and only the bad references are cleared.
	if len(doc.Points) != 2 || len(doc.Networks) != 1 || len(doc.Plans) != 1 || len(doc.Tracks) != 1 || len(doc.Areas) != 1 {
		t.Fatalf("an item was dropped over a lost grouping: %d points, %d networks, %d plans, %d tracks, %d areas",
			len(doc.Points), len(doc.Networks), len(doc.Plans), len(doc.Tracks), len(doc.Areas))
	}
	if doc.Points[0].Folder != "real" {
		t.Errorf("a folder the document holds was cleared: %q", doc.Points[0].Folder)
	}
	for what, got := range map[string]string{
		"point": doc.Points[1].Folder, "network": doc.Networks[0].Folder, "plan": doc.Plans[0].Folder,
		"track": doc.Tracks[0].Folder, "area": doc.Areas[0].Folder,
	} {
		if got != "" {
			t.Errorf("the %s's absent folder was kept: %q", what, got)
		}
	}
	if len(doc.Folders) != 2 || doc.Folders[1].ID != "child" || doc.Folders[1].Parent != "" {
		t.Errorf("want `real` and `child` kept, `child` at the root; got %+v", doc.Folders)
	}
}
