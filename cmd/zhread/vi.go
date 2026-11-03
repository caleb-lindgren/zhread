package main

import (
	"unicode"

	"github.com/gdamore/tcell/v2"
)

// The card preview's context lines are edited with a few of vi's commands,
// in normal mode until i, a, I, A, s or C: h l 0 ^ $ w b e to move, x X r
// D s C and d or c with a motion (dw, cb, d$, dd) to change, u to undo.

// vi is the editor's state on the context under the cursor.
type vi struct {
	col    int  // the cursor, a rune offset into the context
	insert bool // insert mode, else normal
	pend   rune // d, c or r, waiting for the rest of the command
	undo   []edited
}

func (e *edited) clone() edited {
	return edited{append([]rune(nil), e.text...), e.ws, e.we}
}

// insert puts r in at offset at. Text put in at the word's start goes
// before it, and in its middle, into it.
func (e *edited) insert(at int, r rune) {
	e.text = append(e.text[:at], append([]rune{r}, e.text[at:]...)...)
	switch {
	case at <= e.ws:
		e.ws, e.we = e.ws+1, e.we+1
	case at < e.we:
		e.we++
	}
}

// remove deletes the runes from a to b, the word shrinking by what it loses.
func (e *edited) remove(a, b int) {
	a, b = max(min(a, b), 0), min(max(a, b), len(e.text))
	lost := func(x int) int { return max(min(b, x)-a, 0) }
	e.ws, e.we = e.ws-lost(e.ws), e.we-lost(e.we)
	e.text = append(e.text[:a], e.text[b:]...)
}

// class sorts runes for word motions as vi does: a run of Han characters
// is one word, as is a run of letters and digits, or of punctuation.
func class(r rune) int {
	switch {
	case unicode.IsSpace(r):
		return 0
	case isHan(r):
		return 3
	case isWord(r):
		return 2
	}
	return 1
}

// wordFwd is where w goes from i: the start of the next word.
func wordFwd(t []rune, i int) int {
	if i >= len(t) {
		return len(t)
	}
	c := class(t[i])
	for i < len(t) && class(t[i]) == c && c != 0 {
		i++
	}
	for i < len(t) && class(t[i]) == 0 {
		i++
	}
	return i
}

// wordBack is where b goes from i: the start of this word, or of the one
// before when i is at a start already.
func wordBack(t []rune, i int) int {
	i--
	for i > 0 && class(t[i]) == 0 {
		i--
	}
	if i <= 0 {
		return 0
	}
	c := class(t[i])
	for i > 0 && class(t[i-1]) == c {
		i--
	}
	return i
}

// wordEnd is where e goes from i: the end of this word, or of the next.
func wordEnd(t []rune, i int) int {
	i++
	for i < len(t) && class(t[i]) == 0 {
		i++
	}
	if i >= len(t) {
		return max(len(t)-1, 0)
	}
	c := class(t[i])
	for i+1 < len(t) && class(t[i+1]) == c {
		i++
	}
	return i
}

// motion is where a motion key takes the cursor from i in t, and whether
// it is one.
func motion(t []rune, i int, r rune, k tcell.Key) (int, bool) {
	switch {
	case r == 'h' || k == tcell.KeyLeft:
		return max(i-1, 0), true
	case r == 'l' || k == tcell.KeyRight:
		return min(i+1, len(t)), true
	case r == '0' || k == tcell.KeyHome:
		return 0, true
	case r == '^':
		j := 0
		for j < len(t) && class(t[j]) == 0 {
			j++
		}
		return j, true
	case r == '$' || k == tcell.KeyEnd:
		return len(t), true
	case r == 'w':
		return wordFwd(t, i), true
	case r == 'b':
		return wordBack(t, i), true
	case r == 'e':
		return wordEnd(t, i) + 1, true
	}
	return i, false
}

// key applies a key to the context e, which must be a copy of its own. It
// reports whether the key was the editor's; j, k, Enter and the edge keys
// [ ] { } are left to the preview. change is called before each change, to
// save the text for u.
func (s *vi) key(ev *tcell.EventKey, e *edited, change func()) bool {
	r, k := ev.Rune(), ev.Key()
	if k != tcell.KeyRune {
		r = 0
	}
	t := e.text
	if s.insert {
		switch {
		case k == tcell.KeyEscape || k == tcell.KeyEnter:
			s.insert, s.col = false, max(s.col-1, 0)
		case k == tcell.KeyBackspace || k == tcell.KeyBackspace2:
			if s.col > 0 {
				change()
				e.remove(s.col-1, s.col)
				s.col--
			}
		case k == tcell.KeyDelete:
			if s.col < len(t) {
				change()
				e.remove(s.col, s.col+1)
			}
		case k == tcell.KeyLeft:
			s.col = max(s.col-1, 0)
		case k == tcell.KeyRight:
			s.col = min(s.col+1, len(t))
		case r != 0:
			change()
			e.insert(s.col, r)
			s.col++
		}
		return true
	}
	if p := s.pend; p != 0 {
		s.pend = 0
		switch {
		case p == 'r':
			if r != 0 && s.col < len(t) {
				change()
				e.remove(s.col, s.col+1)
				e.insert(s.col, r)
			}
		case r == p: // dd, cc: the whole line
			change()
			e.remove(0, len(t))
			s.col, s.insert = 0, p == 'c'
		default:
			to, ok := motion(t, s.col, r, k)
			if !ok {
				return true
			}
			if p == 'c' && r == 'w' {
				// cw is ce, as in vi
				to = wordEnd(t, s.col) + 1
			}
			change()
			e.remove(s.col, to)
			s.col, s.insert = min(s.col, to), p == 'c'
		}
		s.clamp(e)
		return true
	}
	if to, ok := motion(t, s.col, r, k); ok {
		s.col = to
		if r == 'e' {
			s.col--
		}
		s.clamp(e)
		return true
	}
	switch r {
	case 'i':
		s.insert = true
	case 'a':
		s.insert, s.col = true, min(s.col+1, len(t))
	case 'I':
		s.insert, s.col = true, 0
	case 'A':
		s.insert, s.col = true, len(t)
	case 'x', 's':
		if s.col < len(t) {
			change()
			e.remove(s.col, s.col+1)
		}
		s.insert = r == 's'
	case 'X':
		if s.col > 0 {
			change()
			e.remove(s.col-1, s.col)
			s.col--
		}
	case 'D', 'C':
		change()
		e.remove(s.col, len(t))
		s.insert = r == 'C'
	case 'd', 'c', 'r':
		s.pend = r
	case 'u':
		if n := len(s.undo); n > 0 {
			*e, s.undo = s.undo[n-1], s.undo[:n-1]
		}
	default:
		switch {
		case r == 'j' || r == 'k' || k == tcell.KeyUp ||
			k == tcell.KeyDown || k == tcell.KeyEnter ||
			r == '[' || r == ']' || r == '{' || r == '}':
			return false
		}
	}
	s.clamp(e)
	return true
}

// clamp keeps the cursor on a character in normal mode, and at most just
// past the last one in insert mode.
func (s *vi) clamp(e *edited) {
	end := len(e.text)
	if !s.insert {
		end--
	}
	s.col = min(max(s.col, 0), max(end, 0))
}
