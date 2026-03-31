package twidgets

import (
	"math"
	"os"
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var MapPin rune = '📍'

type Position struct {
	X float64
	Y float64
}

type Location struct {
	Position  Position
	Icon      rune
	ShortName string
	LongName  string
	Style     tcell.Style
}

type Path struct {
	Positions []Position
	// IsClosed indicates if the last position should connect to the first position.
	IsClosed bool
	Style    tcell.Style
}

type PathLayer struct {
	Name     string
	Paths    []*Path
	MinScale float64
	MaxScale float64
	Priority int

	style tcell.Style
}

func NewPathLayer(name string, paths []*Path) *PathLayer {
	return &PathLayer{
		Name:  name,
		Paths: paths,
	}
}

func (pl *PathLayer) WithScaleRange(minScale, maxScale float64) *PathLayer {
	pl.MinScale = minScale
	pl.MaxScale = maxScale
	return pl
}

func (pl *PathLayer) WithPriority(priority int) *PathLayer {
	pl.Priority = priority
	return pl
}

func (pl *PathLayer) SetStyle(style tcell.Style) *PathLayer {
	for _, p := range pl.Paths {
		p.Style = style
	}
	return pl
}

func (pl *PathLayer) PathsForViewport(viewOrigin Position, zoom, width, height float64) []*Path {
	if pl == nil {
		return nil
	}

	scale := scaleFromZoom(zoom)
	if !inScaleRange(scale, pl.MinScale, pl.MaxScale) {
		return nil
	}

	return pl.Paths
}

type PathProvider interface {
	PathsForViewport(viewOrigin Position, zoom, width, height float64) []*Path
}

type LabelMode int

const (
	LabelNone LabelMode = iota
	LabelShort
	LabelLong
)

type Map struct {
	*tview.Box

	// Origin is where the ViewOrigin is set to when the Map view is reset.
	Origin Position
	// ZoomDefault is what Zoom is set to when the Map view is reset.
	ZoomDefault float64

	// ViewOrigin is where the view is centered in world-space.
	ViewOrigin Position
	// Zoom is how many world-units one terminal cell width represents.
	Zoom float64

	// CellRatio is the cell height/width ratio in world-space terms.
	// A good default for terminal cells is often 0.5.
	CellRatio float64

	Locations []*Location
	Paths     []*Path

	PathLayers []PathProvider

	ShowIcon  bool
	LabelMode LabelMode

	PathStyle     tcell.Style
	LocationStyle tcell.Style
	LabelStyle    tcell.Style
	Background    tcell.Color

	capture func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse)

	dragging        bool
	dragStartMouseX int
	dragStartMouseY int
	dragStartOrigin Position
}

func NewMap() *Map {
	m := &Map{
		Box:           tview.NewBox(),
		Origin:        Position{X: 0, Y: 0},
		ZoomDefault:   1.0,
		ViewOrigin:    Position{X: 0, Y: 0},
		Zoom:          1.0,
		CellRatio:     2,
		ShowIcon:      true,
		LabelMode:     LabelShort,
		PathStyle:     tcell.StyleDefault.Foreground(tcell.ColorGray),
		LocationStyle: tcell.StyleDefault.Foreground(tcell.ColorWhite),
		LabelStyle:    tcell.StyleDefault.Foreground(tcell.ColorWhite),
		Background:    tcell.ColorDefault,
	}
	return m
}

func (m *Map) SetPaths(paths []*Path) *Map {
	m.Paths = paths
	return m
}

func (m *Map) SetLocations(locations []*Location) *Map {
	m.Locations = locations
	return m
}

func (m *Map) SetViewOrigin(p Position) *Map {
	m.ViewOrigin = p
	return m
}

func (m *Map) SetZoom(z float64) *Map {
	if z <= 0 {
		return m
	}
	m.Zoom = z
	return m
}

func (m *Map) SetCellRatio(r float64) *Map {
	if r <= 0 {
		return m
	}
	m.CellRatio = r
	return m
}

func (m *Map) ResetView() *Map {
	m.ViewOrigin = m.Origin
	if m.ZoomDefault > 0 {
		m.Zoom = m.ZoomDefault
	}
	return m
}

