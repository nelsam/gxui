// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mixins

import (
	"git.sr.ht/~nelsam/gxui"
	"git.sr.ht/~nelsam/gxui/interval"
	"git.sr.ht/~nelsam/gxui/math"
)

type CodeEditorLinePaintInfo struct {
	LineSpan     interval.IntData
	Runes        []rune
	GlyphOffsets []math.Point
	GlyphWidth   int
	LineHeight   int
	Font         gxui.Font
}

type CodeEditorLineOuter interface {
	DefaultTextBoxLineOuter
	PaintEditorSelections(gxui.Canvas, CodeEditorLinePaintInfo)
	PaintEditorCarets(gxui.Canvas, CodeEditorLinePaintInfo)
	PaintBackgroundSpans(gxui.Canvas, CodeEditorLinePaintInfo)
	PaintGlyphs(gxui.Canvas, CodeEditorLinePaintInfo)
	PaintBorders(gxui.Canvas, CodeEditorLinePaintInfo)
}

// CodeEditorLine
type CodeEditorLine struct {
	DefaultTextBoxLine
	outer CodeEditorLineOuter
	ce    *CodeEditor
}

func (l *CodeEditorLine) Init(outer CodeEditorLineOuter, theme gxui.Theme, ce *CodeEditor, lineIndex int) {
	l.DefaultTextBoxLine.Init(outer, theme, &ce.TextBox, lineIndex)
	l.outer = outer
	l.ce = ce
}

func (l *CodeEditorLine) RuneIndexAt(p math.Point) int {
	font := l.ce.Font()
	controller := l.ce.Controller()

	x := p.X
	i := 0
	offsets := l.offsets(font)
	for ; i < len(offsets) && x > offsets[i].X; i++ {
	}

	return controller.LineStart(l.lineIndex) + i
}

func (l *CodeEditorLine) PaintBackgroundSpans(c gxui.Canvas, info CodeEditorLinePaintInfo) {
	start, _ := info.LineSpan.Span()
	offsets := info.GlyphOffsets
	remaining := interval.IntDataList{info.LineSpan}
	for _, layer := range l.ce.layers {
		if layer != nil && layer.BackgroundColor() != nil {
			color := *layer.BackgroundColor()
			for _, span := range layer.Spans().Overlaps(info.LineSpan) {
				interval.Visit(&remaining, span, func(vs, ve uint64, _ int) {
					s, e := vs-start, ve-start
					r := math.CreateRect(offsets[s].X, 0, offsets[e-1].X+info.GlyphWidth, info.LineHeight)
					c.DrawRoundedRect(r, 3, 3, 3, 3, gxui.TransparentPen, gxui.Brush{Color: color})
				})
				interval.Remove(&remaining, span)
			}
		}
	}
}

func (l *CodeEditorLine) PaintGlyphs(c gxui.Canvas, info CodeEditorLinePaintInfo) {
	start, _ := info.LineSpan.Span()
	runes, offsets, font := info.Runes, info.GlyphOffsets, info.Font
	remaining := interval.IntDataList{info.LineSpan}
	for _, layer := range l.ce.layers {
		if layer != nil && layer.Color() != nil {
			color := *layer.Color()
			for _, span := range layer.Spans().Overlaps(info.LineSpan) {
				interval.Visit(&remaining, span, func(vs, ve uint64, _ int) {
					s, e := vs-start, ve-start
					c.DrawRunes(font, runes[s:e], offsets[s:e], color)
				})
				interval.Remove(&remaining, span)
			}
		}
	}
	for _, span := range remaining {
		s, e := span.Span()
		s, e = s-start, e-start
		c.DrawRunes(font, runes[s:e], offsets[s:e], l.ce.textColor)
	}
}

func (l *CodeEditorLine) PaintBorders(c gxui.Canvas, info CodeEditorLinePaintInfo) {
	start, _ := info.LineSpan.Span()
	offsets := info.GlyphOffsets
	for _, layer := range l.ce.layers {
		if layer != nil && layer.BorderColor() != nil {
			color := *layer.BorderColor()
			interval.Visit(layer.Spans(), info.LineSpan, func(vs, ve uint64, _ int) {
				s, e := vs-start, ve-start
				r := math.CreateRect(offsets[s].X, 0, offsets[e-1].X+info.GlyphWidth, info.LineHeight)
				c.DrawRoundedRect(r, 3, 3, 3, 3, gxui.CreatePen(0.5, color), gxui.TransparentBrush)
			})
		}
	}
}

// endOfChar takes a position of a character and returns the position
// of its end.
func (l *CodeEditorLine) endOfChar(position math.Point) math.Point {
	return position.AddX(l.ce.Font().GlyphMaxSize().W / 2)
}

