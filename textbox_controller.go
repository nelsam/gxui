// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gxui

import (
	"fmt"
	"log"
	"runtime/debug"
	"sort"
	"strings"
	"unicode"
	"unsafe"

	"git.sr.ht/~nelsam/gxui/math"
)

type TextBoxEdit struct {
	At, Delta int
	Old, New  []rune
}

// TextBoxController is a goroutine-safe controller for a text
// box.  Access to the underlying data is performed with atomics,
// so it should be pretty responsive.
type TextBoxController struct {
	// updated atomically to keep the textbox controller mostly
	// goroutine-safe.
	//
	// WARNING: make sure that any 32-bit fields come *AFTER*
	// all of the 64-bit fields, for 32-bit OS compatibility.
	textPtr                   unsafe.Pointer // []rune
	locationHistoryIndexSrc   int64
	indentPtr                 unsafe.Pointer // string
	lineStartsPtr             unsafe.Pointer // []int
	lineEndsPtr               unsafe.Pointer // []int
	locationHistoryPtr        unsafe.Pointer // [][]int
	selectionsPtr             unsafe.Pointer // []TextSelection
	nextSelectionChangedIDSrc int64
	onSelectionChangedPtr     unsafe.Pointer // map[int64]func()
	nextTextChangedIDSrc      int64
	onTextChangedPtr          unsafe.Pointer // map[int64]func([]TextBoxEdit)

	// !!! Separator between 64-bit and 32-bit fields !!!

	storeCaretLocationsNextEditSrc uint32
}

func CreateTextBoxController() *TextBoxController {
	return &TextBoxController{}
}

func (t *TextBoxController) TextEdited(edits []TextBoxEdit) {
	t.updateSelectionsForEdits(edits)
	t.textChanged(edits)
}

func (t *TextBoxController) updateSelectionsForEdits(edits []TextBoxEdit) {
	min := 0
	max := len(t.text())
	var selections []TextSelection
	for _, selection := range t.selections() {
		for _, e := range edits {
			start := e.At
			if e.Delta < 0 {
				start -= e.Delta
			}
			delta := e.Delta
			if selection.start >= start {
				selection.start += delta
			}
			if selection.end >= start {
				selection.end += delta
			}
		}
		if selection.end < selection.start {
			selection.end = selection.start
		}
		selection.start = math.Clamp(selection.start, min, max)
		selection.end = math.Clamp(selection.end, min, max)
		selection = selection.Store()
		selections = append(selections, selection)
	}
	t.setSelections(selections)
}

func (t *TextBoxController) SetTextRunesNoEvent(text []rune) {
	t.setText(text)
	var lineStarts []int
	var lineEnds []int

	lineStarts = append(lineStarts, 0)
	for i, r := range text {
		if r == '\n' {
			lineEnds = append(lineEnds, i)
			lineStarts = append(lineStarts, i+1)
		}
	}
	lineEnds = append(lineEnds, len(text))

	t.setLineStarts(lineStarts)
	t.setLineEnds(lineEnds)
}

func (t *TextBoxController) maybeStoreCaretLocations() {
	if t.storeCaretLocationsNextEdit() {
		t.StoreCaretLocations()
		t.setStoreCaretLocationsNextEdit(false)
	}
}

func (t *TextBoxController) Indent() string {
	return t.indent()
}

func (t *TextBoxController) SetIndent(indent string) {
	for _, r := range indent {
		if !unicode.IsSpace(r) {
			panic("TextBoxController: indent contained non-space characters")
		}
	}
	t.setIndent(indent)
}

func (t *TextBoxController) StoreCaretLocations() {
	idx, history := t.locationHistoryIndex(), t.locationHistory()
	if int(idx) < len(history) {
		history = history[:idx]
	}
	t.setLocationHistory(append(history, t.Carets()))
	t.setLocationHistoryIndex(int64(len(history)))
}

func (t *TextBoxController) OnSelectionChanged(f func()) EventSubscription {
	id := t.nextSelectionChangedID()
	m := t.onSelectionChanged()
	if m == nil {
		m = make(map[int64]func())
	}
	if _, ok := m[id]; ok {
		panic(fmt.Errorf("ID %d already existed in selection change", id))
	}
	m[id] = f
	t.setOnSelectionChanged(m)
	return dumbUnlistener(func() {
		m := t.onSelectionChanged()
		delete(m, id)
		t.setOnSelectionChanged(m)
	})
}

