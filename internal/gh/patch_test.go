package gh

import "testing"

// Patch with one hunk: additions, deletions, context.
const singleHunkPatch = `@@ -10,6 +10,8 @@ def handler(request):
 context_a
 context_b
-old_line
+new_line_1
+new_line_2
 context_c
+new_line_3
 context_d`

func TestBuildPositionMapSingleHunk(t *testing.T) {
	got := BuildPositionMap(singleHunkPatch)

	want := map[int]int{
		10: 1, // context_a
		11: 2, // context_b
		// position 3 is the deleted old_line: no new-side entry
		12: 4, // new_line_1
		13: 5, // new_line_2
		14: 6, // context_c
		15: 7, // new_line_3
		16: 8, // context_d
	}

	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for line, pos := range want {
		if got[line] != pos {
			t.Errorf("line %d: got position %d, want %d", line, got[line], pos)
		}
	}
}

// Two hunks: the second "@@" line itself consumes a position.
const multiHunkPatch = `@@ -1,3 +1,4 @@
 a
+b
 c
 d
@@ -20,3 +21,4 @@ class Foo:
 x
+y
 z
 w`

func TestBuildPositionMapMultiHunk(t *testing.T) {
	got := BuildPositionMap(multiHunkPatch)

	want := map[int]int{
		// hunk 1, new side starts at line 1
		1: 1, // a
		2: 2, // +b
		3: 3, // c
		4: 4, // d
		// position 5 is the second @@ header
		21: 6, // x
		22: 7, // +y
		23: 8, // z
		24: 9, // w
	}

	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for line, pos := range want {
		if got[line] != pos {
			t.Errorf("line %d: got position %d, want %d", line, got[line], pos)
		}
	}
}

func TestBuildPositionMapNoNewlineMarker(t *testing.T) {
	patch := "@@ -1,2 +1,2 @@\n a\n-old\n+new\n\\ No newline at end of file"
	got := BuildPositionMap(patch)

	want := map[int]int{
		1: 1, // a
		2: 3, // +new (position 2 is the deletion)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %v", len(got), len(want), got)
	}
	for line, pos := range want {
		if got[line] != pos {
			t.Errorf("line %d: got position %d, want %d", line, got[line], pos)
		}
	}
}

func TestBuildPositionMapEmptyPatch(t *testing.T) {
	if got := BuildPositionMap(""); len(got) != 0 {
		t.Errorf("empty patch: want empty map, got %v", got)
	}
}

func TestBuildPositionMapLineOutsideDiff(t *testing.T) {
	got := BuildPositionMap(singleHunkPatch)
	if _, ok := got[999]; ok {
		t.Error("line 999 is outside the diff, must have no position")
	}
	if _, ok := got[9]; ok {
		t.Error("line 9 precedes the hunk, must have no position")
	}
}
