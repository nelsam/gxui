// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mixins

import (
	"context"
	"log"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"git.sr.ht/~nelsam/gxui"
	"git.sr.ht/~nelsam/gxui/math"
	"git.sr.ht/~nelsam/gxui/mixins/parts"
)

type TextBoxLine interface {
	gxui.Control
	RuneIndexAt(math.Point) int
	PositionAt(int) math.Point
	SetOffset(int)
}

type TextBoxOuter interface {
	ListOuter
	MaxLineWidth() int
	CreateLine(theme gxui.Theme, index int) (line TextBoxLine, container gxui.Control)
}

// TextBox is a mixin for text boxes.  It is not guaranteed to be goroutine-safe, but
// simple accessors to the underlying text is controlled by gxui.TextBoxController,
// which is itself goroutine-safe.
//
// It's encouraged to develop with the driver in debug mode so that functions that must
// be called on the UI goroutine will panic if they are called on non-UI goroutines.
type TextBox struct {
	List
	gxui.AdapterBase
	parts.Focusable

	outer             TextBoxOuter
	driver            gxui.Driver
	font              gxui.Font
	textColor         gxui.Color
	onRedrawLines     gxui.Event
	multiline         bool
	controller        *gxui.TextBoxController
	adapter           *TextBoxAdapter
	selectionDragging bool
	selectionDrag     gxui.TextSelection
	desiredWidth      int
	startOffset       int

	horizScroll      gxui.ScrollBar
	horizScrollChild *gxui.Child
	horizOffset      int
	horizScrollES    gxui.EventSubscription
	maxLineWidth     int

	stopScrolling  func()
	selectionPoint math.Point
	selectionMu    sync.Mutex
}

func (t *TextBox) lineMouseDown(line TextBoxLine, ev gxui.MouseEvent) {
	if ev.Button == gxui.MouseButtonLeft {
		t.startOffset = t.List.ScrollOffset()
		p := line.RuneIndexAt(ev.Point)
		t.selectionDragging = true
		t.selectionDrag = gxui.CreateTextSelection(p, p, false)
		if !ev.Modifier.Control() {
			t.controller.SetCaret(p)
		}
	}
}

func (t *TextBox) lineMouseUp(line TextBoxLine, ev gxui.MouseEvent) {
	if ev.Button == gxui.MouseButtonLeft {
		t.startOffset = math.Min(t.startOffset, t.List.ScrollOffset())
		t.selectionDragging = false
		if t.stopScrolling != nil {
			t.stopScrolling()
			t.stopScrolling = nil
		}
		if !ev.Modifier.Control() {
			t.controller.SetSelection(t.selectionDrag)
		} else {
			t.controller.AddSelection(t.selectionDrag)
		}
	}
}

func (t *TextBox) Init(outer TextBoxOuter, driver gxui.Driver, theme gxui.Theme, font gxui.Font) {
	t.List.Init(outer, theme)
	t.Focusable.Init(outer)
	t.outer = outer
	t.driver = driver
	t.font = font
	t.onRedrawLines = gxui.CreateEvent(func() {})
	t.controller = gxui.CreateTextBoxController()
	t.adapter = &TextBoxAdapter{TextBox: t}
	t.desiredWidth = 100
	t.SetScrollBarEnabled(false) // Defaults to single line
	t.OnGainedFocus(func() { t.onRedrawLines.Fire() })
	t.OnLostFocus(func() { t.onRedrawLines.Fire() })
	t.horizScroll = theme.CreateScrollBar()
	t.horizScrollChild = t.AddChild(t.horizScroll)
	t.horizScroll.SetOrientation(gxui.Horizontal)
	t.horizScrollES = t.horizScroll.OnScroll(func(from, to int) {
		t.SetHorizOffset(from)
	})

	t.controller.OnTextChanged(func(l []gxui.TextBoxEdit) {
		t.onRedrawLines.Fire()
		t.List.DataChanged(false)
	})
	t.controller.OnSelectionChanged(func() {
		t.onRedrawLines.Fire()
	})

	t.List.SetAdapter(t.adapter)

	// Interface compliance test
	_ = gxui.TextBox(t)
}

func (t *TextBox) MaxLineWidth() int {
	return t.maxLineWidth
}

func (t *TextBox) lineWidthOffset() int {
	return findLineOffset(t.Children()[0])
}

func findLineOffset(child *gxui.Child) int {
	switch src := child.Control.(type) {
	case TextBoxLine:
		return child.Offset.X
	case gxui.Parent:
		for _, child := range src.Children() {
			if offset := findLineOffset(child); offset != -1 {
				return child.Offset.X + offset
			}
		}
	}
	return -1
}

