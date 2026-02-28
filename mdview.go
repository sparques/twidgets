package twidgets

import (
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
	"github.com/rivo/tview"
)

type Cell struct {
	Ch    rune
	Style tcell.Style
}

type Line []Cell

// MarkdownView is a tview primitive that renders Markdown using tcell styles directly.
type MarkdownView struct {
	*tview.Box

	mu sync.RWMutex

	mdText string

	// Unwrapped logical lines, built from the AST.
	lines []Line

	// Scrolling.
	scrollY int
	follow  bool // when true, keep bottom in view after SetMarkdown

	// Base style + palette knobs.
	baseStyle      tcell.Style
	headingStyle   tcell.Style
	emphStyle      tcell.Style
	strongStyle    tcell.Style
	codeStyle      tcell.Style
	codeBlockStyle tcell.Style
	quoteStyle     tcell.Style
	linkStyle      tcell.Style
}

func NewMarkdownView() *MarkdownView {
	v := &MarkdownView{
		Box: tview.NewBox(),

		baseStyle:      tcell.StyleDefault,
		headingStyle:   tcell.StyleDefault.Bold(true).Underline(true),
		emphStyle:      tcell.StyleDefault.Italic(true),
		strongStyle:    tcell.StyleDefault.Bold(true),
		codeStyle:      tcell.StyleDefault.Reverse(true),
		codeBlockStyle: tcell.StyleDefault.Reverse(true),
		quoteStyle:     tcell.StyleDefault.Dim(true),
		linkStyle:      tcell.StyleDefault.Underline(true),
	}
	v.SetBorder(false)
	return v
}

// Style setters (optional niceties).
func (v *MarkdownView) SetBaseStyle(s tcell.Style) *MarkdownView {
	v.mu.Lock()
	v.baseStyle = s

	v.headingStyle = v.baseStyle.Bold(true).Underline(true)
	v.emphStyle = v.baseStyle.Italic(true)
	v.strongStyle = v.baseStyle.Bold(true)
	v.codeStyle = v.baseStyle.Reverse(true)
	v.codeBlockStyle = v.baseStyle.Reverse(true)
	v.quoteStyle = v.baseStyle.Dim(true)
	v.linkStyle = v.baseStyle.Underline(true)

	v.mu.Unlock()
	return v
}
func (v *MarkdownView) SetHeadingStyle(s tcell.Style) *MarkdownView {
	v.mu.Lock()
	v.headingStyle = s
	v.mu.Unlock()
	return v
}
func (v *MarkdownView) SetEmphStyle(s tcell.Style) *MarkdownView {
	v.mu.Lock()
	v.emphStyle = s
	v.mu.Unlock()
	return v
}
func (v *MarkdownView) SetStrongStyle(s tcell.Style) *MarkdownView {
	v.mu.Lock()
	v.strongStyle = s
	v.mu.Unlock()
	return v
}
func (v *MarkdownView) SetCodeStyle(s tcell.Style) *MarkdownView {
	v.mu.Lock()
	v.codeStyle = s
	v.mu.Unlock()
	return v
}
func (v *MarkdownView) SetCodeBlockStyle(s tcell.Style) *MarkdownView {
	v.mu.Lock()
	v.codeBlockStyle = s
	v.mu.Unlock()
	return v
}
func (v *MarkdownView) SetQuoteStyle(s tcell.Style) *MarkdownView {
	v.mu.Lock()
	v.quoteStyle = s
	v.mu.Unlock()
	return v
}
func (v *MarkdownView) SetLinkStyle(s tcell.Style) *MarkdownView {
	v.mu.Lock()
	v.linkStyle = s
	v.mu.Unlock()
	return v
}

func (v *MarkdownView) SetFollow(f bool) *MarkdownView {
	v.mu.Lock()
	v.follow = f
	v.mu.Unlock()
	return v
}

func (v *MarkdownView) SetMarkdown(md string) *MarkdownView {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.mdText = md
	v.lines = parseAndBuildLines(md, v)

	// clamp scroll
	if v.scrollY < 0 {
		v.scrollY = 0
	}
	if v.follow {
		// We don't know final wrapped height until Draw(), but we can bias toward end.
		// We'll clamp again during Draw after wrapping.
		v.scrollY = 1 << 30
	}
	return v
}

func (v *MarkdownView) GetMarkdown() string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.mdText
}

func (v *MarkdownView) ScrollToTop() { v.mu.Lock(); v.scrollY = 0; v.mu.Unlock() }
func (v *MarkdownView) ScrollToEnd() { v.mu.Lock(); v.scrollY = 1 << 30; v.mu.Unlock() }

