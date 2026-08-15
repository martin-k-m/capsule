package config

import (
	"fmt"
	"sort"
	"strings"
)

// value is one parsed value: either a string or an array of strings. Those are
// the only two shapes capsule.toml ever needs, which is what keeps the reader
// below small enough to audit in one sitting instead of taking a dependency.
type value struct {
	str    string
	list   []string
	isList bool
}

// table is the set of key/value pairs under one [header]. The root table is "".
type table map[string]value

// parseTOML reads the subset of TOML that capsule.toml uses:
//
//   - # comments, on their own line or trailing a value
//   - [table] headers, one level deep
//   - key = "basic string" and key = 'literal string'
//   - key = ["a", "b"] arrays, which may span several lines
//   - bare tokens (true, 8080), kept as their literal text
//
// Anything outside that subset is a parse error naming the line. Reporting is
// better than silently dropping a key the author expected to take effect.
func parseTOML(src string) (map[string]table, error) {
	doc := map[string]table{"": {}}
	current := ""

	// A UTF-8 BOM is a byte-order mark, not content. Windows editors and
	// PowerShell's own `Set-Content -Encoding utf8` write one, and it lands on
	// the first key of the file, where it turns `image` into a key no reader
	// recognises and produces an error naming a word that looks correct on
	// screen. Strip it here rather than in each caller, since it can only ever
	// appear at the very start of the document.
	src = strings.TrimPrefix(src, "\uFEFF")

	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		lineNo := i + 1
		line := strings.TrimSpace(stripComment(lines[i]))
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return nil, fmt.Errorf("line %d: unterminated table header", lineNo)
			}
			name := strings.TrimSpace(line[1 : len(line)-1])
			if name == "" {
				return nil, fmt.Errorf("line %d: empty table header", lineNo)
			}
			if _, ok := doc[name]; !ok {
				doc[name] = table{}
			}
			current = name
			continue
		}

		eq := strings.Index(line, "=")
		if eq < 0 {
			return nil, fmt.Errorf("line %d: expected `key = value`, got %q", lineNo, line)
		}
		key := strings.Trim(strings.TrimSpace(line[:eq]), `"'`)
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNo)
		}
		if _, dup := doc[current][key]; dup {
			return nil, fmt.Errorf("line %d: duplicate key %q", lineNo, key)
		}

		raw := strings.TrimSpace(line[eq+1:])

		// An array may span several lines, so keep pulling lines in until the
		// brackets balance. That lets a long `packages = [...]` be formatted
		// readably instead of crammed onto one line.
		if strings.HasPrefix(raw, "[") {
			for !bracketsBalanced(raw) {
				i++
				if i >= len(lines) {
					return nil, fmt.Errorf("line %d: unterminated array", lineNo)
				}
				raw += " " + strings.TrimSpace(stripComment(lines[i]))
			}
			items, err := parseArray(raw)
			if err != nil {
				return nil, fmt.Errorf("line %d: %w", lineNo, err)
			}
			doc[current][key] = value{list: items, isList: true}
			continue
		}

		s, err := parseScalar(raw)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		doc[current][key] = value{str: s}
	}

	return doc, nil
}

// stripComment removes a trailing # comment, ignoring any # that sits inside a
// quoted string (an image tag or an env value can legitimately contain one).
func stripComment(line string) string {
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote == '"' && c == '\\':
			i++ // skip the escaped character
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#':
			return line[:i]
		}
	}
	return line
}

// bracketsBalanced reports whether every [ in s outside a string has a match.
func bracketsBalanced(s string) bool {
	depth := 0
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote == '"' && c == '\\':
			i++
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[':
			depth++
		case c == ']':
			depth--
		}
	}
	return depth == 0
}

// parseArray splits a bracketed list into its scalar elements.
func parseArray(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
		return nil, fmt.Errorf("malformed array %q", raw)
	}
	inner := strings.TrimSpace(raw[1 : len(raw)-1])
	if inner == "" {
		return []string{}, nil
	}

	items := []string{}
	var cur strings.Builder
	var quote byte

	flush := func() error {
		field := strings.TrimSpace(cur.String())
		cur.Reset()
		if field == "" {
			return nil // tolerate a trailing comma
		}
		s, err := parseScalar(field)
		if err != nil {
			return err
		}
		items = append(items, s)
		return nil
	}

	for i := 0; i < len(inner); i++ {
		c := inner[i]
		switch {
		case quote == '"' && c == '\\' && i+1 < len(inner):
			cur.WriteByte(c)
			i++
			cur.WriteByte(inner[i])
		case quote != 0:
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			cur.WriteByte(c)
		case c == ',':
			if err := flush(); err != nil {
				return nil, err
			}
		default:
			cur.WriteByte(c)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated string in array")
	}
	if err := flush(); err != nil {
		return nil, err
	}
	return items, nil
}

// parseScalar reads a single string or bare token.
func parseScalar(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("missing value")
	}
	switch raw[0] {
	case '"':
		if len(raw) < 2 || raw[len(raw)-1] != '"' {
			return "", fmt.Errorf("unterminated string %q", raw)
		}
		return unescape(raw[1 : len(raw)-1]), nil
	case '\'':
		if len(raw) < 2 || raw[len(raw)-1] != '\'' {
			return "", fmt.Errorf("unterminated string %q", raw)
		}
		return raw[1 : len(raw)-1], nil
	}
	// Bare tokens keep their literal text: every capsule.toml field ends up as a
	// string handed to the container runtime anyway.
	if strings.ContainsAny(raw, `"'`) {
		return "", fmt.Errorf("malformed value %q", raw)
	}
	return raw, nil
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case '\\':
			b.WriteByte('\\')
		case '"':
			b.WriteByte('"')
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// sortedKeys returns a map's keys in a stable order. Every place capsule turns
// a map into output (runtime flags, error messages) goes through this, so the
// same capsule.toml always produces byte-identical results.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
