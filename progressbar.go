package twidgets

import (
	"math"
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
	"github.com/rivo/tview"
)

type AlignH int
type AlignV int
type Orientation int

const (
	AlignLeft AlignH = iota
	AlignCenter
	AlignRight
)

const (
	AlignTop AlignV = iota
	AlignMiddle
	AlignBottom
)

const (
	Horizontal Orientation = iota
	Vertical
)

// ProgressBar is a tview primitive that renders a progress indicator with optional end-caps,
// alignment control, and partial-block resolution.
type ProgressBar struct {
	*tview.Box

	mu sync.RWMutex

	orientation Orientation
	alignH      AlignH
	alignV      AlignV

	// progress in [0,1]
	value float64

	// Optional end caps.
	// If empty, not drawn. If set, assumed to take 1 cell each (best with single-rune caps).
	startCap string
	endCap   string

	// Styles
	filledStyle tcell.Style
	emptyStyle  tcell.Style
	capStyle    tcell.Style

	// Whether to use partial blocks (default true).
	partial bool
}

func NewProgressBar() *ProgressBar {
	return &ProgressBar{
		Box:         tview.NewBox(),
		orientation: Horizontal,
		alignH:      AlignCenter,
		alignV:      AlignMiddle,
		value:       0,
		startCap:    "",
		endCap:      "",
		filledStyle: tcell.StyleDefault,
		emptyStyle:  tcell.StyleDefault,
		capStyle:    tcell.StyleDefault,
		partial:     true,
	}
}

func (p *ProgressBar) SetOrientation(o Orientation) *ProgressBar {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.orientation = o
	return p
}

func (p *ProgressBar) SetAlign(h AlignH, v AlignV) *ProgressBar {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.alignH, p.alignV = h, v
	return p
}

// SetProgress sets progress in [0,1]. Values are clamped.
func (p *ProgressBar) SetProgress(v float64) *ProgressBar {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	p.value = v
	return p
}

func (p *ProgressBar) GetProgress() float64 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.value
}

// SetEndCaps sets optional caps. Use "" for none.
// Best results with single-cell glyphs.
func (p *ProgressBar) SetEndCaps(start, end string) *ProgressBar {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.startCap, p.endCap = start, end
	return p
}

func (p *ProgressBar) SetStyles(filled, empty, caps tcell.Style) *ProgressBar {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.filledStyle, p.emptyStyle, p.capStyle = filled, empty, caps
	return p
}

func (p *ProgressBar) SetPartialBlocks(enabled bool) *ProgressBar {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.partial = enabled
	return p
}

func (p *ProgressBar) Draw(screen tcell.Screen) {
	p.Box.DrawForSubclass(screen, p)

	p.mu.RLock()
	orientation := p.orientation
	alignH := p.alignH
	alignV := p.alignV
	value := p.value
	startCap := p.startCap
	endCap := p.endCap
	filledStyle := p.filledStyle
	emptyStyle := p.emptyStyle
	capStyle := p.capStyle
	partial := p.partial
	p.mu.RUnlock()

	x, y, w, h := p.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}

	// Fill background / empty area first, so the bar can be "thin" within a larger rect.
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			screen.SetContent(x+col, y+row, ' ', nil, emptyStyle)
		}
	}

	// Bar thickness is 1 cell: one row (horizontal) or one column (vertical).
	barX, barY := x, y
	barW, barH := w, h
	if orientation == Horizontal {
		barH = 1
		switch alignV {
		case AlignTop:
			barY = y
		case AlignMiddle:
			barY = y + (h-1)/2
		case AlignBottom:
			barY = y + (h - 1)
		}
	} else {
		barW = 1
		switch alignH {
		case AlignLeft:
			barX = x
		case AlignCenter:
			barX = x + (w-1)/2
		case AlignRight:
			barX = x + (w - 1)
		}
	}

	// Determine cap widths (cell width).
	startCapW := capWidth(startCap)
	endCapW := capWidth(endCap)

	// Available length for fill cells.
	var length int
	if orientation == Horizontal {
		length = barW - startCapW - endCapW
	} else {
		length = barH - startCapW - endCapW
	}
	if length <= 0 {
		// Not enough space to draw anything meaningful; still try to draw caps if possible.
		drawCaps(screen, barX, barY, barW, barH, orientation, startCap, endCap, capStyle)
		return
	}

	// Place caps (if any) and compute the fill region origin.
	fillX, fillY := barX, barY
	if orientation == Horizontal {
		// Align the whole bar line horizontally within barW if you want caps + fill to float;
		// but usually barX already represents the "line", so we only align fill within available width.
		// We'll still honor alignH by shifting the entire bar content if rect wider than needed.
		needed := startCapW + length + endCapW
		shift := 0
		switch alignH {
		case AlignLeft:
			shift = 0
		case AlignCenter:
			shift = (barW - needed) / 2
		case AlignRight:
			shift = (barW - needed)
		}
		barX += shift
		fillX = barX + startCapW

		// Draw start cap
		if startCapW > 0 {
			drawStringCell(screen, barX, barY, startCap, capStyle)
		}
		// Draw end cap
		if endCapW > 0 {
			drawStringCell(screen, barX+startCapW+length, barY, endCap, capStyle)
		}

		// Draw the fill region
		drawHorizontalFill(screen, fillX, fillY, length, value, filledStyle, emptyStyle, partial)
	} else {
		// Vertical: honor alignV by shifting the whole bar content within height
		needed := startCapW + length + endCapW
		shift := 0
		switch alignV {
		case AlignTop:
			shift = 0
		case AlignMiddle:
			shift = (barH - needed) / 2
		case AlignBottom:
			shift = (barH - needed)
		}
		barY += shift
		fillY = barY + startCapW

		// Draw start cap at top
		if startCapW > 0 {
			drawStringCell(screen, barX, barY, startCap, capStyle)
		}
		// Draw end cap at bottom
		if endCapW > 0 {
			drawStringCell(screen, barX, barY+startCapW+length, endCap, capStyle)
		}

		drawVerticalFill(screen, barX, fillY, length, value, filledStyle, emptyStyle, partial)
	}
}