func (v *MarkdownView) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return func(ev *tcell.EventKey, setFocus func(p tview.Primitive)) {
		// Basic scroll keys. You can customize to match your BBS vibe.
		switch ev.Key() {
		case tcell.KeyPgDn:
			_, _, _, h := v.GetInnerRect()
			v.ScrollBy(h - 1)
		case tcell.KeyPgUp:
			_, _, _, h := v.GetInnerRect()
			v.ScrollBy(-(h - 1))
		case tcell.KeyDown:
			v.ScrollBy(1)
		case tcell.KeyUp:
			v.ScrollBy(-1)
		case tcell.KeyHome:
			v.ScrollToTop()
		case tcell.KeyEnd:
			v.ScrollToEnd()
		}
	}
}

func (v *MarkdownView) ScrollBy(dy int) {
	v.mu.Lock()
	v.scrollY += dy
	if v.scrollY < 0 {
		v.scrollY = 0
	}
	v.mu.Unlock()
}

func (v *MarkdownView) Draw(screen tcell.Screen) {
	v.Box.Draw(screen)

	v.mu.RLock()
	lines := v.lines
	scrollY := v.scrollY
	baseStyle := v.baseStyle
	v.mu.RUnlock()

	x, y, w, h := v.GetInnerRect()
	if w <= 0 || h <= 0 {
		return
	}

	// Wrap logical lines to screen width.
	wrapped := wrapLines(lines, w)

	// Clamp scrollY now that we know total height.
	maxScroll := len(wrapped) - h
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scrollY > maxScroll {
		scrollY = maxScroll
	}
	if scrollY < 0 {
		scrollY = 0
	}

	// Persist clamped scroll (and handle follow mode using big scroll sentinel).
	v.mu.Lock()
	v.scrollY = scrollY
	v.mu.Unlock()

	// Clear background.
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			screen.SetContent(x+col, y+row, ' ', nil, baseStyle)
		}
	}

	// Draw visible window.
	for row := 0; row < h; row++ {
		src := row + scrollY
		if src < 0 || src >= len(wrapped) {
			continue
		}
		line := wrapped[src]
		col := 0
		for _, c := range line {
			if col >= w {
				break
			}
			ch := c.Ch
			if ch == 0 {
				ch = ' '
			}
			st := c.Style
			if st == (tcell.Style{}) {
				st = baseStyle
			}
			screen.SetContent(x+col, y+row, ch, nil, st)
			col++
		}
	}
}

func parseAndBuildLines(md string, v *MarkdownView) []Line {
	ext := parser.CommonExtensions | parser.AutoHeadingIDs | parser.FencedCode | parser.Strikethrough
	p := parser.NewWithExtensions(ext)

	root := markdown.Parse([]byte(md), p)

	b := &builder{
		view:  v,
		style: v.baseStyle,
		stack: []tcell.Style{v.baseStyle},
	}
	b.ensureLine()
	b.atLineStart = true

	ast.WalkFunc(root, func(node ast.Node, entering bool) ast.WalkStatus {
		switch n := node.(type) {
		case *ast.Document:
			// noop

		case *ast.Paragraph:
			if !entering {
				b.blankLine(1)
			}

		case *ast.Heading:
			if entering {
				// Slightly different per level if you want:
				// we’ll just use headingStyle and prepend ## feel.
				b.pushStyle(v.headingStyle)
			} else {
				b.popStyle()
				b.blankLine(1)
			}

		case *ast.Emph:
			if entering {
				b.pushMerged(v.emphStyle)
			} else {
				b.popStyle()
			}

		case *ast.Strong:
			if entering {
				b.pushMerged(v.strongStyle)
			} else {
				b.popStyle()
			}

		case *ast.Del:
			if entering {
				b.pushMerged(tcell.StyleDefault.StrikeThrough(true))
			} else {
				b.popStyle()
			}

		case *ast.Code:
			if entering {
				b.pushMerged(v.codeStyle)
				b.writeString(string(n.Literal))
				b.popStyle()
			}
			return ast.SkipChildren

		case *ast.CodeBlock:
			if entering {
				b.blankLine(0)
				b.pushMerged(v.codeBlockStyle)
				// Render code block literally, line by line.
				s := string(n.Literal)
				s = strings.ReplaceAll(s, "\r\n", "\n")
				s = strings.TrimRight(s, "\n")
				for i, ln := range strings.Split(s, "\n") {
					if i > 0 {
						b.newline()
					}
					b.writeString(ln)
				}
				b.popStyle()
				b.blankLine(1)
			}
			return ast.SkipChildren

		case *ast.BlockQuote:
			if entering {
				b.quoteDepth++
				b.blankLine(0)
			} else {
				b.quoteDepth--
				b.blankLine(1)
			}

		case *ast.List:
			if entering {
				b.listDepth++
				b.listIndexStack = append(b.listIndexStack, 0)
				b.blankLine(0)
			} else {
				b.listDepth--
				if len(b.listIndexStack) > 0 {
					b.listIndexStack = b.listIndexStack[:len(b.listIndexStack)-1]
				}
				b.blankLine(1)
			}

		case *ast.ListItem:
			if entering {
				b.inListItem = true
				b.hangingIndent = 0

				prefix := " • "
				if b.currentListIsOrdered(n) {
					if len(b.listIndexStack) > 0 {
						b.listIndexStack[len(b.listIndexStack)-1]++
						prefix = itoa(b.listIndexStack[len(b.listIndexStack)-1]) + ". "
					}
				}

				b.newlineIfNeeded()

				// Ensure we emit quote/list base indentation before bullet.
				b.emitLinePrefixIfNeeded()

				// Bullet itself: slightly emphasized.
				b.pushMerged(v.strongStyle)
				b.writeString(prefix)
				b.popStyle()

				// Now set hanging indent so wrapped lines / hard breaks align after the bullet.
				// This is only the indent after the prefix emitter. We already emitted quote + base list indent.
				b.hangingIndent = runeWidth(prefix)
			} else {
				b.inListItem = false
				b.hangingIndent = 0
				b.newline()
			}

		case *ast.Link:
			if entering {
				b.pushMerged(v.linkStyle)
			} else {
				b.popStyle()
			}

		case *ast.Text:
			if entering {
				b.writeString(string(n.Literal))
			}

		case *ast.Softbreak:
			if entering {
				b.writeString(" ")
			}

		case *ast.Hardbreak:
			if entering {
				b.newline()
			}
			/*
				case *ast.ThematicBreak:
					if entering {
						b.newlineIfNeeded()
						b.writeString(strings.Repeat("─", 30))
						b.blankLine(1)
					}
			*/
		default:
			// Unknown nodes: let children render if any.
		}
		return ast.GoToNext
	})

	// Trim trailing empty lines.
	for len(b.lines) > 0 && len(b.lines[len(b.lines)-1]) == 0 {
		b.lines = b.lines[:len(b.lines)-1]
	}
	return b.lines
}

