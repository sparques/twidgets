package twidgets

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type Label struct {
	*tview.Box
	text  string
	align int // tview.AlignLeft / Center / Right
	style tcell.Style
}

func NewLabel(text string) *Label {
	return &Label{
		Box:   tview.NewBox(),
		text:  text,
		align: tview.AlignLeft,
		style: tcell.StyleDefault,
	}
}

func (l *Label) SetText(text string) *Label {
	l.text = text
	return l
}

func (l *Label) SetAlign(align int) *Label {
	l.align = align
	return l
}

func (l *Label) GetStyle() tcell.Style {
	return l.style
}

func (l *Label) SetStyle(style tcell.Style) {
	l.style = style
}

func (l *Label) Draw(screen tcell.Screen) {
	l.Box.DrawForSubclass(screen, l)

	x, y, width, height := l.GetInnerRect()
	if height <= 0 || width <= 0 {
		return
	}

	text := []rune(l.text)

	startX := x
	switch l.align {
	case tview.AlignCenter:
		startX = x + (width-len(text))/2
	case tview.AlignRight:
		startX = x + width - len(text)
	}

	for i, r := range text {
		if i >= width {
			break
		}
		screen.SetContent(startX+i, y, r, nil, l.GetStyle())
	}
}
