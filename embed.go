// Package prompts embeds the frozen v1 prompt extractions (prompts/*.tmpl)
// and renders them for the Go engine. The .tmpl files are VERBATIM Python
// source segments (ADR-008): this package consumes them as data — evaluating
// the string-literal concatenation and substituting {placeholders} — so the
// wording lives in exactly one place, guarded by make prompts-check. The
// engine never inlines prompt text (AGENTS.md rule 2).
package prompts

import (
	"embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

//go:embed prompts/*.tmpl
var fs embed.FS

var headerRe = regexp.MustCompile(`^### ─── block ([0-9]+) · ([^:]+)::(\S+) · v1 line ([0-9]+) ───\s*$`)

// Template is one extracted prompt block, parsed into literal text and
// {placeholder} slots, in order.
type Template struct {
	parts        []part
	Placeholders []string
}

type part struct {
	text        string
	placeholder bool
}

var (
	loadOnce sync.Once
	loadErr  error
	raw      map[string]string // key "<stem>:<n>" → verbatim Python segment
	mu       sync.Mutex
	parsed   map[string]*Template // lazily evaluated (some blocks — r-string
	// regexes, plan-stage segments — aren't consumed until their stage ports)
)

func load() {
	raw = make(map[string]string)
	parsed = make(map[string]*Template)
	entries, err := fs.ReadDir("prompts")
	if err != nil {
		loadErr = err
		return
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".tmpl") {
			continue
		}
		data, err := fs.ReadFile("prompts/" + e.Name())
		if err != nil {
			loadErr = err
			return
		}
		stem := strings.TrimSuffix(e.Name(), ".tmpl")
		if err := parseFile(stem, string(data)); err != nil {
			loadErr = fmt.Errorf("%s: %w", e.Name(), err)
			return
		}
	}
}

func parseFile(stem, content string) error {
	lines := strings.Split(content, "\n")
	var cur []string
	curN := 0
	flush := func() {
		if curN > 0 {
			raw[stem+":"+strconv.Itoa(curN)] = strings.Join(cur, "\n")
		}
	}
	for _, line := range lines {
		if m := headerRe.FindStringSubmatch(line); m != nil {
			flush()
			curN, _ = strconv.Atoi(m[1])
			cur = cur[:0]
			continue
		}
		if curN > 0 {
			cur = append(cur, line)
		}
	}
	flush()
	return nil
}

// evalPySegment evaluates a Python string-literal concatenation (the exact
// source the extractor captured): a sequence of "..." / f"..." literals.
// Literal text accumulates; f-string {name} fields become placeholders;
// {{ and }} unescape to literal braces.
func evalPySegment(src string) (*Template, error) {
	tpl := &Template{}
	appendText := func(s string) {
		if n := len(tpl.parts); n > 0 && !tpl.parts[n-1].placeholder {
			tpl.parts[n-1].text += s
		} else if s != "" {
			tpl.parts = append(tpl.parts, part{text: s})
		}
	}

	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '"':
			lit, adv, err := scanPyString(src[i:])
			if err != nil {
				return nil, err
			}
			appendText(lit)
			i += adv
		case c == 'f' && i+1 < len(src) && src[i+1] == '"':
			adv, err := scanFString(src[i+1:], tpl, appendText)
			if err != nil {
				return nil, err
			}
			i += 1 + adv
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			return nil, fmt.Errorf("unexpected character %q in prompt segment", string(c))
		}
	}
	return tpl, nil
}

// scanFString scans an f"..." body (s[0]=='"'), interleaving literal text and
// {name} placeholder parts into tpl. Returns bytes consumed.
func scanFString(s string, tpl *Template, appendText func(string)) (int, error) {
	if s[0] != '"' {
		return 0, fmt.Errorf("bad f-string")
	}
	var text strings.Builder
	j := 1
	for j < len(s) {
		switch s[j] {
		case '"':
			appendText(text.String())
			return j + 1, nil
		case '\\':
			if j+1 >= len(s) {
				return 0, fmt.Errorf("dangling escape")
			}
			r, err := unescape(s[j+1])
			if err != nil {
				return 0, err
			}
			text.WriteByte(r)
			j += 2
		case '{':
			if j+1 < len(s) && s[j+1] == '{' {
				text.WriteByte('{')
				j += 2
				continue
			}
			end := strings.IndexByte(s[j:], '}')
			if end < 0 {
				return 0, fmt.Errorf("unclosed placeholder")
			}
			name := s[j+1 : j+end]
			appendText(text.String())
			text.Reset()
			tpl.parts = append(tpl.parts, part{text: name, placeholder: true})
			tpl.Placeholders = append(tpl.Placeholders, name)
			j += end + 1
		case '}':
			if j+1 < len(s) && s[j+1] == '}' {
				text.WriteByte('}')
				j += 2
				continue
			}
			return 0, fmt.Errorf("stray '}' in f-string")
		default:
			text.WriteByte(s[j])
			j++
		}
	}
	return 0, fmt.Errorf("unterminated f-string")
}

// scanPyString scans a plain "..." literal starting at s[0]=='"', returning
// the unescaped text and bytes consumed.
func scanPyString(s string) (string, int, error) {
	if s[0] != '"' {
		return "", 0, fmt.Errorf("not a string literal")
	}
	var b strings.Builder
	i := 1
	for i < len(s) {
		switch s[i] {
		case '"':
			return b.String(), i + 1, nil
		case '\\':
			if i+1 >= len(s) {
				return "", 0, fmt.Errorf("dangling escape")
			}
			r, err := unescape(s[i+1])
			if err != nil {
				return "", 0, err
			}
			b.WriteByte(r)
			i += 2
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return "", 0, fmt.Errorf("unterminated string literal")
}

func unescape(c byte) (byte, error) {
	switch c {
	case 'n':
		return '\n', nil
	case 't':
		return '\t', nil
	case '"':
		return '"', nil
	case '\\':
		return '\\', nil
	case '\'':
		return '\'', nil
	default:
		return 0, fmt.Errorf(`unsupported escape \%s in prompt segment`, string(c))
	}
}

// Block returns the parsed template for block n of prompts/<stem>.tmpl,
// evaluating the Python segment on first use.
func Block(stem string, n int) (*Template, error) {
	loadOnce.Do(load)
	if loadErr != nil {
		return nil, loadErr
	}
	key := stem + ":" + strconv.Itoa(n)
	mu.Lock()
	defer mu.Unlock()
	if tpl, ok := parsed[key]; ok {
		return tpl, nil
	}
	src, ok := raw[key]
	if !ok {
		return nil, fmt.Errorf("prompt block %s not found", key)
	}
	tpl, err := evalPySegment(src)
	if err != nil {
		return nil, fmt.Errorf("prompt block %s: %w", key, err)
	}
	parsed[key] = tpl
	return tpl, nil
}

// Render substitutes placeholder values. Every placeholder in the template
// must be present in vars — a missing one is a bug, not a default.
func (t *Template) Render(vars map[string]string) (string, error) {
	var b strings.Builder
	for _, p := range t.parts {
		if !p.placeholder {
			b.WriteString(p.text)
			continue
		}
		v, ok := vars[p.text]
		if !ok {
			return "", fmt.Errorf("prompt placeholder {%s} not supplied", p.text)
		}
		b.WriteString(v)
	}
	return b.String(), nil
}
