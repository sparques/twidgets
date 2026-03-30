package twidgets

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode"
)

type SVGOptions struct {
	Scale     float64
	Offset    Position
	FlipY     bool
	CurveStep float64 // smaller = more segments; 1..4 is a decent start
}

func DefaultSVGOptions() SVGOptions {
	return SVGOptions{
		Scale:     1.0,
		Offset:    Position{},
		FlipY:     true,
		CurveStep: 2.0,
	}
}

func (m *Map) AddSVG(r io.Reader, opts SVGOptions) error {
	paths, err := LoadSVGPaths(r, opts)
	if err != nil {
		return err
	}
	m.Paths = append(m.Paths, paths...)
	return nil
}

func LoadSVGPaths(r io.Reader, opts SVGOptions) ([]*Path, error) {
	if opts.Scale == 0 {
		opts.Scale = 1
	}
	if opts.CurveStep <= 0 {
		opts.CurveStep = 2.0
	}

	dec := xml.NewDecoder(r)
	var out []*Path

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}

		switch se.Name.Local {
		case "path":
			d := attr(se.Attr, "d")
			if d == "" {
				continue
			}
			paths, err := parseSVGPathData(d, opts)
			if err != nil {
				return nil, fmt.Errorf("parse <path>: %w", err)
			}
			out = append(out, paths...)

		case "polyline":
			points := attr(se.Attr, "points")
			if points == "" {
				continue
			}
			p, err := parseSVGPoints(points, false, opts)
			if err != nil {
				return nil, fmt.Errorf("parse <polyline>: %w", err)
			}
			out = append(out, p)

		case "polygon":
			points := attr(se.Attr, "points")
			if points == "" {
				continue
			}
			p, err := parseSVGPoints(points, true, opts)
			if err != nil {
				return nil, fmt.Errorf("parse <polygon>: %w", err)
			}
			out = append(out, p)

		case "rect":
			p, err := parseSVGRect(se, opts)
			if err != nil {
				return nil, fmt.Errorf("parse <rect>: %w", err)
			}
			if p != nil {
				out = append(out, p)
			}
		}
	}

	return out, nil
}