func (t *TextBoxController) OnTextChanged(f func([]TextBoxEdit)) EventSubscription {
	id := t.nextTextChangedID()
	m := t.onTextChanged()
	if m == nil {
		m = make(map[int64]func([]TextBoxEdit))
	}
	if _, ok := m[id]; ok {
		panic(fmt.Errorf("ID %d already existed in text change map", id))
	}
	m[id] = f
	t.setOnTextChanged(m)
	return dumbUnlistener(func() {
		m := t.onTextChanged()
		delete(m, id)
		t.setOnTextChanged(m)
	})
}

func (t *TextBoxController) SelectionCount() int {
	return len(t.selections())
}

func (t *TextBoxController) Selection(i int) TextSelection {
	return t.selections()[i]
}

func (t *TextBoxController) Selections() TextSelectionList {
	log.Printf("DEPRECATION WARNING: TextSelectionList is going away!  " +
		"Please update your code to treat the return value of Selections() " +
		"as a []gxui.TextSelection instead.  We are providing a " +
		"TextBoxController.SelectionSlice() []gxui.TextSelection method for " +
		"a transitionary period.  Stack:")
	debug.PrintStack()
	return t.SelectionSlice()
}

func (t *TextBoxController) SelectionSlice() []TextSelection {
	return t.selections()
}

func (t *TextBoxController) SelectionText(i int) string {
	sel := t.selections()[i]
	runes := t.text()[sel.start:sel.end]
	return RuneArrayToString(runes)
}

func (t *TextBoxController) SelectionLineText(i int) string {
	sel := t.selections()[i]
	line := t.LineIndex(sel.start)
	runes := t.text()[t.LineStart(line):t.LineEnd(line)]
	return RuneArrayToString(runes)
}

func (t *TextBoxController) Caret(i int) int {
	if i >= len(t.selections()) {
		return 0
	}
	return t.selections()[i].Caret()
}

func (t *TextBoxController) Carets() []int {
	sel := t.selections()
	l := make([]int, len(sel))
	for i, s := range sel {
		l[i] = s.Caret()
	}
	return l
}

func (t *TextBoxController) FirstCaret() int {
	return t.Caret(0)
}

func (t *TextBoxController) LastCaret() int {
	return t.Caret(t.SelectionCount() - 1)
}

func (t *TextBoxController) FirstSelection() TextSelection {
	return t.Selection(0)
}

func (t *TextBoxController) LastSelection() TextSelection {
	return t.Selection(t.SelectionCount() - 1)
}

func (t *TextBoxController) LineCount() int {
	return len(t.lineStarts())
}

func (t *TextBoxController) Line(i int) string {
	return RuneArrayToString(t.LineRunes(i))
}

func (t *TextBoxController) LineRunes(i int) []rune {
	s := t.LineStart(i)
	e := t.LineEnd(i)
	return t.text()[s:e]
}

func (t *TextBoxController) LineStart(i int) int {
	if t.LineCount() == 0 {
		return 0
	}
	return t.lineStarts()[i]
}

func (t *TextBoxController) LineEnd(i int) int {
	if t.LineCount() == 0 {
		return 0
	}
	return t.lineEnds()[i]
}

func (t *TextBoxController) LineIndent(lineIndex int) int {
	line := t.Line(lineIndex)
	indentLen := len(t.Indent())
	if indentLen == 0 {
		return 0
	}
	i := 0
	for ; (i+1)*indentLen < len(line) && line[i*indentLen:(i+1)*indentLen] == t.Indent(); i++ {
	}
	return i
}

func (t *TextBoxController) LineIndex(p int) int {
	lineEnds := t.lineEnds()
	return sort.Search(len(t.lineStarts()), func(i int) bool {
		return p <= lineEnds[i]
	})
}

func (t *TextBoxController) Text() string {
	return RuneArrayToString(t.text())
}

func (t *TextBoxController) TextRange(s, e int) string {
	return RuneArrayToString(t.text()[s:e])
}

func (t *TextBoxController) TextRunes() []rune {
	return t.text()
}

func (t *TextBoxController) SetText(str string) {
	t.SetTextRunes(StringToRuneArray(str))
}