type builder struct {
	view *MarkdownView

	lines LineSlice
	cur   Line

	style tcell.Style
	stack []tcell.Style

	quoteDepth int

	listDepth      int
	listIndexStack []int
	inListItem     bool

	atLineStart   bool
	hangingIndent int
}

type LineSlice []Line

func (b *builder) ensureLine() {
	if b.cur == nil {
		b.cur = Line{}
	}
}

func (b *builder) flushLine() {
	if b.cur != nil {
		b.lines = append(b.lines, b.cur)
		b.cur = nil
	}
}

func (b *builder) newline() {
	b.flushLine()
	b.ensureLine()
	b.atLineStart = true
}

func (b *builder) blankLine(n int) {
	b.flushLine()
	for i := 0; i < n; i++ {
		b.lines = append(b.lines, Line{})
	}
	b.ensureLine()
	b.atLineStart = true
	b.hangingIndent = 0
}

func (b *builder) newlineIfNeeded() {
	if b.cur == nil {
		b.ensureLine()
		return
	}
	// If current line already has content, start a new line.
	if len(b.cur) > 0 {
		b.newline()
	}
}

func (b *builder) pushStyle(s tcell.Style) {
	b.stack = append(b.stack, s)
	b.style = s
}

func (b *builder) pushMerged(s tcell.Style) {
	// Merge attributes on top of current style.
	// tcell.Style is immutable-ish: you can only “set” attrs, so we approximate by taking
	// current fg/bg and then applying attrs from s.
	cur := b.style
	fg, bg, _ := cur.Decompose()
	_, _, attrs := s.Decompose()
	merged := tcell.StyleDefault.Foreground(fg).Background(bg)
	// apply attrs we care about
	if attrs&tcell.AttrBold != 0 {
		merged = merged.Bold(true)
	}
	if attrs&tcell.AttrUnderline != 0 {
		merged = merged.Underline(true)
	}
	if attrs&tcell.AttrItalic != 0 {
		merged = merged.Italic(true)
	}
	if attrs&tcell.AttrDim != 0 {
		merged = merged.Dim(true)
	}
	if attrs&tcell.AttrReverse != 0 {
		merged = merged.Reverse(true)
	}
	if attrs&tcell.AttrStrikeThrough != 0 {
		merged = merged.StrikeThrough(true)
	}
	b.pushStyle(merged)
}

func (b *builder) popStyle() {
	if len(b.stack) <= 1 {
		b.style = b.view.baseStyle
		b.stack = []tcell.Style{b.view.baseStyle}
		return
	}
	b.stack = b.stack[:len(b.stack)-1]
	b.style = b.stack[len(b.stack)-1]
}