func (m *Map) ZoomBy(factor float64) *Map {
	if factor <= 0 {
		return m
	}
	m.Zoom *= factor
	if m.Zoom < 1e-9 {
		m.Zoom = 1e-9
	}
	return m
}

func (m *Map) Pan(dx, dy float64) *Map {
	m.ViewOrigin.X += dx
	m.ViewOrigin.Y += dy
	return m
}

func (m *Map) worldToCell(wx, wy float64, innerX, innerY, innerW, innerH int) (cx, cy int, ok bool) {
	if innerW <= 0 || innerH <= 0 || m.Zoom <= 0 || m.CellRatio <= 0 {
		return 0, 0, false
	}

	leftWorld := m.ViewOrigin.X - (float64(innerW) * m.Zoom / 2.0)
	topWorld := m.ViewOrigin.Y + (float64(innerH) * m.Zoom * m.CellRatio / 2.0)

	fx := (wx - leftWorld) / m.Zoom
	fy := (topWorld - wy) / (m.Zoom * m.CellRatio)

	cx = innerX + int(math.Floor(fx))
	cy = innerY + int(math.Floor(fy))
	ok = cx >= innerX && cx < innerX+innerW && cy >= innerY && cy < innerY+innerH
	return
}

// worldToSubcell converts world coordinates into a "braille subcell grid":
// each terminal cell becomes 2 columns x 4 rows.
func (m *Map) worldToSubcell(wx, wy float64, innerW, innerH int) (sx, sy float64) {
	leftWorld := m.ViewOrigin.X - (float64(innerW) * m.Zoom / 2.0)
	topWorld := m.ViewOrigin.Y + (float64(innerH) * m.Zoom * m.CellRatio / 2.0)

	cellX := (wx - leftWorld) / m.Zoom
	cellY := (topWorld - wy) / (m.Zoom * m.CellRatio)

	return cellX * 2.0, cellY * 4.0
}

type brailleCell struct {
	dots  uint8
	style tcell.Style
}

// Braille dot numbering:
//
//	1 4
//	2 5
//	3 6
//	7 8
//
// bit positions in Unicode braille block are dot-1 => bit0, dot-2 => bit1, etc.
func brailleBit(dx, dy int) uint8 {
	switch {
	case dx == 0 && dy == 0:
		return 1 << 0 // dot 1
	case dx == 0 && dy == 1:
		return 1 << 1 // dot 2
	case dx == 0 && dy == 2:
		return 1 << 2 // dot 3
	case dx == 0 && dy == 3:
		return 1 << 6 // dot 7
	case dx == 1 && dy == 0:
		return 1 << 3 // dot 4
	case dx == 1 && dy == 1:
		return 1 << 4 // dot 5
	case dx == 1 && dy == 2:
		return 1 << 5 // dot 6
	case dx == 1 && dy == 3:
		return 1 << 7 // dot 8
	default:
		return 0
	}
}

func putBrailleDot(buf map[[2]int]brailleCell, cellX, cellY, dotX, dotY int, style tcell.Style) {
	if dotX < 0 || dotX > 1 || dotY < 0 || dotY > 3 {
		return
	}
	key := [2]int{cellX, cellY}
	bc := buf[key]
	bc.dots |= brailleBit(dotX, dotY)
	bc.style = style
	buf[key] = bc
}

func rasterLineSubcells(x0, y0, x1, y1 float64, plot func(ix, iy int)) {
	dx := x1 - x0
	dy := y1 - y0
	steps := int(math.Ceil(math.Max(math.Abs(dx), math.Abs(dy))))
	if steps < 1 {
		plot(int(math.Round(x0)), int(math.Round(y0)))
		return
	}
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := x0 + dx*t
		y := y0 + dy*t
		plot(int(math.Round(x)), int(math.Round(y)))
	}
}

