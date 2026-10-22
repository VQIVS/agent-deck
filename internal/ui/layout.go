package ui

// Rect is a terminal-cell rectangle used for layout and hit testing.
type Rect struct{ X, Y, W, H int }

// Layout reserves full width for master and divides the remaining rows evenly.
// Very small terminals switch to focus-only rendering in View.
func Layout(w, h, workers int) []Rect {
	w = max(1, w)
	h = max(1, h)
	workers = max(0, workers)
	if workers == 0 {
		return []Rect{{0, 0, w, h}}
	}
	cols := min(workers, max(1, w/44))
	rows := (workers + cols - 1) / cols
	top := max(1, h/(rows+1))
	out := []Rect{{0, 0, w, top}}
	for i := 0; i < workers; i++ {
		r, c := i/cols, i%cols
		count := min(cols, workers-r*cols)
		x := c * w / count
		right := (c + 1) * w / count
		y := top + r*(h-top)/rows
		bottom := top + (r+1)*(h-top)/rows
		out = append(out, Rect{x, y, right - x, bottom - y})
	}
	return out
}

// Grid lays out equal-role panes, or reserves a full-width first pane for a lead.
func Grid(w, h, count int, lead bool) []Rect {
	if count <= 0 {
		return nil
	}
	if lead {
		return Layout(w, h, count-1)
	}
	w = max(1, w)
	h = max(1, h)
	cols := min(count, max(1, w/44))
	rows := (count + cols - 1) / cols
	var out []Rect
	for i := 0; i < count; i++ {
		r, c := i/cols, i%cols
		n := min(cols, count-r*cols)
		x := c * w / n
		y := r * h / rows
		out = append(out, Rect{x, y, (c+1)*w/n - x, (r+1)*h/rows - y})
	}
	return out
}

func Capacity(w, h int, lead bool) int {
	cols := max(1, w/44)
	rows := max(1, h/11)
	if lead {
		return max(1, 1+cols*(rows-1))
	}
	return max(1, cols*rows)
}
