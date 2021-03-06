// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gxui

type KeyboardEvent struct {
	Key      KeyboardKey
	Modifier KeyboardModifier
}

func (e KeyboardEvent) String() string {
	return concat(e.Modifier.String(), e.Key.String())
}
