package vnx

import "fmt"

// Folder is one node of the library tree. Folders are the only top-level items
// that carry an id, because they are the only ones referenced elsewhere: every
// other item names its folder, and nothing names them back.
type Folder struct {
	ID     string
	Name   string
	Notes  string
	Parent string // "" at the root
	Hidden bool
	Data   []Record
}

var folderKeys = map[string]bool{
	"id": true, "name": true, "notes": true, "parent": true, "hidden": true, "data": true,
}

// folderFromValue reads one element of `folders`. seen holds the ids of the
// folders already kept, because the first one with an id wins. ok is false
// when the folder is dropped.
func folderFromValue(item any, at string, seen map[string]bool, diags []Diagnostic) (Folder, bool, []Diagnostic) {
	obj, ok := item.(map[string]any)
	if !ok {
		return Folder{}, false, append(diags, Diagnostic{Severity: Warning, Path: at,
			Message: fmt.Sprintf("folder is %s, expected an object — dropped", jsonKind(item))})
	}
	id, _ := obj["id"].(string)
	if id == "" {
		// Without an id nothing can reference it, so the folder cannot hold
		// anything. Unlike a dangling parent, dropping it loses nothing that
		// was reachable.
		return Folder{}, false, append(diags, Diagnostic{Severity: Warning, Path: at,
			Message: "folder has no `id` — dropped, nothing could reference it"})
	}
	if seen[id] {
		return Folder{}, false, append(diags, Diagnostic{Severity: Warning, Path: at,
			Message: fmt.Sprintf("duplicate folder id %q — dropped, the first one wins", id)})
	}
	seen[id] = true

	var f Folder
	f.ID = id
	f.Name, diags = stringField(obj, "name", diags)
	f.Notes, diags = stringField(obj, "notes", diags)
	f.Parent, diags = stringField(obj, "parent", diags)
	f.Hidden, diags = boolField(obj, "hidden", at, diags)
	f.Data, diags = dataField(obj, at, diags)
	diags = reportUnknownKeys(obj, at, folderKeys, diags)
	return f, true, diags
}

// resolveParents clears any parent that cannot be walked, and says so.
//
// The choice of WHAT to drop matters. §6.2 treats a parent naming a folder
// the document does not hold as absent ([CodeFolderAbsent]), and a circular
// one is cleared the same way: the REFERENCE goes, not the folder. Dropping
// the folder would take everything filed under it with it; clearing the parent
// loses only the folder's position in the tree. Losing a position is
// recoverable by hand; losing content is not.
//
// paths is the source path of each folder, parallel to folders. A folder's
// index in folders is not its index in the file once an earlier one was
// dropped, and the diagnostic must name the `parent` key the file holds.
func resolveParents(folders []Folder, paths []string, diags []Diagnostic) ([]Folder, []Diagnostic) {
	index := make(map[string]int, len(folders))
	for i, f := range folders {
		index[f.ID] = i
	}
	for i := range folders {
		if folders[i].Parent == "" {
			continue
		}
		at := paths[i] + "/parent"
		if _, ok := index[folders[i].Parent]; !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at, Code: CodeFolderAbsent,
				Message: fmt.Sprintf("parent folder %q is not in this document — the folder sits at the root", folders[i].Parent)})
			folders[i].Parent = ""
			continue
		}
		if cycle := walkToRoot(folders, index, i); cycle {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: "parent chain forms a cycle — folder kept at the root"})
			folders[i].Parent = ""
		}
	}
	return folders, diags
}

// folderIDs is the set of ids of the folders kept.
func folderIDs(folders []Folder) map[string]bool {
	held := make(map[string]bool, len(folders))
	for _, f := range folders {
		held[f.ID] = true
	}
	return held
}

// clearAbsentFolders treats as absent every item `folder` that names a folder
// not in held, and reports each with [CodeFolderAbsent] at the item's
// `folder` key (§6.2). paths is the source path of each item, parallel to
// items; kind names the item in the message.
//
// The item is kept. Like a dangling parent, the reference is the unusable
// part, not the item: dropping it would lose content over a lost grouping.
func clearAbsentFolders[T any](items []T, paths []string, kind string, held map[string]bool, folder func(*T) *string, diags []Diagnostic) []Diagnostic {
	for i := range items {
		f := folder(&items[i])
		if *f == "" || held[*f] {
			continue
		}
		diags = append(diags, Diagnostic{Severity: Warning, Path: paths[i] + "/folder", Code: CodeFolderAbsent,
			Message: fmt.Sprintf("folder %q is not in this document — the %s sits at the top level", *f, kind)})
		*f = ""
	}
	return diags
}

// walkToRoot reports whether following parents from start revisits a folder.
// Bounded by the folder count, so a cycle cannot spin.
func walkToRoot(folders []Folder, index map[string]int, start int) bool {
	seen := map[int]bool{start: true}
	cur := start
	for range folders {
		parent := folders[cur].Parent
		if parent == "" {
			return false
		}
		next, ok := index[parent]
		if !ok {
			return false
		}
		if seen[next] {
			return true
		}
		seen[next] = true
		cur = next
	}
	return true
}

// boolField reads an optional boolean. Absence is not false-by-default in
// general — `hidden` happens to mean visible when absent (§3.8), which is why
// the caller, not this helper, owns that meaning.
func boolField(obj map[string]any, key, path string, diags []Diagnostic) (bool, []Diagnostic) {
	v, present := obj[key]
	if !present {
		return false, diags
	}
	b, ok := v.(bool)
	if !ok {
		return false, append(diags, Diagnostic{Severity: Warning, Path: path + "/" + key,
			Code:    CodeWrongTypeOptional,
			Message: fmt.Sprintf("expected a boolean, found %s — field dropped, item kept", jsonKind(v))})
	}
	return b, diags
}
