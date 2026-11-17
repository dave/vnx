package vnx

import "fmt"

// Folder is one node of the library tree. Folders are the only top-level items
// that carry an id, because they are the only ones referenced elsewhere: every
// other item names its folder, and nothing names them back.
type Folder struct {
	ID     string
	Name   string
	Parent string // "" at the root
	Hidden bool
}

var folderKeys = map[string]bool{
	"id": true, "name": true, "parent": true, "hidden": true,
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
	rawID, present := obj["id"]
	if !present {
		// `id` is REQUIRED (§3.10): a missing one drops the folder, reported at
		// the folder because there is no key to point at.
		return Folder{}, false, append(diags, Diagnostic{Severity: Warning, Path: at,
			Code:    CodeWrongTypeRequired,
			Message: "folder has no `id` — dropped, nothing could reference it"})
	}
	id, ok := rawID.(string)
	if !ok {
		return Folder{}, false, append(diags, Diagnostic{Severity: Warning, Path: at + "/id",
			Code:    CodeWrongTypeRequired,
			Message: fmt.Sprintf("`id` is %s, expected a string — folder dropped", jsonKind(rawID))})
	}
	if id == "" {
		// A folder's id is a non-empty string (§3.2), so "" is a value of the
		// wrong type in a REQUIRED key, named at the key (§6.3).
		return Folder{}, false, append(diags, Diagnostic{Severity: Warning, Path: at + "/id",
			Code:    CodeWrongTypeRequired,
			Message: "folder `id` is empty, expected a non-empty string — folder dropped"})
	}
	if seen[id] {
		// Folder ids are unique (§3.2). The first folder keeps the id, so every
		// reference to it resolves to the folder a reader met first.
		return Folder{}, false, append(diags, Diagnostic{Severity: Warning, Path: at + "/id",
			Code:    CodeFolderDuplicateID,
			Message: fmt.Sprintf("folder id %q is already used by an earlier folder — this folder is dropped", id)})
	}
	seen[id] = true

	var f Folder
	f.ID = id
	f.Name, diags = stringField(obj, "name", at, diags)
	f.Parent, diags = stringField(obj, "parent", at, diags)
	f.Hidden, diags = boolField(obj, "hidden", at, diags)
	diags = reportUnknownKeys(obj, at, folderKeys, diags)
	return f, true, diags
}

// resolveParents clears any parent that cannot be walked, and says so.
//
// The choice of WHAT to drop matters. §6.2 treats a parent naming a folder
// the document does not hold as absent ([CodeFolderAbsent]), and a loop is
// broken the same way ([CodeFolderParentLoop]): the REFERENCE goes, not the
// folder. Dropping
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
		}
	}
	// §3.2, §6.2: the folders are taken in document order, and the first folder
	// of each loop has its `parent` treated as absent. Clearing it breaks that
	// loop, so no later folder of the same loop is reported. A folder whose
	// chain only LEADS INTO a loop is not in it and is not reported: following
	// its parents never comes back to it.
	for i := range folders {
		if folders[i].Parent == "" || !inParentLoop(folders, index, i) {
			continue
		}
		diags = append(diags, Diagnostic{Severity: Warning, Path: paths[i] + "/parent", Code: CodeFolderParentLoop,
			Message: fmt.Sprintf("folder %q is in a parent loop — it sits at the root", folders[i].ID)})
		folders[i].Parent = ""
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

// inParentLoop reports whether following parents from start comes back to
// start. A chain that only enters a loop never does. The walk is bounded by the
// folder count: a chain longer than that has repeated a folder.
func inParentLoop(folders []Folder, index map[string]int, start int) bool {
	cur := start
	for range folders {
		next, ok := index[folders[cur].Parent]
		if !ok || folders[cur].Parent == "" {
			return false
		}
		if next == start {
			return true
		}
		cur = next
	}
	return false
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
