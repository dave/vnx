package kml

import "github.com/dave/vnx/go/pkg/vnx"

// The folder tree, which is KML's one real win over the other two lossy
// formats (spec 5b §3.2): <Folder> nests, so the library's shape survives and
// no name has to carry a path.
//
// ⚠ THIS PACKAGE DOES NOT CALL vnx.FlatName, AND THAT IS THE RULING, not an
// omission (spec §2.7: "KML is unaffected: it has real nested <Folder>
// elements and keeps the tree"). It uses vnx.FolderPath for one narrower
// purpose — asking whether a folder's ancestry resolves at all — because a
// folder whose parent chain dangles or cycles has no place in a tree and must
// still be written somewhere.

// bucket is what one container — the document root, or one folder — holds.
// Every field is a list of indices into the Document's own slices, so nothing
// here copies an item.
type bucket struct {
	folders   []int
	waypoints []int
	areas     []int
	tracks    []int
	routes    []int
	networks  []int
}

// layout is the whole document arranged as a tree.
type layout struct {
	root    bucket
	byIndex map[int]*bucket // keyed by the folder's index in d.Folders
	// loose holds the folders whose ancestry does not resolve, and the items
	// naming a folder this document does not have. Both are written at the
	// root and both are diagnosed: an item is never dropped for where it sits.
	looseFolders []int
	loose        bucket
	// placeable[i] is true when folder i has a resolvable ancestry AND is the
	// first folder with its id.
	placeable []bool
}

// plan arranges the document.
//
// ⚠ THE FIRST FOLDER WITH AN ID WINS, matching vnx.decodeFolders ("duplicate
// folder id %q — dropped, the first one wins"). A later duplicate is written at
// the root as itself and receives no children, so a duplicated id cannot make
// this writer emit one subtree twice — which it otherwise would, silently, and
// only for hand-built documents nobody looks at twice.
//
// ⚠ THE PLACEABLE TREE CANNOT CYCLE, so the recursive walk that reads it cannot
// hang. vnx.FolderPath returns ok only when the parent chain reaches the root
// within the folder count, and a child edge exists only from a placeable folder
// to its parent — so every child's depth is exactly one more than its parent's,
// and depth is bounded. This is why the emptiness of a `seen` set is a proof
// rather than an oversight.
func plan(d *vnx.Document) *layout {
	l := &layout{
		byIndex:   make(map[int]*bucket, len(d.Folders)),
		placeable: make([]bool, len(d.Folders)),
	}

	first := make(map[string]int, len(d.Folders))
	for i := range d.Folders {
		id := d.Folders[i].ID
		if id == "" {
			continue
		}
		if _, dup := first[id]; !dup {
			first[id] = i
		}
	}

	for i := range d.Folders {
		id := d.Folders[i].ID
		if id == "" || first[id] != i {
			// An id-less folder is unreachable — nothing can name it — and a
			// duplicate is not the one that wins. Neither can hold anything, so
			// neither gets a bucket; both are still written, at the root.
			continue
		}
		// ⚠ EVERY FOLDER AN ITEM CAN NAME GETS A BUCKET, placeable or not. A
		// folder with a cyclic parent is written at the root instead of nested,
		// but it is still a real <Folder> that items name and must sit inside —
		// giving it no bucket would send its contents to the root beside it and
		// report them as homeless, which they are not.
		l.byIndex[i] = &bucket{}
		if _, ok := vnx.FolderPath(d, id); ok {
			l.placeable[i] = true
		}
	}

	for i := range d.Folders {
		if !l.placeable[i] {
			l.looseFolders = append(l.looseFolders, i)
			continue
		}
		parent := d.Folders[i].Parent
		if parent == "" {
			l.root.folders = append(l.root.folders, i)
			continue
		}
		// Placeable implies the whole chain resolved, so this lookup succeeds.
		l.byIndex[first[parent]].folders = append(l.byIndex[first[parent]].folders, i)
	}

	file := func(folder string, add func(*bucket)) {
		if folder == "" {
			add(&l.root)
			return
		}
		if i, ok := first[folder]; ok {
			add(l.byIndex[i])
			return
		}
		add(&l.loose)
	}
	// The item's own folder element exists wherever `first` knows the id — at
	// the root when it was loose, nested when it was placeable — so an item
	// nests correctly either way, and only an id this document has no folder
	// for is homeless.
	for i := range d.Waypoints {
		i := i
		file(d.Waypoints[i].Folder, func(b *bucket) { b.waypoints = append(b.waypoints, i) })
	}
	for i := range d.Areas {
		i := i
		file(d.Areas[i].Folder, func(b *bucket) { b.areas = append(b.areas, i) })
	}
	for i := range d.Tracks {
		i := i
		file(d.Tracks[i].Folder, func(b *bucket) { b.tracks = append(b.tracks, i) })
	}
	for i := range d.Routes {
		i := i
		file(d.Routes[i].Folder, func(b *bucket) { b.routes = append(b.routes, i) })
	}
	for i := range d.Networks {
		i := i
		file(d.Networks[i].Folder, func(b *bucket) { b.networks = append(b.networks, i) })
	}
	return l
}