func (t *TextBox) LayoutChildren() {
	t.List.LayoutChildren()
	if t.scrollBarEnabled {
		size := t.Size().Contract(t.Padding())
		scrollAreaSize := size
		scrollAreaSize.W -= t.scrollBar.Size().W

		offset := t.Padding().LT()
		barSize := t.horizScroll.DesiredSize(math.ZeroSize, scrollAreaSize)
		t.horizScrollChild.Layout(math.CreateRect(0, size.H-barSize.H, scrollAreaSize.W, size.H).Canon().Offset(offset))

		maxLineWidth := t.outer.MaxLineWidth()
		entireContentVisible := size.W > maxLineWidth
		t.horizScroll.SetVisible(!entireContentVisible)
		if entireContentVisible && t.horizOffset != 0 {
			t.SetHorizOffset(0)
		}
	}
}

func (t *TextBox) updateChildOffsets(parent gxui.Parent, offset int) {
	for _, child := range parent.Children() {
		switch src := child.Control.(type) {
		case TextBoxLine:
			src.SetOffset(offset)
		case gxui.Parent:
			t.updateChildOffsets(src, offset)
		}
	}
}

func (t *TextBox) updateHorizScrollLimit() {
	maxWidth := t.MaxLineWidth()
	size := t.Size().Contract(t.outer.Padding())
	maxScroll := math.Max(maxWidth-size.W, 0)
	math.Clamp(t.horizOffset, 0, maxScroll)
	t.horizScroll.SetScrollLimit(maxWidth)
}

func (t *TextBox) HorizOffset() int {
	return t.horizOffset
}

func (t *TextBox) SetHorizOffset(offset int) {
	t.updateHorizScrollLimit()
	t.updateChildOffsets(t, offset)
	t.horizScroll.SetScrollPosition(offset, offset+t.Size().W)
	if t.horizOffset != offset {
		t.horizOffset = offset
		t.LayoutChildren()
	}
}

func (t *TextBox) SetSize(size math.Size) {
	t.List.SetSize(size)
	t.SetHorizOffset(t.horizOffset)
}

func (t *TextBox) SizeChanged() {
	t.SetHorizOffset(t.horizOffset)
	t.outer.Relayout()
}

func (t *TextBox) textRect() math.Rect {
	return t.outer.Size().Rect().Contract(t.Padding())
}

func (t *TextBox) pageLines() int {
	return (t.outer.Size().H - t.outer.Padding().H()) / t.MajorAxisItemSize()
}

func (t *TextBox) Controller() *gxui.TextBoxController {
	return t.controller
}

func (t *TextBox) OnRedrawLines(f func()) gxui.EventSubscription {
	return t.onRedrawLines.Listen(f)
}

func (t *TextBox) OnSelectionChanged(f func()) gxui.EventSubscription {
	return t.controller.OnSelectionChanged(f)
}

func (t *TextBox) OnTextChanged(f func([]gxui.TextBoxEdit)) gxui.EventSubscription {
	return t.controller.OnTextChanged(f)
}

func (t *TextBox) Runes() []rune {
	return t.controller.TextRunes()
}

func (t *TextBox) Text() string {
	return t.controller.Text()
}

func (t *TextBox) SetText(text string) {
	t.controller.SetText(text)
	t.outer.Relayout()
}

func (t *TextBox) TextColor() gxui.Color {
	return t.textColor
}

func (t *TextBox) SetTextColor(color gxui.Color) {
	t.textColor = color
	t.Relayout()
}

func (t *TextBox) Font() gxui.Font {
	return t.font
}

func (t *TextBox) SetFont(font gxui.Font) {
	if t.font != font {
		t.font = font
		t.Relayout()
	}
}

func (t *TextBox) Multiline() bool {
	return t.multiline
}

func (t *TextBox) SetMultiline(multiline bool) {
	if t.multiline != multiline {
		t.multiline = multiline
		t.SetScrollBarEnabled(multiline)
		t.outer.Relayout()
	}
}

func (t *TextBox) DesiredWidth() int {
	return t.desiredWidth
}

func (t *TextBox) SetDesiredWidth(desiredWidth int) {
	if t.desiredWidth != desiredWidth {
		t.desiredWidth = desiredWidth
		t.SizeChanged()
	}
}

func (t *TextBox) StartOffset() int {
	return t.startOffset
}

func (t *TextBox) SelectionPoint() math.Point {
	t.selectionMu.Lock()
	defer t.selectionMu.Unlock()
	return t.selectionPoint
}

