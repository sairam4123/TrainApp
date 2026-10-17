package tui

import "trainapp/visualization/schematic"

// ZoomStep is a level of detail and a zoom at it.
type ZoomStep struct {
	Level schematic.Level
	Zoom  int
}

// ZoomSteps run from the overview to the most detailed drawing.
var ZoomSteps = []ZoomStep{
	{schematic.Overview, 1},
	{schematic.Layout, 1},
	{schematic.Layout, 2},
	{schematic.Layout, 3},
	{schematic.Detail, 1},
	{schematic.Detail, 2},
}

// Camera is a pane's view of a drawn schematic: the zoom step it is drawn
// at, and the frame cell (X, Y) at the pane's top left. The owner draws the
// frame (Draw) and tells the camera the pane's size (W, H).
type Camera struct {
	Step  int
	X, Y  int
	W, H  int
	Frame *schematic.Frame

	// Draw draws the schematic at a zoom step, with whatever else the owner
	// shows on it (state, marks).
	Draw func(ZoomStep) *schematic.Frame
}

// NewCamera starts at the first layout step.
func NewCamera(draw func(ZoomStep) *schematic.Frame) *Camera {
	c := &Camera{Step: 1, Draw: draw, W: 1, H: 1}
	c.Redraw()
	return c
}

// Redraw draws the frame again at the camera's step.
func (c *Camera) Redraw() {
	c.Frame = c.Draw(ZoomSteps[c.Step])
	c.Clamp()
}

// Clamp keeps the frame on screen, centred if it is smaller than the pane.
func (c *Camera) Clamp() { c.X, c.Y = c.Clamped(c.X, c.Y) }

// Clamped is where the view at (x, y) is kept to.
func (c *Camera) Clamped(x, y int) (int, int) {
	fw, fh := c.Frame.Width(), c.Frame.Height()
	if fw <= c.W {
		x = (fw - c.W) / 2
	} else {
		x = min(max(x, 0), fw-c.W)
	}
	if fh <= c.H {
		y = (fh - c.H) / 2
	} else {
		y = min(max(y, 0), fh-c.H)
	}
	return x, y
}

// Pan moves the view by (dx, dy) cells.
func (c *Camera) Pan(dx, dy int) {
	c.X, c.Y = c.X+dx, c.Y+dy
	c.Clamp()
}

// ZoomTo changes the zoom step, keeping the place at pane cell (sx, sy)
// where it is.
func (c *Camera) ZoomTo(step, sx, sy int) {
	step = min(max(step, 0), len(ZoomSteps)-1)
	if step == c.Step {
		return
	}
	a := c.Frame.AnchorAt(c.X + sx)
	fy := float64(c.Y+sy) / float64(max(c.Frame.Height(), 1))
	c.Step = step
	c.Frame = c.Draw(ZoomSteps[c.Step])
	c.X = c.Frame.Column(a) - sx
	c.Y = int(fy*float64(c.Frame.Height())) - sy
	c.Clamp()
}

// Fit picks the widest layout step that fits the pane, or the overview.
func (c *Camera) Fit() {
	c.Step = 0
	for i := len(ZoomSteps) - 1; i > 0; i-- {
		if ZoomSteps[i].Level != schematic.Layout {
			continue
		}
		if f := c.Draw(ZoomSteps[i]); f.Width() <= c.W && f.Height() <= c.H {
			c.Step = i
			break
		}
	}
	c.Redraw()
}

// CentreOn puts frame cell (x, y) in the middle of the pane.
func (c *Camera) CentreOn(x, y int) {
	c.X, c.Y = x-c.W/2, y-c.H/2
	c.Clamp()
}

// Show moves the view as little as it takes to have frame cell (x, y) on
// the pane, a margin of cells from its edges if there is room.
func (c *Camera) Show(x, y, margin int) {
	mx, my := min(margin, c.W/4), min(margin/2, c.H/4)
	if x < c.X+mx {
		c.X = x - mx
	} else if x >= c.X+c.W-mx {
		c.X = x - c.W + mx + 1
	}
	if y < c.Y+my {
		c.Y = y - my
	} else if y >= c.Y+c.H-my {
		c.Y = y - c.H + my + 1
	}
	c.Clamp()
}

// Lines are the pane's rows of the frame.
func (c *Camera) Lines(color bool) []string {
	return c.Frame.View(c.X, c.Y, c.W, c.H, color)
}

// FrameCell is the frame cell under pane cell (sx, sy).
func (c *Camera) FrameCell(sx, sy int) (int, int) { return c.X + sx, c.Y + sy }