func (b *builder) emitLinePrefixIfNeeded() {
	if !b.atLineStart {
		return
	}

	// Base indent from nesting (lists)
	baseIndent := 0
	if b.listDepth > 0 {
		baseIndent += (b.listDepth - 1) * 2
	}

	// Quote gets drawn *after* base indent, terminal-style.
	for range baseIndent {
		b.cur = append(b.cur, Cell{Ch: ' ', Style: b.style})
	}

	if b.quoteDepth > 0 {
		// Use quote style for the bar only.
		b.pushMerged(b.view.quoteStyle)
		b.cur = append(b.cur, Cell{Ch: '│', Style: b.style})
		b.popStyle()
		b.cur = append(b.cur, Cell{Ch: ' ', Style: b.style})
	}

	// Hanging indent for continued list item lines (align under content after bullet)
	for range b.hangingIndent {
		b.cur = append(b.cur, Cell{Ch: ' ', Style: b.style})
	}

	b.atLineStart = false
}

func (b *builder) writeRune(r rune) {
	b.ensureLine()
	if r != '\n' {
		b.emitLinePrefixIfNeeded()
	}
	b.cur = append(b.cur, Cell{Ch: r, Style: b.style})
}

func (b *builder) writeString(s string) {
	b.ensureLine()
	// Preserve newlines if any slipped in.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	for len(s) > 0 {
		if s[0] == '\n' {
			b.newline()
			s = s[1:]
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			// bad byte; render replacement
			r = '�'
			size = 1
		}
		b.writeRune(r)
		s = s[size:]
	}
}

func (b *builder) currentListIsOrdered(item *ast.ListItem) bool {
	// We need to look upward to the parent list; gomarkdown’s AST makes this annoying.
	// Pragmatic hack: check ancestors for *ast.List and read Flags.
	for p := item.GetParent(); p != nil; p = p.GetParent() {
		if l, ok := p.(*ast.List); ok {
			return (l.ListFlags&ast.ListTypeOrdered != 0)
		}
	}
	return false
}

func wrapLines(src []Line, width int) []Line {
	if width <= 0 {
		return nil
	}

	var out []Line
	for _, ln := range src {
		if len(ln) == 0 {
			out = append(out, Line{})
			continue
		}
		out = append(out, wrapOneLine(ln, width)...)
	}
	return out
}

func wrapOneLine(ln Line, width int) []Line {
	// Word wrap that preserves styles and breaks on spaces when possible.
	var out []Line
	var cur Line
	curW := 0

	flush := func() {
		out = append(out, cur)
		cur = Line{}
		curW = 0
	}

	// We’ll build “words” as slices of cells.
	var word Line
	wordW := 0
	var pendingSpace *Cell

	emitWord := func() {
		if len(word) == 0 {
			return
		}
		// If this word would exceed width, flush current line first (unless empty).
		need := wordW
		if pendingSpace != nil && curW > 0 {
			need += 1
		}
		if need > width && curW > 0 {
			flush()
		}
		// Add pending space if needed.
		if pendingSpace != nil && curW > 0 && curW < width {
			cur = append(cur, *pendingSpace)
			curW++
		}
		pendingSpace = nil

		// If word longer than width, hard-wrap.
		for len(word) > 0 {
			spaceLeft := width - curW
			if spaceLeft <= 0 {
				flush()
				spaceLeft = width
			}
			if wordW <= spaceLeft {
				cur = append(cur, word...)
				curW += wordW
				word = nil
				wordW = 0
				return
			}
			// take prefix
			cur = append(cur, word[:spaceLeft]...)
			// compute remaining width
			word = word[spaceLeft:]
			wordW -= spaceLeft
			flush()
		}
	}

	for _, c := range ln {
		if c.Ch == ' ' || c.Ch == '\t' {
			// End current word first.
			emitWord()

			// Preserve leading whitespace (indentation) instead of collapsing it.
			if curW == 0 && pendingSpace == nil && len(word) == 0 {
				tmp := c
				if tmp.Ch == '\t' {
					tmp.Ch = ' ' // could expand to multiple spaces if you want
				}
				cur = append(cur, tmp)
				curW++
				if curW >= width {
					flush()
				}
				continue
			}

			// Otherwise collapse runs of whitespace into a single space.
			tmp := c
			tmp.Ch = ' '
			pendingSpace = &tmp
			continue
		}

		word = append(word, c)
		wordW++
	}

	emitWord()

	// If we ended with content in cur, flush.
	if len(cur) > 0 {
		out = append(out, cur)
	} else if len(out) == 0 {
		out = append(out, Line{})
	}
	return out
}

func itoa(n int) string {
	// tiny int->string for list numbering; no fmt import.
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + (n % 10))
		n /= 10
	}
	return string(b[i:])
}

func runeWidth(s string) int {
	// simple rune count; good enough for ASCII bullets/numbers
	return utf8.RuneCountInString(s)
}
