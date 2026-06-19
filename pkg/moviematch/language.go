package moviematch

import "strings"

func NormalizeLanguages(values []string) []string {
	seen := make(map[string]struct{})
	var ret []string
	for _, value := range values {
		code := LanguageCode(value)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		ret = append(ret, code)
	}
	return ret
}

func LanguageCode(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if i := strings.IndexAny(value, "-_"); i >= 0 {
		value = value[:i]
	}
	return value
}