func attr(attrs []xml.Attr, name string) string {
	for _, a := range attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

func parseSVGPoints(s string, closed bool, opts SVGOptions) (*Path, error) {
	fields := splitNumbersAndCommas(s)
	if len(fields)%2 != 0 {
		return nil, fmt.Errorf("odd number of coordinates in points")
	}

	var pts []Position
	for i := 0; i < len(fields); i += 2 {
		x, err := strconv.ParseFloat(fields[i], 64)
		if err != nil {
			return nil, err
		}
		y, err := strconv.ParseFloat(fields[i+1], 64)
		if err != nil {
			return nil, err
		}
		pts = append(pts, transformSVGPoint(Position{X: x, Y: y}, opts))
	}

	return &Path{
		Positions: pts,
		IsClosed:  closed,
	}, nil
}

func parseSVGRect(se xml.StartElement, opts SVGOptions) (*Path, error) {
	x, _ := parseFloatDefault(attr(se.Attr, "x"), 0)
	y, _ := parseFloatDefault(attr(se.Attr, "y"), 0)
	w, err := parseFloatDefault(attr(se.Attr, "width"), 0)
	if err != nil {
		return nil, err
	}
	h, err := parseFloatDefault(attr(se.Attr, "height"), 0)
	if err != nil {
		return nil, err
	}
	if w == 0 || h == 0 {
		return nil, nil
	}

	pts := []Position{
		transformSVGPoint(Position{X: x, Y: y}, opts),
		transformSVGPoint(Position{X: x + w, Y: y}, opts),
		transformSVGPoint(Position{X: x + w, Y: y + h}, opts),
		transformSVGPoint(Position{X: x, Y: y + h}, opts),
	}

	return &Path{
		Positions: pts,
		IsClosed:  true,
	}, nil
}

func parseFloatDefault(s string, def float64) (float64, error) {
	if strings.TrimSpace(s) == "" {
		return def, nil
	}
	return strconv.ParseFloat(s, 64)
}

func splitNumbersAndCommas(s string) []string {
	s = strings.ReplaceAll(s, ",", " ")
	return strings.Fields(s)
}

func transformSVGPoint(p Position, opts SVGOptions) Position {
	p.X = p.X*opts.Scale + opts.Offset.X
	if opts.FlipY {
		p.Y = -p.Y
	}
	p.Y = p.Y*opts.Scale + opts.Offset.Y
	return p
}

func parseSVGPathData(d string, opts SVGOptions) ([]*Path, error) {
	toks, err := tokenizeSVGPath(d)
	if err != nil {
		return nil, err
	}

	var out []*Path
	var cur *Path
	var pen Position
	var subpathStart Position
	var cmd byte
	i := 0

	newPath := func() {
		cur = &Path{}
		out = append(out, cur)
	}

	for i < len(toks) {
		if isCommandToken(toks[i]) {
			cmd = toks[i][0]
			i++
		} else if cmd == 0 {
			return nil, fmt.Errorf("path data starts with number, missing command")
		}

		switch cmd {
		case 'M', 'm':
			// first pair is move, subsequent pairs are implicit line-to
			first := true
			for i+1 < len(toks) && !isCommandToken(toks[i]) {
				x, err := strconv.ParseFloat(toks[i], 64)
				if err != nil {
					return nil, err
				}
				y, err := strconv.ParseFloat(toks[i+1], 64)
				if err != nil {
					return nil, err
				}
				i += 2

				p := Position{X: x, Y: y}
				if cmd == 'm' {
					p.X += pen.X
					p.Y += pen.Y
				}

				if first {
					newPath()
					pen = p
					subpathStart = p
					cur.Positions = append(cur.Positions, transformSVGPoint(p, opts))
					first = false
				} else {
					pen = p
					cur.Positions = append(cur.Positions, transformSVGPoint(p, opts))
				}
			}

		case 'L', 'l':
			if cur == nil {
				newPath()
				subpathStart = pen
				cur.Positions = append(cur.Positions, transformSVGPoint(pen, opts))
			}
			for i+1 < len(toks) && !isCommandToken(toks[i]) {
				x, err := strconv.ParseFloat(toks[i], 64)
				if err != nil {
					return nil, err
				}
				y, err := strconv.ParseFloat(toks[i+1], 64)
				if err != nil {
					return nil, err
				}
				i += 2

				p := Position{X: x, Y: y}
				if cmd == 'l' {
					p.X += pen.X
					p.Y += pen.Y
				}
				pen = p
				cur.Positions = append(cur.Positions, transformSVGPoint(p, opts))
			}

		case 'H', 'h':
			if cur == nil {
				newPath()
				subpathStart = pen
				cur.Positions = append(cur.Positions, transformSVGPoint(pen, opts))
			}
			for i < len(toks) && !isCommandToken(toks[i]) {
				x, err := strconv.ParseFloat(toks[i], 64)
				if err != nil {
					return nil, err
				}
				i++

				p := pen
				if cmd == 'h' {
					p.X += x
				} else {
					p.X = x
				}
				pen = p
				cur.Positions = append(cur.Positions, transformSVGPoint(p, opts))
			}

		case 'V', 'v':
			if cur == nil {
				newPath()
				subpathStart = pen
				cur.Positions = append(cur.Positions, transformSVGPoint(pen, opts))
			}
			for i < len(toks) && !isCommandToken(toks[i]) {
				y, err := strconv.ParseFloat(toks[i], 64)
				if err != nil {
					return nil, err
				}
				i++

				p := pen
				if cmd == 'v' {
					p.Y += y
				} else {
					p.Y = y
				}
				pen = p
				cur.Positions = append(cur.Positions, transformSVGPoint(p, opts))
			}

		case 'C', 'c':
			if cur == nil {
				newPath()
				subpathStart = pen
				cur.Positions = append(cur.Positions, transformSVGPoint(pen, opts))
			}
			for i+5 < len(toks) && !isCommandToken(toks[i]) {
				x1, _ := strconv.ParseFloat(toks[i], 64)
				y1, _ := strconv.ParseFloat(toks[i+1], 64)
				x2, _ := strconv.ParseFloat(toks[i+2], 64)
				y2, _ := strconv.ParseFloat(toks[i+3], 64)
				x, _ := strconv.ParseFloat(toks[i+4], 64)
				y, _ := strconv.ParseFloat(toks[i+5], 64)
				i += 6

				p1 := Position{X: x1, Y: y1}
				p2 := Position{X: x2, Y: y2}
				p3 := Position{X: x, Y: y}
				if cmd == 'c' {
					p1.X += pen.X
					p1.Y += pen.Y
					p2.X += pen.X
					p2.Y += pen.Y
					p3.X += pen.X
					p3.Y += pen.Y
				}

				flattenCubicBezier(
					pen,
					p1,
					p2,
					p3,
					opts.CurveStep,
					func(p Position) {
						cur.Positions = append(cur.Positions, transformSVGPoint(p, opts))
					},
				)
				pen = p3
			}

		case 'Z', 'z':
			if cur != nil {
				cur.IsClosed = true
				pen = subpathStart
			}

		default:
			return nil, fmt.Errorf("unsupported SVG path command %q", cmd)
		}
	}

	// Drop degenerate paths.
	dst := out[:0]
	for _, p := range out {
		if p != nil && len(p.Positions) >= 2 {
			dst = append(dst, p)
		}
	}
	out = dst

	return out, nil
}

func tokenizeSVGPath(s string) ([]string, error) {
	var out []string
	i := 0
	for i < len(s) {
		r := rune(s[i])

		if unicode.IsSpace(r) || r == ',' {
			i++
			continue
		}

		if isSVGCommandByte(s[i]) {
			out = append(out, s[i:i+1])
			i++
			continue
		}

		start := i
		if s[i] == '+' || s[i] == '-' {
			i++
		}

		digits := false
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits = true
		}
		if i < len(s) && s[i] == '.' {
			i++
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
				digits = true
			}
		}
		if !digits {
			return nil, fmt.Errorf("invalid number near %q", s[start:])
		}
		if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
			i++
			if i < len(s) && (s[i] == '+' || s[i] == '-') {
				i++
			}
			expDigits := false
			for i < len(s) && s[i] >= '0' && s[i] <= '9' {
				i++
				expDigits = true
			}
			if !expDigits {
				return nil, fmt.Errorf("invalid exponent near %q", s[start:])
			}
		}

		out = append(out, s[start:i])
	}
	return out, nil
}