func capWidth(s string) int {
	if s == "" {
		return 0
	}
	// This returns display width. If it's not 1, we still *reserve* that many cells,
	// but the renderer draws the string starting at the first cell.
	// Best: use single-cell glyphs for caps.
	return runewidth.StringWidth(s)
}

func drawStringCell(screen tcell.Screen, x, y int, s string, st tcell.Style) {
	if s == "" {
		return
	}
	// Draw runes left-to-right; truncate if too wide for intended region.
	// For caps, you should keep s to a single-cell glyph anyway.
	for _, r := range s {
		screen.SetContent(x, y, r, nil, st)
		x += runewidth.RuneWidth(r)
	}
}

func drawCaps(screen tcell.Screen, x, y, w, h int, o Orientation, start, end string, st tcell.Style) {
	_ = w
	_ = h
	if o == Horizontal {
		if start != "" {
			drawStringCell(screen, x, y, start, st)
		}
		if end != "" {
			drawStringCell(screen, x+w-capWidth(end), y, end, st)
		}
	} else {
		if start != "" {
			drawStringCell(screen, x, y, start, st)
		}
		if end != "" {
			drawStringCell(screen, x, y+h-capWidth(end), end, st)
		}
	}
}

// Horizontal partial blocks: 0..8
var hBlocks = []rune{' ', '▏', '▎', '▍', '▌', '▋', '▊', '▉', '█'}

// Vertical partial blocks (bottom fill): 0..8
var vBlocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func drawHorizontalFill(screen tcell.Screen, x, y, length int, value float64, filled, empty tcell.Style, partial bool) {
	if length <= 0 {
		return
	}

	// Total "subcells" in eighths
	total := float64(length)
	if partial {
		total *= 8.0
	}
	filledUnits := value * total

	fullCells := 0
	remainder := 0

	if partial {
		fullCells = int(math.Floor(filledUnits / 8.0))
		remainder = int(math.Round(filledUnits - float64(fullCells)*8.0))
		if remainder < 0 {
			remainder = 0
		} else if remainder > 8 {
			remainder = 8
		}
	} else {
		fullCells = int(math.Round(value * float64(length)))
		if fullCells < 0 {
			fullCells = 0
		} else if fullCells > length {
			fullCells = length
		}
		remainder = 0
	}

	// Draw full cells
	for i := 0; i < length; i++ {
		if i < fullCells {
			screen.SetContent(x+i, y, '█', nil, filled)
		} else {
			screen.SetContent(x+i, y, ' ', nil, empty)
		}
	}

	// Draw remainder cell (only if partial and there is room)
	if partial && remainder > 0 && fullCells < length {
		screen.SetContent(x+fullCells, y, hBlocks[remainder], nil, filled)
		// Remaining portion should look empty; leaving the rest as empty background is fine.
	}
}

func drawVerticalFill(screen tcell.Screen, x, y, length int, value float64, filled, empty tcell.Style, partial bool) {
	if length <= 0 {
		return
	}

	// We'll fill from bottom to top within the fill region.
	total := float64(length)
	if partial {
		total *= 8.0
	}
	filledUnits := value * total

	fullCells := 0
	remainder := 0

	if partial {
		fullCells = int(math.Floor(filledUnits / 8.0))
		remainder = int(math.Round(filledUnits - float64(fullCells)*8.0))
		if remainder < 0 {
			remainder = 0
		} else if remainder > 8 {
			remainder = 8
		}
	} else {
		fullCells = int(math.Round(value * float64(length)))
		if fullCells < 0 {
			fullCells = 0
		} else if fullCells > length {
			fullCells = length
		}
		remainder = 0
	}

	// Draw empty first
	for i := 0; i < length; i++ {
		screen.SetContent(x, y+i, ' ', nil, empty)
	}

	// Full cells from bottom upward
	for i := 0; i < fullCells; i++ {
		screen.SetContent(x, y+(length-1-i), '█', nil, filled)
	}

	// Remainder cell sits above the full cells (i.e., next cell up from bottom-fill)
	if partial && remainder > 0 && fullCells < length {
		screen.SetContent(x, y+(length-1-fullCells), vBlocks[remainder], nil, filled)
	}
}
