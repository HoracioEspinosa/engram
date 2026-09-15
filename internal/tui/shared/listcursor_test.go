package shared

import "testing"

func TestListCursorMovesAndDragsItsWindow(t *testing.T) {
	c := ListCursor{}

	c = c.Move(1, 10, 3)
	if c.Index != 1 || c.Offset != 0 {
		t.Fatalf("after one step down = %+v, want the window still at the top", c)
	}

	c = c.Move(1, 10, 3).Move(1, 10, 3)
	if c.Index != 3 || c.Offset != 1 {
		t.Fatalf("past the window's end = %+v, want the window pulled along", c)
	}

	c = c.Move(-3, 10, 3)
	if c.Index != 0 || c.Offset != 0 {
		t.Fatalf("back to the top = %+v", c)
	}
}

func TestListCursorStopsAtBothEnds(t *testing.T) {
	if c := (ListCursor{}).Move(-1, 5, 3); c.Index != 0 {
		t.Fatalf("up from the first row = %+v, want it to stay put rather than wrap", c)
	}
	if c := (ListCursor{Index: 4, Offset: 2}).Move(1, 5, 3); c.Index != 4 {
		t.Fatalf("down from the last row = %+v, want it to stay put", c)
	}
}

func TestListCursorTopAndBottom(t *testing.T) {
	if c := (ListCursor{Index: 7, Offset: 5}).Top(); c.Index != 0 || c.Offset != 0 {
		t.Fatalf("Top = %+v", c)
	}
	c := (ListCursor{}).Bottom(10, 3)
	if c.Index != 9 || c.Offset != 7 {
		t.Fatalf("Bottom = %+v, want the last row with the window over it", c)
	}
	if c := (ListCursor{Index: 3}).Bottom(0, 3); c.Index != 0 || c.Offset != 0 {
		t.Fatalf("Bottom of an empty list = %+v", c)
	}
}

func TestListCursorClampsAfterAShorterReload(t *testing.T) {
	// The page that came back is shorter than the one the cursor was in.
	c := ListCursor{Index: 40, Offset: 38}.Clamp(3, 5)
	if c.Index != 2 || c.Offset != 0 {
		t.Fatalf("Clamp = %+v, want the pair inside the three rows that are left", c)
	}
	if c := (ListCursor{Index: 2}).Clamp(0, 5); c.Index != 0 || c.Offset != 0 {
		t.Fatalf("Clamp of an empty list = %+v", c)
	}
	if c := (ListCursor{Index: -4, Offset: -2}).Clamp(5, 2); c.Index != 0 || c.Offset != 0 {
		t.Fatalf("Clamp of a negative pair = %+v", c)
	}
	// A window taller than the list keeps the offset at the top.
	if c := (ListCursor{Index: 1}).Clamp(2, 10); c.Offset != 0 {
		t.Fatalf("Clamp with a window taller than the list = %+v", c)
	}
	// A window of zero still behaves as one row.
	if c := (ListCursor{Index: 3}).Clamp(5, 0); c.Index != 3 || c.Offset != 3 {
		t.Fatalf("Clamp with no visible rows = %+v", c)
	}
}

func TestListCursorWindow(t *testing.T) {
	from, to := ListCursor{Offset: 2}.Window(10, 3)
	if from != 2 || to != 5 {
		t.Fatalf("Window = [%d,%d), want [2,5)", from, to)
	}
	if from, to := (ListCursor{Offset: 8}).Window(10, 5); from != 8 || to != 10 {
		t.Fatalf("Window past the end = [%d,%d), want [8,10)", from, to)
	}
	if from, to := (ListCursor{Offset: 40}).Window(10, 5); from != 10 || to != 10 {
		t.Fatalf("Window from an offset past the list = [%d,%d)", from, to)
	}
	if from, to := (ListCursor{Offset: -1}).Window(10, 5); from != 0 || to != 5 {
		t.Fatalf("Window from a negative offset = [%d,%d)", from, to)
	}
	if from, to := (ListCursor{}).Window(0, 5); from != 0 || to != 0 {
		t.Fatalf("Window of an empty list = [%d,%d)", from, to)
	}
	if from, to := (ListCursor{}).Window(5, 0); from != 0 || to != 0 {
		t.Fatalf("Window with no visible rows = [%d,%d)", from, to)
	}
}

func TestListCursorUnpack(t *testing.T) {
	index, offset := ListCursor{Index: 3, Offset: 1}.Unpack()
	if index != 3 || offset != 1 {
		t.Fatalf("Unpack = (%d, %d)", index, offset)
	}
}