func (l *CodeEditorLine) PaintEditorCarets(c gxui.Canvas, info CodeEditorLinePaintInfo) {
	controller := l.textbox.controller
	for i, count := 0, controller.SelectionCount(); i < count; i++ {
		caret := controller.Caret(i)
		line := controller.LineIndex(caret)
		if line == l.lineIndex {
			var offset math.Point
			start := controller.LineStart(line)
			if len(info.GlyphOffsets) > 0 && caret > start {
				caretOffsetIndex := caret - start - 1
				offset = l.endOfChar(info.GlyphOffsets[caretOffsetIndex])
			}
			top := math.Point{X: l.caretWidth + offset.X, Y: 0}
			bottom := top.Add(math.Point{X: 0, Y: l.Size().H})
			l.outer.PaintCaret(c, top, bottom)
		}
	}
}

func (l *CodeEditorLine) paintSelection(c gxui.Canvas, info CodeEditorLinePaintInfo, first, last int) {
	var startOffset, endOffset math.Point
	if first >= len(info.GlyphOffsets) {
		first = len(info.GlyphOffsets) - 1
	}
	if first > 0 {
		startOffset = l.endOfChar(info.GlyphOffsets[first-1])
	}
	if last >= len(info.GlyphOffsets) {
		last = len(info.GlyphOffsets) - 1
	}
	endOffset = l.endOfChar(info.GlyphOffsets[last])

	width := endOffset.X - startOffset.X
	m := l.outer.MeasureRunes(first, last)
	m.W = width
	top := math.Point{X: l.caretWidth + startOffset.X}
	bottom := top.Add(m.Point())
	l.outer.PaintSelection(c, top, bottom)
}

func (l *CodeEditorLine) PaintEditorSelections(c gxui.Canvas, info CodeEditorLinePaintInfo) {
	if len(info.GlyphOffsets) == 0 {
		return
	}

	controller := l.textbox.controller
	ls, le := controller.LineStart(l.lineIndex), controller.LineEnd(l.lineIndex)
	selections := controller.SelectionSlice()

	selections = l.addDragging(selections)
	for _, s := range selections {
		start := s.Start()
		end := s.End()
		if start == end {
			continue
		}
		if start > end {
			start, end = end, start
		}
		if end <= ls || start >= le {
			continue
		}
		if start <= ls && end > le {
			l.paintSelection(c, info, 0, le-ls)
			return
		}
		start -= ls
		if start < 0 {
			start = 0
		}
		end -= ls
		l.paintSelection(c, info, start, end-1)
	}
}

func (l *CodeEditorLine) offsets(font gxui.Font) []math.Point {
	rect := l.Size().Rect().OffsetX(l.caretWidth)
	runes := l.ce.Controller().LineRunes(l.lineIndex)
	offsets := font.Layout(&gxui.TextBlock{
		Runes:     runes,
		AlignRect: rect,
		H:         gxui.AlignLeft,
		V:         gxui.AlignMiddle,
	})
	l.applyTabWidth(runes, offsets, font)
	for i, offset := range offsets {
		offsets[i] = offset.AddX(-l.offset)
	}
	return offsets
}

// DefaultTextBoxLine overrides
func (l *CodeEditorLine) Paint(c gxui.Canvas) {
	font := l.ce.Font()
	controller := l.ce.Controller()
	if l.lineIndex >= controller.LineCount() {
		// The controller's text differs slightly from the
		// CodeEditor's lines - best to avoid the panic on
		// the next few lines.
		return
	}
	start := controller.LineStart(l.lineIndex)
	end := controller.LineEnd(l.lineIndex)

	var info CodeEditorLinePaintInfo
	if start != end {
		info = CodeEditorLinePaintInfo{
			LineSpan:     interval.CreateIntData(start, end, nil),
			Runes:        controller.LineRunes(l.lineIndex),
			GlyphOffsets: l.offsets(font),
			GlyphWidth:   font.GlyphMaxSize().W,
			LineHeight:   l.Size().H,
			Font:         font,
		}

		l.outer.PaintBackgroundSpans(c, info)
		l.outer.PaintEditorSelections(c, info)
		l.outer.PaintGlyphs(c, info)
		l.outer.PaintBorders(c, info)
	}

	if l.textbox.HasFocus() {
		l.outer.PaintEditorCarets(c, info)
	}
}

func (l *CodeEditorLine) PositionAt(runeIndex int) math.Point {
	tabDiff := l.tabDelta(l.textbox.Font())
	point := l.DefaultTextBoxLine.PositionAt(runeIndex)
	for _, r := range l.textbox.Controller().LineRunes(l.lineIndex) {
		if r == '\t' {
			point.X += tabDiff
		}
	}
	return point
}

// tabDelta returns the difference between the current font's measurement
// of a tab character and the actual tab size based on spaces times the
// code editor's tab width.
func (l *CodeEditorLine) tabDelta(font gxui.Font) int {
	tabWidth := font.Measure(&gxui.TextBlock{Runes: []rune{'\t'}}).W
	spaceWidth := font.Measure(&gxui.TextBlock{Runes: []rune{' '}}).W
	return spaceWidth*l.ce.TabWidth() - tabWidth
}

func (l *CodeEditorLine) applyTabWidth(runes []rune, offsets []math.Point, font gxui.Font) {
	tabExtra := l.tabDelta(font)
	for i, r := range runes {
		if r == '\t' {
			for j := i; j < len(offsets); j++ {
				offsets[j].X += tabExtra
			}
		}
	}
	return
}