func (m *Map) drawPaths(screen tcell.Screen, innerX, innerY, innerW, innerH int) {
	if innerW <= 0 || innerH <= 0 {
		return
	}

	viewMinX, viewMinY, viewMaxX, viewMaxY := m.worldBounds(innerW, innerH)

	padX := m.Zoom
	padY := m.Zoom * m.CellRatio

	// padded bounds so near-edge lines still draw
	viewMinX -= padX
	viewMaxX += padX
	viewMinY -= padY
	viewMaxY += padY

	buf := make(map[[2]int]brailleCell)

	for _, path := range m.visiblePaths() {
		if path == nil || len(path.Positions) == 0 {
			continue
		}

		style := path.Style
		if style == tcell.StyleDefault {
			style = m.PathStyle
		}

		points := path.Positions
		segments := len(points) - 1
		for i := 0; i < segments; i++ {
			a := points[i]
			b := points[i+1]

			segMinX, segMinY, segMaxX, segMaxY := segmentBounds(a, b)
			if !rectsOverlap(segMinX, segMinY, segMaxX, segMaxY, viewMinX, viewMinY, viewMaxX, viewMaxY) {
				continue
			}

			x0, y0 := m.worldToSubcell(a.X, a.Y, innerW, innerH)
			x1, y1 := m.worldToSubcell(b.X, b.Y, innerW, innerH)

			rasterLineSubcells(x0, y0, x1, y1, func(ix, iy int) {
				cellX := ix / 2
				cellY := iy / 4
				dotX := ix % 2
				dotY := iy % 4

				if ix < 0 && ix%2 != 0 {
					cellX--
					dotX += 2
				}
				if iy < 0 && iy%4 != 0 {
					cellY--
					dotY += 4
				}

				if cellX < 0 || cellX >= innerW || cellY < 0 || cellY >= innerH {
					return
				}
				putBrailleDot(buf, innerX+cellX, innerY+cellY, dotX, dotY, style)
			})
		}

		if path.IsClosed && len(points) > 1 {
			a := points[len(points)-1]
			b := points[0]
			x0, y0 := m.worldToSubcell(a.X, a.Y, innerW, innerH)
			x1, y1 := m.worldToSubcell(b.X, b.Y, innerW, innerH)

			rasterLineSubcells(x0, y0, x1, y1, func(ix, iy int) {
				cellX := ix / 2
				cellY := iy / 4
				dotX := ix % 2
				dotY := iy % 4

				if ix < 0 && ix%2 != 0 {
					cellX--
					dotX += 2
				}
				if iy < 0 && iy%4 != 0 {
					cellY--
					dotY += 4
				}

				if cellX < 0 || cellX >= innerW || cellY < 0 || cellY >= innerH {
					return
				}
				putBrailleDot(buf, innerX+cellX, innerY+cellY, dotX, dotY, style)
			})
		}
	}

	// Stable draw order.
	keys := make([][2]int, 0, len(buf))
	for k := range buf {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i][1] != keys[j][1] {
			return keys[i][1] < keys[j][1]
		}
		return keys[i][0] < keys[j][0]
	})

	for _, k := range keys {
		bc := buf[k]
		if bc.dots == 0 {
			continue
		}
		r := rune(0x2800 + rune(bc.dots))
		screen.SetContent(k[0], k[1], r, nil, bc.style.Background(m.Background))
	}
}

func (m *Map) drawLocations(screen tcell.Screen, innerX, innerY, innerW, innerH int) {
	for _, loc := range m.Locations {
		if loc == nil {
			continue
		}

		x, y, ok := m.worldToCell(loc.Position.X, loc.Position.Y, innerX, innerY, innerW, innerH)
		if !ok {
			continue
		}

		style := loc.Style
		if style == tcell.StyleDefault {
			style = m.LocationStyle
		}
		style = style.Background(m.Background)

		ch := loc.Icon
		if ch == 0 {
			ch = '●'
		}
		if !m.ShowIcon {
			ch = ' '
		}

		screen.SetContent(x, y, ch, nil, style)

		var label string
		switch m.LabelMode {
		case LabelShort:
			label = loc.ShortName
		case LabelLong:
			label = loc.LongName
		default:
			label = ""
		}

		if label == "" {
			continue
		}

		labelX := x + 2
		if labelX >= innerX+innerW || y < innerY || y >= innerY+innerH {
			continue
		}

		maxWidth := innerX + innerW - labelX
		if maxWidth <= 0 {
			continue
		}

		fg, _, _ := m.LabelStyle.Decompose()
		if fg == tcell.ColorDefault {
			fg = tview.Styles.PrimaryTextColor
		}

		tview.Print(screen, label, labelX, y, maxWidth, tview.AlignLeft, fg)
	}
}

