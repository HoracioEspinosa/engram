package shared

// ListCursor is the selection and the scroll offset of a list, and the
// arithmetic that keeps them consistent.
//
// Every list screen used to carry its own copy of the same four rules — move
// the cursor, pull the window after it, clamp both when the data changes,
// jump to either end — and a copy that drifted showed a cursor outside the
// rows it had drawn. The rules live here once; a screen keeps the two
// numbers and asks for the next pair.
type ListCursor struct {
	// Index is the selected row.
	Index int
	// Offset is the first row drawn.
	Offset int
}

// Move steps the cursor by delta over count rows, dragging the window of
// visible rows along with it. It stops at either end rather than wrapping: a
// list that wraps loses the reader's place.
func (c ListCursor) Move(delta, count, visible int) ListCursor {
	c.Index += delta
	return c.Clamp(count, visible)
}

// Top selects the first row.
func (c ListCursor) Top() ListCursor { return ListCursor{} }

// Bottom selects the last row.
func (c ListCursor) Bottom(count, visible int) ListCursor {
	if count <= 0 {
		return ListCursor{}
	}
	return ListCursor{Index: count - 1}.Clamp(count, visible)
}

// Clamp brings the pair back inside count rows and pulls the window over the
// cursor. A screen calls it after a reload: the page that came back may be
// shorter than the one the cursor was sitting in.
func (c ListCursor) Clamp(count, visible int) ListCursor {
	if count <= 0 {
		return ListCursor{}
	}
	if visible < 1 {
		visible = 1
	}

	if c.Index < 0 {
		c.Index = 0
	}
	if c.Index > count-1 {
		c.Index = count - 1
	}

	if c.Offset > c.Index {
		c.Offset = c.Index
	}
	if c.Index >= c.Offset+visible {
		c.Offset = c.Index - visible + 1
	}
	if maxOffset := count - visible; c.Offset > maxOffset {
		c.Offset = maxOffset
	}
	if c.Offset < 0 {
		c.Offset = 0
	}
	return c
}

// Window is the half-open range of rows to draw: [from, to).
func (c ListCursor) Window(count, visible int) (from, to int) {
	if count <= 0 || visible <= 0 {
		return 0, 0
	}
	from = c.Offset
	if from < 0 {
		from = 0
	}
	if from > count {
		from = count
	}
	to = from + visible
	if to > count {
		to = count
	}
	return from, to
}

// Unpack returns the pair as the two plain ints a screen keeps, so a handler
// reads as one assignment instead of four.
func (c ListCursor) Unpack() (index, offset int) { return c.Index, c.Offset }
