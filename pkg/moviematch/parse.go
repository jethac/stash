package moviematch

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

var (
	providerTokenRe = regexp.MustCompile(`(?i)\{([^{}]+)\}`)
	tmdbTokenRe     = regexp.MustCompile(`(?i)^tmdb-(\d+)$`)
	imdbTokenRe     = regexp.MustCompile(`(?i)^imdb-(tt\d+)$`)
	editionTokenRe  = regexp.MustCompile(`(?i)^edition-(.+)$`)
	yearRe          = regexp.MustCompile(`(?:^|[\s._\-\(\[])(19\d{2}|20\d{2})(?:$|[\s._\-\)\]])`)
	splitPartRe     = regexp.MustCompile(`(?i)(?:\s+|[._-]+)(?:pt|part|cd|disc|disk|dvd)\s*\d+$`)
	resolutionRe    = regexp.MustCompile(`(?i)^(?:480p|540p|720p|1080p|2160p|4320p|4k|8k)$`)
)

func ParseHint(filePath, root string) Hint {
	cleanedPath := CleanSlashPath(filePath)
	root = CleanSlashPath(root)
	folder := path.Base(path.Dir(cleanedPath))
	fileBase := strings.TrimSuffix(path.Base(cleanedPath), path.Ext(cleanedPath))
	source := chooseSourceName(folder, fileBase, root)
	title, year, tmdbID, imdbID, edition, quality, residual := parseSourceName(source)
	return Hint{
		Path:          cleanedPath,
		SourceFolder:  folder,
		SourceName:    source,
		Title:         title,
		Year:          year,
		TMDBID:        tmdbID,
		IMDbID:        imdbID,
		Edition:       edition,
		QualityTokens: quality,
		Residual:      residual,
	}
}

func chooseSourceName(folder, fileBase, root string) string {
	rootBase := path.Base(root)
	if folder != "" && folder != "." && folder != "/" && strings.EqualFold(folder, rootBase) && providerTokenRe.MatchString(folder) {
		return folder
	}
	if folder != "" && folder != "." && folder != "/" && !strings.EqualFold(folder, rootBase) {
		if hasProviderOrYear(folder) || genericFileName(fileBase) || !hasProviderOrYear(fileBase) {
			return folder
		}
	}
	return fileBase
}

func parseSourceName(source string) (string, int, string, string, string, []string, []string) {
	source = strings.TrimSpace(source)
	var tmdbID, imdbID, edition string
	withoutTokens := providerTokenRe.ReplaceAllStringFunc(source, func(token string) string {
		value := strings.TrimSpace(strings.Trim(token, "{}"))
		switch {
		case tmdbTokenRe.MatchString(value):
			tmdbID = tmdbTokenRe.FindStringSubmatch(value)[1]
		case imdbTokenRe.MatchString(value):
			imdbID = imdbTokenRe.FindStringSubmatch(value)[1]
		case editionTokenRe.MatchString(value):
			edition = strings.TrimSpace(editionTokenRe.FindStringSubmatch(value)[1])
		}
		return " "
	})

	normalized := strings.ReplaceAll(withoutTokens, "_", " ")
	normalized = strings.ReplaceAll(normalized, ".", " ")
	normalized = splitPartRe.ReplaceAllString(normalized, "")

	var year int
	matches := yearRe.FindAllStringSubmatchIndex(normalized, -1)
	if len(matches) > 0 {
		match := matches[len(matches)-1]
		value := normalized[match[2]:match[3]]
		year, _ = strconv.Atoi(value)
		normalized = strings.TrimSpace(normalized[:match[0]] + " " + normalized[match[1]:])
	}

	words := strings.Fields(normalized)
	var titleWords, quality, residual []string
	for _, word := range words {
		clean := strings.Trim(word, "[](){}")
		lower := strings.ToLower(clean)
		switch {
		case clean == "":
		case resolutionRe.MatchString(clean), knownQualityToken(lower):
			quality = append(quality, clean)
		case looksLikeReleaseToken(clean):
			residual = append(residual, clean)
		default:
			titleWords = append(titleWords, clean)
		}
	}
	title := strings.Join(titleWords, " ")
	title = strings.Join(strings.Fields(title), " ")
	return title, year, tmdbID, imdbID, edition, quality, residual
}

func hasProviderOrYear(value string) bool {
	return providerTokenRe.MatchString(value) || yearRe.MatchString(value)
}

func genericFileName(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "" || value == "video" || value == "movie" || value == "feature" || strings.HasPrefix(value, "vts_")
}

func knownQualityToken(value string) bool {
	switch value {
	case "bluray", "blu-ray", "bdrip", "web-dl", "webdl", "webrip", "hdrip", "dvdrip", "x264", "x265", "h264", "h265", "hevc", "avc", "aac", "dts", "truehd", "atmos", "remux", "proper", "repack", "extended", "uncut", "uncensored":
		return true
	default:
		return false
	}
}

func looksLikeReleaseToken(value string) bool {
	if len(value) >= 8 && strings.ToUpper(value) == value {
		return true
	}
	return false
}

func CleanSlashPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	for strings.Contains(value, "//") {
		value = strings.ReplaceAll(value, "//", "/")
	}
	return value
}