func isSVGCommandByte(b byte) bool {
	switch b {
	case 'M', 'm', 'L', 'l', 'H', 'h', 'V', 'v', 'C', 'c', 'Z', 'z':
		return true
	default:
		return false
	}
}

func isCommandToken(s string) bool {
	return len(s) == 1 && isSVGCommandByte(s[0])
}

func flattenCubicBezier(p0, p1, p2, p3 Position, step float64, emit func(Position)) {
	// crude but effective: estimate curve length from control polygon
	approxLen := dist(p0, p1) + dist(p1, p2) + dist(p2, p3)
	n := int(math.Ceil(approxLen / step))
	if n < 1 {
		n = 1
	}

	for i := 1; i <= n; i++ {
		t := float64(i) / float64(n)
		emit(cubicBezierPoint(p0, p1, p2, p3, t))
	}
}

func cubicBezierPoint(p0, p1, p2, p3 Position, t float64) Position {
	u := 1 - t
	tt := t * t
	uu := u * u
	uuu := uu * u
	ttt := tt * t

	return Position{
		X: uuu*p0.X + 3*uu*t*p1.X + 3*u*tt*p2.X + ttt*p3.X,
		Y: uuu*p0.Y + 3*uu*t*p1.Y + 3*u*tt*p2.Y + ttt*p3.Y,
	}
}

func dist(a, b Position) float64 {
	dx := b.X - a.X
	dy := b.Y - a.Y
	return math.Hypot(dx, dy)
}
