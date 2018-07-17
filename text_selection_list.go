// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gxui

import "github.com/nelsam/gxui/interval"

type TextSelectionList []TextSelection

func (l TextSelectionList) Transform(transform SelectionTransform) TextSelectionList {
	res := TextSelectionList{}
	for _, s := range l {
		interval.Merge(&res, transform(s))
	}
	return res
}

func (l TextSelectionList) TransformCarets(transform SelectionTransform) TextSelectionList {
	res := TextSelectionList{}
	for _, s := range l {
		moved := transform(s)
		if s.caretAtStart {
			s.start = moved.start
			s.storedStart = moved.storedStart
		} else {
			s.end = moved.end
			s.storedEnd = moved.storedEnd
		}
		if s.start > s.end {
			s.start, s.end = s.end, s.start
			s.caretAtStart = !s.caretAtStart
		}
		interval.Merge(&res, s)
	}
	return res
}

func (l TextSelectionList) Len() int {
	return len(l)
}

func (l TextSelectionList) Cap() int {
	return cap(l)
}

func (l *TextSelectionList) SetLen(length int) {
	*l = (*l)[:length]
}

func (l *TextSelectionList) GrowTo(length, capacity int) {
	old := *l
	*l = make(TextSelectionList, length, capacity)
	copy(*l, old)
}

func (l TextSelectionList) Copy(to, from, count int) {
	copy(l[to:to+count], l[from:from+count])
}

func (l TextSelectionList) Interval(index int) (start, end uint64) {
	return l[index].Span()
}

func (l TextSelectionList) SetInterval(index int, start, end uint64) {
	l[index].start = int(start)
	l[index].end = int(end)
}

func (l TextSelectionList) MergeData(index int, i interval.Node) {
	sel := i.(TextSelection)
	l[index].caretAtStart = sel.caretAtStart
	l[index].storedStart = sel.storedStart
	l[index].storedEnd = sel.storedEnd
}