func (t *TextBoxController) SetTextRunes(text []rune) {
	t.SetTextRunesNoEvent(text)
	t.TextEdited([]TextBoxEdit{})
}

func (t *TextBoxController) SetTextEdits(text []rune, edits []TextBoxEdit) {
	t.SetTextRunesNoEvent(text)
	t.TextEdited(edits)
}

func (t *TextBoxController) IndexFirst(sel TextSelection) TextSelection {
	sel.start = 0
	sel.end = 0
	return sel.Store()
}

func (t *TextBoxController) IndexLast(sel TextSelection) TextSelection {
	end := len(t.text())
	sel.start = end
	sel.end = end
	return sel.Store()
}

func (t *TextBoxController) IndexLeft(sel TextSelection) TextSelection {
	sel.start = math.Max(sel.start-1, 0)
	sel.end = math.Max(sel.end-1, 0)
	return sel.Store()
}

func (t *TextBoxController) IndexRight(sel TextSelection) TextSelection {
	text := t.text()
	sel.start = math.Min(sel.start+1, len(text))
	sel.end = math.Min(sel.end+1, len(text))
	return sel.Store()
}

func (t *TextBoxController) IndexWordLeft(sel TextSelection) TextSelection {
	sel.start = t.indexWordLeft(sel.start)
	sel.end = t.indexWordLeft(sel.end)
	return sel.Store()
}

func (t *TextBoxController) indexWordLeft(i int) int {
	i--
	if i <= 0 {
		return 0
	}
	for ; i > 0 && t.RuneInWord(t.text()[i]); i-- {
	}
	return i
}

func (t *TextBoxController) IndexWordRight(sel TextSelection) TextSelection {
	sel.start = t.indexWordRight(sel.start)
	sel.end = t.indexWordRight(sel.end)
	return sel.Store()
}

func (t *TextBoxController) indexWordRight(i int) int {
	text := t.text()
	i++
	if i >= len(text) {
		return len(text)
	}
	for ; i < len(text) && t.RuneInWord(text[i]); i++ {
	}
	return i
}

func (t *TextBoxController) IndexUp(sel TextSelection) TextSelection {
	sel.start = t.indexUp(sel.start, sel.storedStart)
	sel.end = t.indexUp(sel.end, sel.storedEnd)
	if sel.start == 0 {
		return sel.Store()
	}
	return sel
}

func (t *TextBoxController) indexUp(i, stored int) int {
	line := t.LineIndex(i)
	storedLine := t.LineIndex(stored)
	x := stored - t.LineStart(storedLine)
	if line > 0 {
		return math.Min(t.LineStart(line-1)+x, t.LineEnd(line-1))
	}
	return 0
}

func (t *TextBoxController) IndexDown(sel TextSelection) TextSelection {
	sel.start = t.indexDown(sel.start, sel.storedStart)
	sel.end = t.indexDown(sel.end, sel.storedEnd)
	if sel.end == len(t.text()) {
		return sel.Store()
	}
	return sel
}

func (t *TextBoxController) indexDown(i, stored int) int {
	line := t.LineIndex(i)
	storedLine := t.LineIndex(stored)
	x := stored - t.LineStart(storedLine)
	if line < t.LineCount()-1 {
		return math.Min(t.LineStart(line+1)+x, t.LineEnd(line+1))
	}
	return len(t.text())
}

func (t *TextBoxController) IndexHome(sel TextSelection) TextSelection {
	sel.start = t.indexHome(sel.start)
	sel.end = t.indexHome(sel.end)
	return sel.Store()
}

func (t *TextBoxController) indexHome(i int) int {
	line := t.LineIndex(i)
	start := t.LineStart(line)
	indent := t.LineIndent(line)
	if start+indent < i {
		return start + indent
	}
	return start
}

func (t *TextBoxController) IndexEnd(sel TextSelection) TextSelection {
	sel.start = t.LineEnd(t.LineIndex(sel.start))
	sel.end = t.LineEnd(t.LineIndex(sel.end))
	return sel.Store()
}

type SelectionTransform func(TextSelection) TextSelection

func (t *TextBoxController) ClearSelections() {
	t.setStoreCaretLocationsNextEdit(true)
	t.SetCaret(t.Caret(0))
}

func (t *TextBoxController) SetCaret(c int) {
	t.setStoreCaretLocationsNextEdit(true)
	t.SetSelection(CreateTextSelection(c, c, false))
}