func (t *TextBox) Select(sel gxui.TextSelectionList) {
	log.Printf("DEPRECATION WARNING: gxui.TextSelectionList is going away!  " +
		"Please update your code to pass in a []gxui.TextSelection instead.  " +
		"We are temporarily providing a TextBox.SelectSlice([]gxui.TextSelection) " +
		"method for a transitionary period.")
	debug.PrintStack()
	t.SelectSlice(sel)
}

func (t *TextBox) SelectSlice(sel []gxui.TextSelection) {
	t.controller.StoreCaretLocations()
	t.controller.SetSelections(sel)
	// Use two scroll tos to try and display all selections (if it fits on screen)
	t.ScrollToRune(t.controller.FirstSelection().First())
	t.ScrollToRune(t.controller.LastSelection().Last())
}

func (t *TextBox) SelectAll() {
	t.controller.StoreCaretLocations()
	t.controller.SelectAll()
	t.ScrollToRune(t.controller.FirstCaret())
}

func (t *TextBox) Carets() []int {
	return t.controller.Carets()
}

func (t *TextBox) RuneIndexAt(pnt math.Point) (index int, found bool) {
	for _, child := range gxui.ControlsUnder(pnt, t) {
		line, _ := child.C.(TextBoxLine)
		if line == nil {
			continue
		}

		pnt = gxui.ParentToChild(pnt, t.outer, line)
		return line.RuneIndexAt(pnt), true
	}
	return -1, false
}

func (t *TextBox) TextAt(s, e int) string {
	return t.controller.TextRange(s, e)
}

func (t *TextBox) WordAt(runeIndex int) string {
	s, e := t.controller.WordAt(runeIndex)
	return t.controller.TextRange(s, e)
}

func (t *TextBox) LineIndex(runeIndex int) int {
	return t.controller.LineIndex(runeIndex)
}

func (t *TextBox) LineStart(line int) int {
	return t.controller.LineStart(line)
}

func (t *TextBox) LineEnd(line int) int {
	return t.controller.LineEnd(line)
}

func (t *TextBox) ScrollToLine(i int) {
	t.List.ScrollTo(i)
}

func (t *TextBox) ScrollToRune(i int) {
	lineIndex := t.controller.LineIndex(i)
	t.ScrollToLine(lineIndex)

	size := t.Size()
	lineOffset := t.lineWidthOffset()
	padding := t.Padding()
	horizStart := t.horizOffset
	horizEnd := t.horizOffset + size.W - padding.W() - lineOffset
	line, _ := t.outer.CreateLine(t.theme, lineIndex)
	if i < 0 || i > len(t.Controller().TextRunes()) {
		return
	}
	pos := line.PositionAt(i)
	if horizStart > pos.X {
		t.SetHorizOffset(pos.X)
	}
	if horizEnd < pos.X {
		t.SetHorizOffset(pos.X - size.W + padding.W() + lineOffset)
	}
}

