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

func decodeFolders(arr []any, diags []Diagnostic) ([]Folder, []Diagnostic) {
	out := make([]Folder, 0, len(arr))
	seen := make(map[string]bool, len(arr))

	for i, item := range arr {
		at := fmt.Sprintf("/folders/%d", i)
		obj, ok := item.(map[string]any)
		if !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("folder is %s, expected an object — dropped", jsonKind(item))})
			continue
		}
		id, _ := obj["id"].(string)
		if id == "" {
			// Without an id nothing can reference it, so the folder cannot hold
			// anything. Unlike a dangling parent, dropping it loses nothing that
			// was reachable.
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: "folder has no `id` — dropped, nothing could reference it"})
			continue
		}
		if seen[id] {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("duplicate folder id %q — dropped, the first one wins", id)})
			continue
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
		out = append(out, f)
	}

	return resolveParents(out, diags)
}

// resolveParents clears any parent that cannot be walked, and says so.
//
// The choice of WHAT to drop matters. A dangling or circular parent is an
// unusable value, and §6.2 drops the offending element — which is the
// REFERENCE, not the folder. Dropping the folder would take everything filed
// under it with it; clearing the parent loses only the folder's position in the
// tree. Losing a position is recoverable by hand; losing content is not.
func resolveParents(folders []Folder, diags []Diagnostic) ([]Folder, []Diagnostic) {
	index := make(map[string]int, len(folders))
	for i, f := range folders {
		index[f.ID] = i
	}
	for i := range folders {
		if folders[i].Parent == "" {
			continue
		}
		at := fmt.Sprintf("/folders/%d/parent", i)
		if _, ok := index[folders[i].Parent]; !ok {
			diags = append(diags, Diagnostic{Severity: Warning, Path: at,
				Message: fmt.Sprintf("parent %q is not in this document — folder kept at the root", folders[i].Parent)})
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
