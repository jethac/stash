package moviematch

import (
	"fmt"
	"strconv"
)

// Hint is the parsed identity signal from a scene file path.
type Hint struct {
	Path           string
	SourceFolder   string
	SourceName     string
	Title          string
	Year           int
	TMDBID         string
	IMDbID         string
	Edition        string
	QualityTokens  []string
	Residual       []string
	RuntimeSeconds float64
}

// Candidate is a normalized TMDB movie candidate.
type Candidate struct {
	TMDBID              int
	IMDbID              string
	Title               string
	OriginalTitle       string
	ReleaseDate         string
	RuntimeMinutes      int
	Overview            string
	PosterURL           string
	BackdropURL         string
	Director            string
	ProductionCompanies []string
	Translations        map[string]string
	AlternativeTitles   []string
	QueryMode           string
}

func (c Candidate) Year() int {
	if len(c.ReleaseDate) < 4 {
		return 0
	}
	year, _ := strconv.Atoi(c.ReleaseDate[:4])
	return year
}

func (c Candidate) TMDBURL() string {
	if c.TMDBID == 0 {
		return ""
	}
	return fmt.Sprintf("https://www.themoviedb.org/movie/%d", c.TMDBID)
}

func (c Candidate) IMDbURL() string {
	if c.IMDbID == "" {
		return ""
	}
	return "https://www.imdb.com/title/" + c.IMDbID + "/"
}

type ScoredCandidate struct {
	Candidate Candidate
	Score     float64
	Reasons   []string
}
