package basic

import (
	"git.sr.ht/~nelsam/gxui"
	"git.sr.ht/~nelsam/gxui/mixins"
)

func CreateTableLayout(theme *Theme) gxui.TableLayout {
	l := &mixins.TableLayout{}
	l.Init(l, theme)
	return l
}
