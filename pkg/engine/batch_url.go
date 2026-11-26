package engine

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// MaxBatchURLs caps wildcard/bulk expansion to prevent accidental flooding.
const MaxBatchURLs = 500

// ExpandBatchURLs parses multi-line text (or .txt file contents) and expands
// numeric (e.g. [01-20], [1-10]) and single-letter alpha (e.g. [a-z], [A-F])
// wildcard ranges into deduplicated HTTP/HTTPS URLs.
func ExpandBatchURLs(input string) (urls []string, warnings []string, err error) {
	normalized := strings.ReplaceAll(input, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")

	seen := make(map[string]struct{})
	for lineIdx, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			warnings = append(warnings, fmt.Sprintf("line %d skipped: must start with http:// or https://", lineIdx+1))
			continue
		}

		expanded, expErr := expandLineWildcards(line, MaxBatchURLs-len(urls)+1)
		if expErr != nil {
			return nil, warnings, fmt.Errorf("line %d: %w", lineIdx+1, expErr)
		}
		for _, candidate := range expanded {
			u, parseErr := url.ParseRequestURI(candidate)
			if parseErr != nil || u.Host == "" {
				warnings = append(warnings, fmt.Sprintf("line %d skipped invalid URL %q", lineIdx+1, candidate))
				continue
			}
			if _, dup := seen[candidate]; dup {
				continue
			}
			if len(urls) >= MaxBatchURLs {
				return nil, warnings, fmt.Errorf("batch exceeds maximum limit of %d URLs", MaxBatchURLs)
			}
			seen[candidate] = struct{}{}
			urls = append(urls, candidate)
		}
	}

	return urls, warnings, nil
}

func expandLineWildcards(pattern string, limit int) ([]string, error) {
	openIdx := strings.IndexByte(pattern, '[')
	if openIdx < 0 {
		if strings.IndexByte(pattern, ']') >= 0 {
			return nil, errors.New("unmatched ']' in wildcard pattern")
		}
		return []string{pattern}, nil
	}
	closeIdx := strings.IndexByte(pattern[openIdx:], ']')
	if closeIdx < 0 {
		return nil, errors.New("unclosed '[' in wildcard pattern")
	}
	closeIdx += openIdx

	token := pattern[openIdx+1 : closeIdx]
	values, err := expandWildcardToken(token, limit)
	if err != nil {
		return nil, err
	}

	prefix := pattern[:openIdx]
	suffix := pattern[closeIdx+1:]
	out := make([]string, 0, len(values))
	for _, v := range values {
		sub, err := expandLineWildcards(prefix+v+suffix, limit-len(out))
		if err != nil {
			return nil, err
		}
		out = append(out, sub...)
		if len(out) > limit {
			return nil, fmt.Errorf("batch exceeds maximum limit of %d URLs", MaxBatchURLs)
		}
	}
	return out, nil
}

func expandWildcardToken(token string, limit int) ([]string, error) {
	parts := strings.Split(strings.TrimSpace(token), "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid wildcard [%s]: expected [start-end]", token)
	}
	startStr := strings.TrimSpace(parts[0])
	endStr := strings.TrimSpace(parts[1])
	if startStr == "" || endStr == "" {
		return nil, fmt.Errorf("invalid wildcard [%s]: empty bound", token)
	}

	// Single-letter alpha range: [a-z] or [A-Z]
	if len(startStr) == 1 && len(endStr) == 1 && isASCIIAlpha(startStr[0]) && isASCIIAlpha(endStr[0]) {
		s, e := startStr[0], endStr[0]
		sameCase := (s >= 'a' && s <= 'z' && e >= 'a' && e <= 'z') || (s >= 'A' && s <= 'Z' && e >= 'A' && e <= 'Z')
		if !sameCase || s > e {
			return nil, fmt.Errorf("invalid alpha wildcard [%s]", token)
		}
		count := int(e-s) + 1
		if count > limit {
			return nil, fmt.Errorf("batch exceeds maximum limit of %d URLs", MaxBatchURLs)
		}
		res := make([]string, 0, count)
		for ch := s; ch <= e; ch++ {
			res = append(res, string(ch))
		}
		return res, nil
	}

	// Numeric range: [01-20] or [1-100]
	startNum, err1 := strconv.Atoi(startStr)
	endNum, err2 := strconv.Atoi(endStr)
	if err1 != nil || err2 != nil || startNum < 0 || endNum < startNum {
		return nil, fmt.Errorf("invalid numeric wildcard [%s]", token)
	}
	count := endNum - startNum + 1
	if count > limit {
		return nil, fmt.Errorf("batch exceeds maximum limit of %d URLs", MaxBatchURLs)
	}
	padWidth := 0
	if len(startStr) > 1 && startStr[0] == '0' {
		padWidth = len(startStr)
	}
	res := make([]string, 0, count)
	for n := startNum; n <= endNum; n++ {
		if padWidth > 0 {
			res = append(res, fmt.Sprintf("%0*d", padWidth, n))
		} else {
			res = append(res, strconv.Itoa(n))
		}
	}
	return res, nil
}

func isASCIIAlpha(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