func (m *Map) Draw(screen tcell.Screen) {
	m.Box.DrawForSubclass(screen, m)

	x, y, w, h := m.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}

	bgStyle := tcell.StyleDefault.Background(m.Background)
	for row := y; row < y+h; row++ {
		for col := x; col < x+w; col++ {
			mainc, combc, style, _ := screen.GetContent(col, row)
			if mainc == 0 && len(combc) == 0 {
				screen.SetContent(col, row, ' ', nil, bgStyle)
			} else {
				screen.SetContent(col, row, mainc, combc, style.Background(m.Background))
			}
		}
	}

	m.drawPaths(screen, x, y, w, h)
	m.drawLocations(screen, x, y, w, h)
}

func (m *Map) Focus(delegate func(p tview.Primitive)) {
	if delegate != nil {
		delegate(m.Box)
	}
}

// Connect adds a path to m connecting a to b.
func (m *Map) Connect(a, b *Location) *Path {
	path := &Path{
		Positions: []Position{a.Position, b.Position},
	}

	m.Paths = append(m.Paths, path)

	return path
}

func (m *Map) HasFocus() bool {
	return m.Box.HasFocus()
}

func (m *Map) Blur() {
	m.Box.Blur()
}

func (m *Map) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return m.WrapInputHandler(func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
		switch event.Key() {
		case tcell.KeyUp:
			m.Pan(0, -m.Zoom*m.CellRatio*2)
		case tcell.KeyDown:
			m.Pan(0, m.Zoom*m.CellRatio*2)
		case tcell.KeyLeft:
			m.Pan(m.Zoom*2, 0)
		case tcell.KeyRight:
			m.Pan(-m.Zoom*2, 0)
		case tcell.KeyRune:
			switch event.Rune() {
			case '+', '=':
				m.ZoomBy(0.8) // zoom in
			case '-', '_':
				m.ZoomBy(1.25) // zoom out
			case '0':
				m.ResetView()
			case 'i':
				m.ShowIcon = !m.ShowIcon
			case 'l':
				m.LabelMode = (m.LabelMode + 1) % 3
			}
		}
	})
}

func (m *Map) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return m.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		if m.capture != nil {
			action, event = m.capture(action, event)
		}

		x, y := event.Position()
		rectX, rectY, rectW, rectH := m.GetRect()
		if x < rectX || x >= rectX+rectW || y < rectY || y >= rectY+rectH {
			// If the mouse leaves the widget while dragging, keep consuming motion.
			if m.dragging && (action == tview.MouseMove || action == tview.MouseLeftUp) {
				switch action {
				case tview.MouseMove:
					m.updateDrag(x, y)
				case tview.MouseLeftUp:
					m.endDrag()
				}
				return true, m
			}
			return false, nil
		}

		setFocus(m)

		switch action {
		case tview.MouseLeftDown:
			m.beginDrag(x, y)
			return true, m

		case tview.MouseMove:
			if m.dragging {
				m.updateDrag(x, y)
				return true, m
			}

		case tview.MouseLeftUp:
			if m.dragging {
				m.endDrag()
				return true, m
			}

		case tview.MouseScrollUp:
			m.ZoomBy(0.9)
			return true, m

		case tview.MouseScrollDown:
			m.ZoomBy(1.1)
			return true, m
		}

		return true, m
	})
}

func (m *Map) SetMouseCapture(capture func(action tview.MouseAction, event *tcell.EventMouse) (tview.MouseAction, *tcell.EventMouse)) *Map {
	m.capture = capture
	return m
}

func (m *Map) beginDrag(mouseX, mouseY int) {
	m.dragging = true
	m.dragStartMouseX = mouseX
	m.dragStartMouseY = mouseY
	m.dragStartOrigin = m.ViewOrigin
}

