package twidgets

import (
	"math"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type ScrollBar struct {
	*tview.Box

	horizontal bool

	startRune rune
	midRune   rune
	endRune   rune
	thumbRune rune

	style      tcell.Style
	thumbStyle tcell.Style

	min      int
	max      int
	position int
	pageSize int

	lineStep int
	pageStep int

	disabled bool
	autoHide bool

	dragging   bool
	dragOffset int

	changed func(min, max, position int)
}

func NewScrollBar() *ScrollBar {
	sb := &ScrollBar{
		Box:        tview.NewBox(),
		startRune:  '▲',
		midRune:    '┊',
		endRune:    '▼',
		thumbRune:  '┃',
		style:      tcell.StyleDefault,
		thumbStyle: tcell.StyleDefault,
		pageSize:   1,
		lineStep:   1,
		pageStep:   10,
	}
	// sb.SetMouseCapture(sb.mouseCapture)
	return sb
}

func (s *ScrollBar) SetHorizontal(horizontal bool) *ScrollBar {
	s.horizontal = horizontal
	if horizontal {
		s.startRune = '◀'
		s.midRune = '┄'
		s.endRune = '▶'
		s.thumbRune = '━'
	}
	return s
}

func (s *ScrollBar) SetDecor(start, mid, end rune) *ScrollBar {
	s.startRune = start
	s.midRune = mid
	s.endRune = end
	return s
}

func (s *ScrollBar) SetThumbRune(r rune) *ScrollBar {
	s.thumbRune = r
	return s
}

func (s *ScrollBar) SetStyle(style tcell.Style) *ScrollBar {
	s.style = style
	return s
}

func (s *ScrollBar) SetThumbStyle(style tcell.Style) *ScrollBar {
	s.thumbStyle = style
	return s
}

func (s *ScrollBar) SetDisabled(disabled bool) *ScrollBar {
	s.disabled = disabled
	return s
}

func (s *ScrollBar) SetAutoHide(autoHide bool) *ScrollBar {
	s.autoHide = autoHide
	return s
}

func (s *ScrollBar) SetSteps(lineStep, pageStep int) *ScrollBar {
	if lineStep > 0 {
		s.lineStep = lineStep
	}
	if pageStep > 0 {
		s.pageStep = pageStep
	}
	return s
}

func (s *ScrollBar) SetChangedFunc(fn func(min, max, position int)) *ScrollBar {
	s.changed = fn
	return s
}

func (s *ScrollBar) SetScrollFunc(fn func(min, max, position int)) *ScrollBar {
	// Alias for your proposed API.
	s.changed = fn
	return s
}

func (s *ScrollBar) SetUpdateFunc(fn func(min, max, position int)) *ScrollBar {
	// Not a callback in this implementation.
	// Retained only to match your proposed shape if you want compatibility.
	// Prefer SetRange / SetPageSize instead.
	return s
}

func (s *ScrollBar) SetRange(min, max, position int) *ScrollBar {
	s.min = min
	s.max = max
	s.position = position
	s.clamp()
	return s
}

func (s *ScrollBar) SetPageSize(pageSize int) *ScrollBar {
	if pageSize < 1 {
		pageSize = 1
	}
	s.pageSize = pageSize
	s.clamp()
	return s
}

func (s *ScrollBar) GetRange() (min, max, position int) {
	return s.min, s.max, s.position
}

func (s *ScrollBar) clamp() {
	if s.max < s.min {
		s.max = s.min
	}
	maxPos := s.maxPosition()
	if s.position < s.min {
		s.position = s.min
	}
	if s.position > maxPos {
		s.position = maxPos
	}
}

func (s *ScrollBar) maxPosition() int {
	if s.max <= s.min {
		return s.min
	}
	m := s.max - s.pageSize
	if m < s.min {
		return s.min
	}
	return m
}

func (s *ScrollBar) trackLength(width, height int) int {
	if s.horizontal {
		return width - 2
	}
	return height - 2
}

func (s *ScrollBar) shouldHide(width, height int) bool {
	if !s.autoHide {
		return false
	}
	if s.trackLength(width, height) <= 0 {
		return true
	}
	return (s.max - s.min) <= s.pageSize
}

func (s *ScrollBar) thumbMetrics(trackLen int) (start, size int) {
	if trackLen <= 0 {
		return 0, 0
	}

	content := s.max - s.min
	if content <= 0 || content <= s.pageSize {
		return 0, trackLen
	}

	size = int(math.Round(float64(s.pageSize) / float64(content) * float64(trackLen)))
	if size < 1 {
		size = 1
	}
	if size > trackLen {
		size = trackLen
	}

	maxPos := s.maxPosition() - s.min
	if maxPos <= 0 {
		return 0, size
	}

	pos := s.position - s.min
	start = int(math.Round(float64(pos) / float64(maxPos) * float64(trackLen-size)))
	if start < 0 {
		start = 0
	}
	if start > trackLen-size {
		start = trackLen - size
	}

	return start, size
}

func (s *ScrollBar) Draw(screen tcell.Screen) {
	s.Box.DrawForSubclass(screen, s)

	x, y, width, height := s.GetInnerRect()
	if width <= 0 || height <= 0 {
		return
	}
	if s.shouldHide(width, height) {
		return
	}

	trackLen := s.trackLength(width, height)
	if trackLen < 0 {
		return
	}

	thumbStart, thumbSize := s.thumbMetrics(trackLen)

	put := func(px, py int, r rune, st tcell.Style) {
		screen.SetContent(px, py, r, nil, st)
	}

	if s.horizontal {
		if width < 2 {
			return
		}
		put(x, y, s.startRune, s.style)
		for i := 0; i < trackLen; i++ {
			r := s.midRune
			st := s.style
			if i >= thumbStart && i < thumbStart+thumbSize {
				r = s.thumbRune
				st = s.thumbStyle
			}
			put(x+1+i, y, r, st)
		}
		put(x+width-1, y, s.endRune, s.style)
	} else {
		if height < 2 {
			return
		}
		put(x, y, s.startRune, s.style)
		for i := 0; i < trackLen; i++ {
			r := s.midRune
			st := s.style
			if i >= thumbStart && i < thumbStart+thumbSize {
				r = s.thumbRune
				st = s.thumbStyle
			}
			put(x, y+1+i, r, st)
		}
		put(x, y+height-1, s.endRune, s.style)
	}
}

func (s *ScrollBar) emitChanged() {
	if s.changed != nil {
		s.changed(s.min, s.max, s.position)
	}
}

func (s *ScrollBar) setPosition(pos int, emit bool) {
	old := s.position
	s.position = pos
	s.clamp()
	if emit && s.position != old {
		s.emitChanged()
	}
}

func (s *ScrollBar) trackIndexFromMouse(x, y int) int {
	rx, ry, width, _ := s.GetInnerRect()
	if s.horizontal {
		return x - rx - 1
	}
	_ = width
	return y - ry - 1
}

func (s *ScrollBar) MouseHandler() func(
	action tview.MouseAction,
	event *tcell.EventMouse,
	setFocus func(p tview.Primitive),
) (consumed bool, capture tview.Primitive) {
	return s.WrapMouseHandler(func(
		action tview.MouseAction,
		event *tcell.EventMouse,
		setFocus func(p tview.Primitive),
	) (consumed bool, capture tview.Primitive) {
		if s.disabled {
			return false, nil
		}

		x, y := event.Position()
		rx, ry, width, height := s.GetInnerRect()
		if width <= 0 || height <= 0 {
			return false, nil
		}

		trackLen := s.trackLength(width, height)
		thumbStart, thumbSize := s.thumbMetrics(trackLen)

		inside := x >= rx && y >= ry && x < rx+width && y < ry+height

		switch action {
		case tview.MouseScrollUp:
			if inside {
				s.setPosition(s.position-s.lineStep, true)
				return true, nil
			}
			return false, nil

		case tview.MouseScrollDown:
			if inside {
				s.setPosition(s.position+s.lineStep, true)
				return true, nil
			}
			return false, nil

		case tview.MouseLeftDown:
			if !inside {
				return false, nil
			}

			// Optional:
			setFocus(s)

			// Arrow buttons.
			if (!s.horizontal && y == ry) || (s.horizontal && x == rx) {
				s.setPosition(s.position-s.lineStep, true)
				return true, nil
			}
			if (!s.horizontal && y == ry+height-1) || (s.horizontal && x == rx+width-1) {
				s.setPosition(s.position+s.lineStep, true)
				return true, nil
			}

			idx := s.trackIndexFromMouse(x, y)
			if idx >= thumbStart && idx < thumbStart+thumbSize {
				s.dragging = true
				s.dragOffset = idx - thumbStart
				return true, s // capture future mouse events
			}

			if idx < thumbStart {
				s.setPosition(s.position-s.pageStep, true)
				return true, nil
			}
			s.setPosition(s.position+s.pageStep, true)
			return true, nil

		case tview.MouseMove:
			if !s.dragging {
				return false, nil
			}
			if trackLen <= 0 {
				return true, s
			}

			idx := s.trackIndexFromMouse(x, y) - s.dragOffset
			usable := trackLen - thumbSize
			if usable <= 0 {
				s.setPosition(s.min, true)
				return true, s
			}
			if idx < 0 {
				idx = 0
			}
			if idx > usable {
				idx = usable
			}

			maxPos := s.maxPosition() - s.min
			pos := s.min + int(math.Round(float64(idx)/float64(usable)*float64(maxPos)))
			s.setPosition(pos, true)
			return true, s // keep capture while dragging

		case tview.MouseLeftUp:
			if s.dragging {
				s.dragging = false
				return true, nil // release capture
			}
			return false, nil
		}

		return false, nil
	})
}
