package filter

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

type Matcher struct {
	include []*regexp.Regexp
	exclude []*regexp.Regexp
}

func New(includes, excludes []string) (*Matcher, error) {
	m := &Matcher{}
	for _, pattern := range includes {
		re, err := compile(pattern)
		if err != nil {
			return nil, err
		}
		m.include = append(m.include, re)
	}
	for _, pattern := range excludes {
		re, err := compile(pattern)
		if err != nil {
			return nil, err
		}
		m.exclude = append(m.exclude, re)
	}
	return m, nil
}

func ReadIgnore(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".winsuckignore"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read .winsuckignore: %w", err)
	}

	var patterns []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, line)
		}
	}
	return patterns, nil
}

func (m *Matcher) Include(name string) bool {
	name = strings.TrimPrefix(path.Clean(strings.ReplaceAll(name, "\\", "/")), "./")
	for _, re := range m.exclude {
		if re.MatchString(name) {
			return false
		}
	}
	if len(m.include) == 0 {
		return true
	}
	for _, re := range m.include {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

func (m *Matcher) ExcludesDirectory(name string) bool {
	name = strings.TrimSuffix(strings.TrimPrefix(path.Clean(strings.ReplaceAll(name, "\\", "/")), "./"), "/")
	for _, re := range m.exclude {
		if re.MatchString(name) || re.MatchString(name+"/") {
			return true
		}
	}
	return false
}

func compile(pattern string) (*regexp.Regexp, error) {
	pattern = strings.TrimPrefix(strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/")), "./")
	if pattern == "" {
		return nil, fmt.Errorf("empty glob pattern")
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