func (m *Map) updateDrag(mouseX, mouseY int) {
	if !m.dragging {
		return
	}

	dxCells := mouseX - m.dragStartMouseX
	dyCells := mouseY - m.dragStartMouseY

	// Dragging right should move the map content right under the cursor,
	// which means the view origin moves left in world space.
	m.ViewOrigin.X = m.dragStartOrigin.X - float64(dxCells)*m.Zoom
	m.ViewOrigin.Y = m.dragStartOrigin.Y + float64(dyCells)*m.Zoom*m.CellRatio
}

func (m *Map) endDrag() {
	m.dragging = false
}

func (m *Map) worldBounds(innerW, innerH int) (minX, minY, maxX, maxY float64) {
	halfW := float64(innerW) * m.Zoom / 2.0
	halfH := float64(innerH) * m.Zoom * m.CellRatio / 2.0

	minX = m.ViewOrigin.X - halfW
	maxX = m.ViewOrigin.X + halfW
	minY = m.ViewOrigin.Y - halfH
	maxY = m.ViewOrigin.Y + halfH
	return
}

func (m *Map) visiblePaths() []*Path {
	var out []*Path

	// Base paths, if you still keep them.
	out = append(out, m.Paths...)

	if len(m.PathLayers) == 0 {
		return out
	}

	type provided struct {
		priority int
		order    int
		paths    []*Path
	}

	var batches []provided

	x, y, w, h := m.GetInnerRect()
	_ = x
	_ = y

	for i, provider := range m.PathLayers {
		if provider == nil {
			continue
		}

		paths := provider.PathsForViewport(
			m.ViewOrigin,
			m.Zoom,
			float64(w),
			float64(h),
		)
		if len(paths) == 0 {
			continue
		}

		priority := 0
		if pl, ok := provider.(*PathLayer); ok && pl != nil {
			priority = pl.Priority
		}

		batches = append(batches, provided{
			priority: priority,
			order:    i,
			paths:    paths,
		})
	}

	sort.SliceStable(batches, func(i, j int) bool {
		if batches[i].priority != batches[j].priority {
			return batches[i].priority < batches[j].priority
		}
		return batches[i].order < batches[j].order
	})

	for _, batch := range batches {
		out = append(out, batch.paths...)
	}

	return out
}

func (m *Map) AddPathProvider(p PathProvider) *Map {
	if p != nil {
		m.PathLayers = append(m.PathLayers, p)
	}
	return m
}

func (m *Map) AddPathLayer(layer *PathLayer) *Map {
	if layer != nil {
		m.PathLayers = append(m.PathLayers, layer)
	}
	return m
}

func (m *Map) LoadGeoJSONPathLayer(filename string, layer *PathLayer, opts GeoJSONOptions) error {
	paths, err := LoadGeoJSONPathsFile(filename, opts)
	if err != nil {
		return err
	}

	if layer == nil {
		layer = &PathLayer{}
	}
	layer.Paths = append(layer.Paths, paths...)

	m.AddPathLayer(layer)
	return nil
}

func LoadGeoJSONPathsFile(filename string, opts GeoJSONOptions) ([]*Path, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return LoadGeoJSONPaths(f, opts)
}

func segmentBounds(a, b Position) (minX, minY, maxX, maxY float64) {
	minX, maxX = a.X, b.X
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	minY, maxY = a.Y, b.Y
	if minY > maxY {
		minY, maxY = maxY, minY
	}
	return
}

func rectsOverlap(aMinX, aMinY, aMaxX, aMaxY, bMinX, bMinY, bMaxX, bMaxY float64) bool {
	return aMinX <= bMaxX && aMaxX >= bMinX && aMinY <= bMaxY && aMaxY >= bMinY
}

func scaleFromZoom(zoom float64) float64 {
	if zoom <= 0 {
		return 0
	}
	return 1.0 / zoom
}

func inScaleRange(scale, minScale, maxScale float64) bool {
	if minScale > 0 && scale < minScale {
		return false
	}
	if maxScale > 0 && scale > maxScale {
		return false
	}
	return true
}
