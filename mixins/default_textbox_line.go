// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mixins

import (
	"github.com/nelsam/gxui"
	"github.com/nelsam/gxui/math"
	"github.com/nelsam/gxui/mixins/base"

	"unicode/utf8"
)

type DefaultTextBoxLineOuter interface {
	base.ControlOuter
	MeasureRunes(s, e int) math.Size
	PaintText(c gxui.Canvas)
	PaintCarets(c gxui.Canvas)
	PaintCaret(c gxui.Canvas, top, bottom math.Point)
	PaintSelections(c gxui.Canvas)
	PaintSelection(c gxui.Canvas, top, bottom math.Point)
}

// DefaultTextBoxLine
type DefaultTextBoxLine struct {
	base.Control
	outer      DefaultTextBoxLineOuter
	textbox    *TextBox
	lineIndex  int
	caretWidth int
	offset     int
}

func (t *DefaultTextBoxLine) Init(outer DefaultTextBoxLineOuter, theme gxui.Theme, textbox *TextBox, lineIndex int) {
	t.Control.Init(outer, theme)
	t.outer = outer
	t.textbox = textbox
	t.lineIndex = lineIndex
	t.SetCaretWidth(2)
	t.OnAttach(func() {
		ev := t.textbox.OnRedrawLines(t.Redraw)
		t.OnDetach(ev.Unlisten)
	})

	// Interface compliance test
	_ = TextBoxLine(t)
}

func (t *DefaultTextBoxLine) SetOffset(offset int) {
	t.offset = offset
	t.Redraw()
}

func (t *DefaultTextBoxLine) SetCaretWidth(width int) {
	if t.caretWidth != width {
		t.caretWidth = width
	}
}

func (t *DefaultTextBoxLine) DesiredSize(min, max math.Size) math.Size {
	return max
}

func (t *DefaultTextBoxLine) Paint(c gxui.Canvas) {
	if t.textbox.HasFocus() {
		t.outer.PaintSelections(c)
	}

	t.outer.PaintText(c)

	if t.textbox.HasFocus() {
		t.outer.PaintCarets(c)
	}
}

func (t *DefaultTextBoxLine) MeasureRunes(s, e int) math.Size {
	controller := t.textbox.controller
	size := t.textbox.font.Measure(&gxui.TextBlock{
		Runes: controller.TextRunes()[s:e],
	})
	size.W -= t.offset
	return size
}

func (t *DefaultTextBoxLine) PaintText(c gxui.Canvas) {
	runes := []rune(t.textbox.controller.Line(t.lineIndex))
	f := t.textbox.font
	offsets := f.Layout(&gxui.TextBlock{
		Runes:     runes,
		AlignRect: t.Size().Rect().OffsetX(t.caretWidth),
		H:         gxui.AlignLeft,
		V:         gxui.AlignBottom,
	})
	for i, offset := range offsets {
		offsets[i] = offset.AddX(-t.offset)
	}
	c.DrawRunes(f, runes, offsets, t.textbox.textColor)
}

func (t *DefaultTextBoxLine) PaintCarets(c gxui.Canvas) {
	controller := t.textbox.controller
	for i, cnt := 0, controller.SelectionCount(); i < cnt; i++ {
		e := controller.Caret(i)
		l := controller.LineIndex(e)
		if l == t.lineIndex {
			s := controller.LineStart(l)
			m := t.outer.MeasureRunes(s, e)
			top := math.Point{X: t.caretWidth + m.W, Y: 0}
			bottom := top.Add(math.Point{X: 0, Y: t.Size().H})
			t.outer.PaintCaret(c, top, bottom)
		}
	}
}

func (t *DefaultTextBoxLine) addDragging(selections []gxui.TextSelection) []gxui.TextSelection {
	if !t.textbox.selectionDragging {
		return selections
	}
	var i int
	for i = range selections {
		if selections[i].Start() > t.textbox.selectionDrag.Start() {
			break
		}
	}
	selections = append(selections, gxui.TextSelection{})
	copy(selections[i+1:], selections[i:len(selections)-2])
	selections[i] = t.textbox.selectionDrag
	return selections
}

func (t *DefaultTextBoxLine) paintSelection(c gxui.Canvas, ls, le, first, last int) {
	if first >= last {
		return
	}
	x := t.outer.MeasureRunes(ls, ls+first).W
	m := t.outer.MeasureRunes(ls+first, ls+last)
	top := math.Point{X: t.caretWidth + x}
	bottom := top.Add(m.Point())
	t.outer.PaintSelection(c, top, bottom)
}

func (t *DefaultTextBoxLine) PaintSelections(c gxui.Canvas) {
	controller := t.textbox.controller

	ls, le := controller.LineStart(t.lineIndex), controller.LineEnd(t.lineIndex)

	selections := t.addDragging(controller.SelectionSlice())
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
			t.paintSelection(c, ls, le, 0, le-ls)
			return
		}
		start -= ls
		if start < 0 {
			start = 0
		}
		end -= ls
		t.paintSelection(c, ls, le, start, end)
	}
}

func (t *DefaultTextBoxLine) PaintCaret(c gxui.Canvas, top, bottom math.Point) {
	r := math.Rect{Min: top, Max: bottom}.ExpandI(t.caretWidth / 2)
	c.DrawRoundedRect(r, 1, 1, 1, 1, gxui.CreatePen(0.5, gxui.Gray70), gxui.WhiteBrush)
}

func (t *DefaultTextBoxLine) PaintSelection(c gxui.Canvas, top, bottom math.Point) {
	r := math.Rect{Min: top, Max: bottom}.ExpandI(t.caretWidth / 2)
	c.DrawRoundedRect(r, 1, 1, 1, 1, gxui.TransparentPen, gxui.Brush{Color: gxui.Gray40})
}

// TextBoxLine compliance
func (t *DefaultTextBoxLine) RuneIndexAt(p math.Point) int {
	font := t.textbox.font
	controller := t.textbox.controller

	x := p.X
	line := controller.Line(t.lineIndex)
	i := 0
	count := utf8.RuneCountInString(line)
	for ; i < count && x > font.Measure(&gxui.TextBlock{Runes: []rune(line[:i+1])}).W; i++ {
	}

	return controller.LineStart(t.lineIndex) + i
}

func (t *DefaultTextBoxLine) PositionAt(runeIndex int) math.Point {
	font := t.textbox.font
	controller := t.textbox.controller

	x := runeIndex - controller.LineStart(t.lineIndex)
	runes := controller.LineRunes(t.lineIndex)
	return font.Measure(&gxui.TextBlock{Runes: runes[:x]}).Point()
}