func (t *TextBoxController) AddCaret(c int) {
	t.AddSelection(CreateTextSelection(c, c, false))
}

func (t *TextBoxController) AddSelection(s TextSelection) {
	t.setStoreCaretLocationsNextEdit(true)
	sel := t.selections()
	if len(sel) == 0 {
		t.setSelections([]TextSelection{s})
		return
	}
	var i int
	for i = range sel {
		if sel[i].Start() > s.Start() {
			break
		}
	}
	sel = append(sel, TextSelection{})
	copy(sel[i+1:], sel[i:len(sel)-1])
	sel[i] = s
	t.setSelections(sel)
	t.selectionChanged()
}

func (t *TextBoxController) SetSelection(s TextSelection) {
	t.SetSelections([]TextSelection{s})
}

func (t *TextBoxController) SetSelections(s []TextSelection) {
	t.setStoreCaretLocationsNextEdit(true)
	t.setSelections(s)
	t.selectionChanged()
}

func (t *TextBoxController) SelectAll() {
	t.setStoreCaretLocationsNextEdit(true)
	t.SetSelection(CreateTextSelection(0, len(t.text()), false))
}

func (t *TextBoxController) RestorePreviousSelections() {
	idx, history := t.locationHistoryIndex(), t.locationHistory()
	if int(idx) == len(history) {
		t.StoreCaretLocations()
		idx--
	}
	if idx > 0 {
		idx--
		locations := history[idx]
		sel := make([]TextSelection, len(locations))
		for i, l := range locations {
			sel[i] = CreateTextSelection(l, l, false)
		}
		t.setSelections(sel)
		t.selectionChanged()
	}
}

func (t *TextBoxController) RestoreNextSelections() {
	idx, history := t.locationHistoryIndex(), t.locationHistory()
	if int(idx) < len(history)-1 {
		idx++
		locations := history[idx]
		sel := make([]TextSelection, len(locations))
		for i, l := range locations {
			sel[i] = CreateTextSelection(l, l, false)
		}
		t.setSelections(sel)
		t.selectionChanged()
	}
}

func (t *TextBoxController) AddCarets(transform SelectionTransform) {
	t.setStoreCaretLocationsNextEdit(true)
	add := t.selections()
	for i, s := range add {
		add[i] = transform(s)
	}
	sel := t.selections()
	sel = append(sel, add...)
	var j int
	for i := 0; i < len(sel); i++ {
		if sel[i].Start() > add[j].Start() {
			copy(sel[i+1:], sel[i:len(sel)-1])
			sel[i] = add[j]
			j++
			if j == len(add) {
				break
			}
		}
	}
	t.setSelections(sel)
	t.selectionChanged()
}

func (t *TextBoxController) growSelection(a, b TextSelection) TextSelection {
	if (a.caretAtStart && a.end < b.start) || (!a.caretAtStart && a.start > b.end) {
		a.caretAtStart = !a.caretAtStart
	}
	if a.caretAtStart {
		a.start = b.start
		a.storedEnd = b.storedEnd
	} else {
		a.end = b.end
		a.storedStart = b.storedStart
	}
	return a
}

func (t *TextBoxController) GrowSelections(transform SelectionTransform) {
	t.setStoreCaretLocationsNextEdit(true)
	sel := t.selections()
	for i, s := range sel {
		sel[i] = t.growSelection(s, transform(s))
	}
	t.setSelections(sel)
	t.selectionChanged()
}

func (t *TextBoxController) MoveSelections(transform SelectionTransform) {
	t.setStoreCaretLocationsNextEdit(true)
	sel := t.selections()
	for i, s := range sel {
		sel[i] = transform(s)
	}
	t.setSelections(sel)
	t.selectionChanged()
}