func (t *TextBox) KeyPress(ev gxui.KeyboardEvent) (consume bool) {
	switch ev.Key {
	case gxui.KeyLeft:
		switch {
		case ev.Modifier.Shift() && ev.Modifier.Control():
			t.controller.SelectLeftByWord()
		case ev.Modifier.Shift():
			t.controller.SelectLeft()
		case ev.Modifier.Alt():
			t.controller.RestorePreviousSelections()
		case !t.controller.Deselect(true):
			if ev.Modifier.Control() {
				t.controller.MoveLeftByWord()
			} else {
				t.controller.MoveLeft()
			}
		}
		t.ScrollToRune(t.controller.FirstCaret())
		return true
	case gxui.KeyRight:
		switch {
		case ev.Modifier.Shift() && ev.Modifier.Control():
			t.controller.SelectRightByWord()
		case ev.Modifier.Shift():
			t.controller.SelectRight()
		case ev.Modifier.Alt():
			t.controller.RestoreNextSelections()
		case !t.controller.Deselect(false):
			if ev.Modifier.Control() {
				t.controller.MoveRightByWord()
			} else {
				t.controller.MoveRight()
			}
		}
		t.ScrollToRune(t.controller.LastCaret())
		return true
	case gxui.KeyUp:
		switch {
		case ev.Modifier.Shift() && ev.Modifier.Alt():
			t.controller.AddCaretsUp()
		case ev.Modifier.Shift():
			t.controller.SelectUp()
		default:
			t.controller.Deselect(true)
			t.controller.MoveUp()
		}
		t.ScrollToRune(t.controller.FirstCaret())
		return true
	case gxui.KeyDown:
		switch {
		case ev.Modifier.Shift() && ev.Modifier.Alt():
			t.controller.AddCaretsDown()
		case ev.Modifier.Shift():
			t.controller.SelectDown()
		default:
			t.controller.Deselect(false)
			t.controller.MoveDown()
		}
		t.ScrollToRune(t.controller.LastCaret())
		return true
	case gxui.KeyHome:
		switch {
		case ev.Modifier.Shift() && ev.Modifier.Control():
			t.controller.SelectFirst()
		case ev.Modifier.Control():
			t.controller.MoveFirst()
		case ev.Modifier.Shift():
			t.controller.SelectHome()
		default:
			t.controller.Deselect(true)
			t.controller.MoveHome()
		}
		t.ScrollToRune(t.controller.FirstCaret())
		return true
	case gxui.KeyEnd:
		switch {
		case ev.Modifier.Shift() && ev.Modifier.Control():
			t.controller.SelectLast()
		case ev.Modifier.Control():
			t.controller.MoveLast()
		case ev.Modifier.Shift():
			t.controller.SelectEnd()
		default:
			t.controller.Deselect(false)
			t.controller.MoveEnd()
		}
		t.ScrollToRune(t.controller.LastCaret())
		return true
	case gxui.KeyPageUp:
		switch {
		case ev.Modifier.Shift():
			for i, c := 0, t.pageLines(); i < c; i++ {
				t.controller.SelectUp()
			}
		default:
			t.controller.Deselect(true)
			for i, c := 0, t.pageLines(); i < c; i++ {
				t.controller.MoveUp()
			}
		}
		t.ScrollToRune(t.controller.FirstCaret())
		return true
	case gxui.KeyPageDown:
		switch {
		case ev.Modifier.Shift():
			for i, c := 0, t.pageLines(); i < c; i++ {
				t.controller.SelectDown()
			}
		default:
			t.controller.Deselect(false)
			for i, c := 0, t.pageLines(); i < c; i++ {
				t.controller.MoveDown()
			}
		}
		t.ScrollToRune(t.controller.LastCaret())
		return true
	case gxui.KeyBackspace:
		t.controller.Backspace()
		return true
	case gxui.KeyDelete:
		t.controller.Delete()
		return true
	case gxui.KeyEnter:
		if t.multiline {
			t.controller.ReplaceWithNewline()
			return true
		}
	case gxui.KeyA:
		if ev.Modifier.Control() {
			t.controller.SelectAll()
			return true
		}
	case gxui.KeyX:
		fallthrough
	case gxui.KeyC:
		if ev.Modifier.Control() {
			parts := make([]string, t.controller.SelectionCount())
			for i, _ := range parts {
				parts[i] = t.controller.SelectionText(i)
				if parts[i] == "" {
					// Copy line instead.
					parts[i] = "\n" + t.controller.SelectionLineText(i)
				}
			}
			str := strings.Join(parts, "\n")
			t.driver.SetClipboard(str)

			if ev.Key == gxui.KeyX {
				t.controller.ReplaceAll("")
			}
			return true
		}
	case gxui.KeyV:
		if ev.Modifier.Control() {
			str, _ := t.driver.GetClipboard()
			t.controller.ReplaceAll(str)
			t.controller.Deselect(false)
			return true
		}
	case gxui.KeyEscape:
		t.controller.ClearSelections()
	}

	return t.List.KeyPress(ev)
}

func (t *TextBox) KeyStroke(ev gxui.KeyStrokeEvent) (consume bool) {
	if ev.Modifier == 0 || ev.Modifier.Shift() {
		t.controller.ReplaceAllRunes([]rune{ev.Character})
		t.controller.Deselect(false)
	}
	t.InputEventHandler.KeyStroke(ev)
	return true
}

func (t *TextBox) Click(ev gxui.MouseEvent) (consume bool) {
	t.InputEventHandler.Click(ev)
	return true
}

func (t *TextBox) DoubleClick(ev gxui.MouseEvent) (consume bool) {
	if p, ok := t.RuneIndexAt(ev.Point); ok {
		s, e := t.controller.WordAt(p)
		if ev.Modifier&gxui.ModControl != 0 {
			t.controller.AddSelection(gxui.CreateTextSelection(s, e, false))
		} else {
			t.controller.SetSelection(gxui.CreateTextSelection(s, e, false))
		}
	}
	t.InputEventHandler.DoubleClick(ev)
	return true
}