func (t *TextBoxController) AddCaretsUp()       { t.AddCarets(t.IndexUp) }
func (t *TextBoxController) AddCaretsDown()     { t.AddCarets(t.IndexDown) }
func (t *TextBoxController) SelectFirst()       { t.GrowSelections(t.IndexFirst) }
func (t *TextBoxController) SelectLast()        { t.GrowSelections(t.IndexLast) }
func (t *TextBoxController) SelectLeft()        { t.GrowSelections(t.IndexLeft) }
func (t *TextBoxController) SelectRight()       { t.GrowSelections(t.IndexRight) }
func (t *TextBoxController) SelectUp()          { t.GrowSelections(t.IndexUp) }
func (t *TextBoxController) SelectDown()        { t.GrowSelections(t.IndexDown) }
func (t *TextBoxController) SelectHome()        { t.GrowSelections(t.IndexHome) }
func (t *TextBoxController) SelectEnd()         { t.GrowSelections(t.IndexEnd) }
func (t *TextBoxController) SelectLeftByWord()  { t.GrowSelections(t.IndexWordLeft) }
func (t *TextBoxController) SelectRightByWord() { t.GrowSelections(t.IndexWordRight) }
func (t *TextBoxController) MoveFirst()         { t.MoveSelections(t.IndexFirst) }
func (t *TextBoxController) MoveLast()          { t.MoveSelections(t.IndexLast) }
func (t *TextBoxController) MoveLeft()          { t.MoveSelections(t.IndexLeft) }
func (t *TextBoxController) MoveRight()         { t.MoveSelections(t.IndexRight) }
func (t *TextBoxController) MoveUp()            { t.MoveSelections(t.IndexUp) }
func (t *TextBoxController) MoveDown()          { t.MoveSelections(t.IndexDown) }
func (t *TextBoxController) MoveLeftByWord()    { t.MoveSelections(t.IndexWordLeft) }
func (t *TextBoxController) MoveRightByWord()   { t.MoveSelections(t.IndexWordRight) }
func (t *TextBoxController) MoveHome()          { t.MoveSelections(t.IndexHome) }
func (t *TextBoxController) MoveEnd()           { t.MoveSelections(t.IndexEnd) }

func (t *TextBoxController) Delete() {
	t.maybeStoreCaretLocations()
	text := t.text()
	edits := []TextBoxEdit{}
	sel := t.selections()
	for i := len(sel) - 1; i >= 0; i-- {
		s := sel[i]
		if s.start == s.end && s.end < len(text) {
			old := append([]rune{}, text[s.start])
			copy(text[s.start:], text[s.start+1:])
			text = text[:len(text)-1]
			edits = append(edits, TextBoxEdit{
				At:    s.start,
				Delta: -1,
				Old:   old,
			})
		} else {
			old := append([]rune{}, text[s.start:s.end]...)
			copy(text[s.start:], text[s.end:])
			length := s.Length()
			text = text[:len(text)-length]
			edits = append(edits, TextBoxEdit{
				At:    s.start,
				Delta: -length,
				Old:   old,
			})
		}
		sel[i] = CreateTextSelection(s.end, s.end, false)
	}
	t.setSelections(sel)
	t.SetTextEdits(text, edits)
}

func (t *TextBoxController) Backspace() {
	t.maybeStoreCaretLocations()
	text := t.text()
	edits := []TextBoxEdit{}
	sel := t.selections()
	for i := len(sel) - 1; i >= 0; i-- {
		s := sel[i]
		if s.start == s.end && s.start > 0 {
			old := append([]rune{}, text[s.start-1])
			copy(text[s.start-1:], text[s.start:])
			text = text[:len(text)-1]
			edits = append(edits, TextBoxEdit{
				At:    s.start - 1,
				Delta: -1,
				Old:   old,
			})
		} else {
			old := append([]rune{}, text[s.start:s.end]...)
			copy(text[s.start:], text[s.end:])
			l := s.Length()
			text = text[:len(text)-l]
			edits = append(edits, TextBoxEdit{
				At:    s.start,
				Delta: -l,
				Old:   old,
			})
		}
		sel[i] = CreateTextSelection(s.end, s.end, false)
	}
	t.setSelections(sel)
	t.SetTextEdits(text, edits)
}

func (t *TextBoxController) ReplaceAll(str string) []TextBoxEdit {
	return t.Replace(func(TextSelection) string { return str })
}

func (t *TextBoxController) ReplaceAllRunes(str []rune) []TextBoxEdit {
	return t.ReplaceRunes(func(TextSelection) []rune { return str })
}

func (t *TextBoxController) Replace(f func(sel TextSelection) string) []TextBoxEdit {
	return t.ReplaceRunes(func(s TextSelection) []rune { return StringToRuneArray(f(s)) })
}

func (t *TextBoxController) ReplaceRunes(f func(sel TextSelection) []rune) (edits []TextBoxEdit) {
	t.maybeStoreCaretLocations()
	var (
		text = t.text()
		edit TextBoxEdit
	)
	sel := t.selections()
	for i := len(sel) - 1; i >= 0; i-- {
		s := sel[i]
		text, edit = t.ReplaceAt(text, s.start, s.end, f(s))
		edits = append(edits, edit)
	}
	t.SetTextRunesNoEvent(text)
	t.TextEdited(edits)
	return edits
}

func (t *TextBoxController) ReplaceAt(text []rune, s, e int, replacement []rune) ([]rune, TextBoxEdit) {
	replacementLen := len(replacement)
	delta := replacementLen - (e - s)
	if delta > 0 {
		text = append(text, make([]rune, delta)...)
	}
	old := append([]rune{}, text[s:e]...)
	copy(text[e+delta:], text[e:])
	copy(text[s:], replacement)
	if delta < 0 {
		text = text[:len(text)+delta]
	}
	return text, TextBoxEdit{
		At:    s,
		Delta: delta,
		Old:   old,
		New:   replacement,
	}
}

func (t *TextBoxController) ReplaceWithNewline() {
	t.ReplaceAll("\n")
	t.Deselect(false)
}

func (t *TextBoxController) ReplaceWithNewlineKeepIndent() {
	t.Replace(func(sel TextSelection) string {
		s, _ := sel.Range()
		indent := t.LineIndent(t.LineIndex(s))
		return "\n" + strings.Repeat(t.Indent(), indent)
	})
	t.Deselect(false)
}

func (t *TextBoxController) IndentSelection() {
	var (
		edits    []TextBoxEdit
		edit     TextBoxEdit
		text     = t.text()
		sel      = t.selections()
		indent   = t.Indent()
		lastLine = -1
	)
	for i := len(sel) - 1; i >= 0; i-- {
		s := sel[i]
		lis, lie := t.LineIndex(s.start), t.LineIndex(s.end)
		if lastLine == lie {
			lie--
		}
		for l := lie; l >= lis; l-- {
			ls := t.LineStart(l)
			text, edit = t.ReplaceAt(text, ls, ls, []rune(indent))
			edits = append(edits, edit)
		}
		lastLine = lis
	}
	t.SetTextEdits(text, edits)
}

func (t *TextBoxController) UnindentSelection() {
	var (
		edits    []TextBoxEdit
		edit     TextBoxEdit
		text     = t.text()
		sel      = t.selections()
		indent   = t.Indent()
		lastLine = -1
	)
	for i := len(sel) - 1; i >= 0; i-- {
		s := sel[i]
		lis, lie := t.LineIndex(s.start), t.LineIndex(s.end)
		if lastLine == lie {
			lie--
		}
		for l := lie; l >= lis; l-- {
			indents := t.LineIndent(l)
			if indents > 0 {
				ls := t.LineStart(l)
				text, edit = t.ReplaceAt(text, ls, ls+len(indent), []rune{})
				edits = append(edits, edit)
			}
		}
		lastLine = lis
	}
	t.SetTextEdits(text, edits)
}

func (t *TextBoxController) RuneInWord(r rune) bool {
	switch {
	case unicode.IsLetter(r), unicode.IsNumber(r), r == '_':
		return true
	default:
		return false
	}
}

func (t *TextBoxController) WordAt(runeIdx int) (s, e int) {
	text := t.text()
	s, e = runeIdx, runeIdx
	for s > 0 && t.RuneInWord(text[s-1]) {
		s--
	}
	for e < len(text) && t.RuneInWord(text[e]) {
		e++
	}
	return s, e
}

func (t *TextBoxController) Deselect(moveCaretToStart bool) (deselected bool) {
	deselected = false
	sel := t.selections()
	for i, s := range sel {
		if s.start == s.end {
			continue
		}
		deselected = true
		if moveCaretToStart {
			s.end = s.start
		} else {
			s.start = s.end
		}
		sel[i] = s
	}
	if deselected {
		t.setSelections(sel)
		t.selectionChanged()
	}
	return
}

func (t *TextBoxController) LineAndRow(index int) (line, row int) {
	line = t.LineIndex(index)
	row = index - t.LineStart(line)
	return
}