func (t *TextBox) runSelectionScroller(ctx context.Context, is_vertical bool, freq time.Duration, edge, rateMult int) {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()
	for range ticker.C {
		select {
		case <-ctx.Done():
			return
		default:
		}

		point := t.SelectionPoint()
		var rate int
		var scrl gxui.ScrollBar
		var value int
		if is_vertical {
			value = point.Y
			scrl = t.List.scrollBar
		} else {
			value = point.X
			scrl = t.horizScroll
		}
		from, to := scrl.ScrollPosition()
		width := to - from
		if value < edge && from > 0 {
			rate = (edge - value) * rateMult
			from -= rate
			to -= rate
		} else if (width-value) < edge && to < scrl.ScrollLimit() {
			rate = (value - (width - edge)) * rateMult
			from += rate
			to += rate
		}
		from = math.Clamp(from, 0, scrl.ScrollLimit())
		to = math.Clamp(to, 0, scrl.ScrollLimit())
		t.driver.CallSync(func() {
			scrl.SetScrollPosition(from, to)
		})
	}
}

func (t *TextBox) selectionScroll(ev gxui.MouseEvent) {
	if !t.selectionDragging {
		return
	}
	t.selectionMu.Lock()
	t.selectionPoint = ev.Point
	t.selectionMu.Unlock()
	p, ok := t.RuneIndexAt(ev.Point)
	if !ok {
		return
	}
	defer t.onRedrawLines.Fire()

	from := t.selectionDrag.Caret()
	if from < p {
		t.selectionDrag = gxui.CreateTextSelection(from, p, true)
	} else {
		t.selectionDrag = gxui.CreateTextSelection(p, from, false)
	}

	const (
		ScrollHFreq     = 10 * time.Millisecond
		ScrollVFreq     = 100 * time.Millisecond
		ScrollHEdge     = 80
		ScrollHRateMult = 4
		ScrollVEdge     = 40
		ScrollVRateMult = 1
	)

	if t.stopScrolling != nil {
		return
	}
	ctx, stop := context.WithCancel(context.Background())
	t.stopScrolling = stop

	go t.runSelectionScroller(ctx, false, ScrollHFreq, ScrollHEdge, ScrollHRateMult)
	go t.runSelectionScroller(ctx, true, ScrollVFreq, ScrollVEdge, ScrollVRateMult)
}

func (t *TextBox) MouseMove(ev gxui.MouseEvent) {
	t.List.MouseMove(ev)
	t.selectionScroll(ev)
}

func (t *TextBox) CreateLine(theme gxui.Theme, index int) (line TextBoxLine, container gxui.Control) {
	l := &DefaultTextBoxLine{}
	l.Init(l, theme, t, index)
	return l, l
}

func (t *TextBox) Paint(c gxui.Canvas) {
	t.maxLineWidth = 0
	t.PaintChildren.Paint(c)
}

func (t *TextBox) PaintChild(c gxui.Canvas, child *gxui.Child, idx int) {
	t.PaintChildren.PaintChild(c, child, idx)
	width := child.Control.Size().W
	if width > t.maxLineWidth {
		t.maxLineWidth = width
	}
}

// mixins.List overrides
func (t *TextBox) PaintSelection(c gxui.Canvas, r math.Rect) {}

func (t *TextBox) PaintMouseOverBackground(c gxui.Canvas, r math.Rect) {}

// gxui.AdapterCompliance
type TextBoxAdapter struct {
	gxui.DefaultAdapter
	TextBox *TextBox
}

func (t *TextBoxAdapter) Count() int {
	return math.Max(t.TextBox.controller.LineCount(), 1)
}

func (t *TextBoxAdapter) ItemAt(index int) gxui.AdapterItem {
	return index
}

func (t *TextBoxAdapter) ItemIndex(item gxui.AdapterItem) int {
	return item.(int)
}

func (t *TextBoxAdapter) Size(theme gxui.Theme) math.Size {
	tb := t.TextBox
	return math.Size{W: tb.desiredWidth, H: tb.font.GlyphMaxSize().H}
}

func (t *TextBoxAdapter) Create(theme gxui.Theme, index int) gxui.Control {
	line, container := t.TextBox.outer.CreateLine(theme, index)
	line.SetOffset(t.TextBox.horizOffset)
	line.OnMouseDown(func(ev gxui.MouseEvent) {
		t.TextBox.lineMouseDown(line, ev)
	})
	line.OnMouseUp(func(ev gxui.MouseEvent) {
		t.TextBox.lineMouseUp(line, ev)
	})
	return container
}
