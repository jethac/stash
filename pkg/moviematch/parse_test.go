package moviematch

import "testing"

func TestParseHintPrefersMovieFolderAndProviderID(t *testing.T) {
	hint := ParseHint("/data/Porn [FR]/Some Movie (2019) {tmdb-12345}/video.mkv", "/data/Porn [FR]")

	if hint.SourceName != "Some Movie (2019) {tmdb-12345}" {
		t.Fatalf("SourceName = %q", hint.SourceName)
	}
	if hint.Title != "Some Movie" {
		t.Fatalf("Title = %q", hint.Title)
	}
	if hint.Year != 2019 {
		t.Fatalf("Year = %d", hint.Year)
	}
	if hint.TMDBID != "12345" {
		t.Fatalf("TMDBID = %q", hint.TMDBID)
	}
}

func TestParseHintUsesStandaloneFileAndStripsQualityTokens(t *testing.T) {
	hint := ParseHint("/data/Porn [EN]/Another.Movie.2020.{imdb-tt1234567}.1080p.BluRay.x264.mkv", "/data/Porn [EN]")

	if hint.Title != "Another Movie" {
		t.Fatalf("Title = %q", hint.Title)
	}
	if hint.Year != 2020 {
		t.Fatalf("Year = %d", hint.Year)
	}
	if hint.IMDbID != "tt1234567" {
		t.Fatalf("IMDbID = %q", hint.IMDbID)
	}
	for _, want := range []string{"1080p", "BluRay", "x264"} {
		if !containsString(hint.QualityTokens, want) {
			t.Fatalf("QualityTokens = %#v, missing %q", hint.QualityTokens, want)
		}
	}
}

func TestParseHintRepresentativePathFixtures(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		root     string
		source   string
		title    string
		year     int
		tmdbID   string
		imdbID   string
		edition  string
		quality  []string
		residual []string
	}{
		{
			name:    "plex folder with edition",
			path:    "/data/Porn [EN]/Movie Name (2021) {edition-Director's Cut}/feature.mkv",
			root:    "/data/Porn [EN]",
			source:  "Movie Name (2021) {edition-Director's Cut}",
			title:   "Movie Name",
			year:    2021,
			edition: "Director's Cut",
		},
		{
			name:   "bracket year and tmdb id",
			path:   "/data/Porn [FR]/French Title [2018] {tmdb-98765}/French Title.mkv",
			root:   "/data/Porn [FR]",
			source: "French Title [2018] {tmdb-98765}",
			title:  "French Title",
			year:   2018,
			tmdbID: "98765",
		},
		{
			name:   "split disc suffix",
			path:   "/data/Porn [JP]/Japanese Title 2017/Japanese Title - disc1.mkv",
			root:   "/data/Porn [JP]",
			source: "Japanese Title 2017",
			title:  "Japanese Title",
			year:   2017,
		},
		{
			name:     "release group residual",
			path:     "/data/Porn [EN]/Another Movie 2020 1080p BluRay x264 RELEASEGRP.mkv",
			root:     "/data/Porn [EN]",
			source:   "Another Movie 2020 1080p BluRay x264 RELEASEGRP",
			title:    "Another Movie",
			year:     2020,
			quality:  []string{"1080p", "BluRay", "x264"},
			residual: []string{"RELEASEGRP"},
		},
		{
			name:   "imdb id token",
			path:   "/data/Porn [EN]/IMDb Movie (2016) {imdb-tt0372784}/movie.mkv",
			root:   "/data/Porn [EN]",
			source: "IMDb Movie (2016) {imdb-tt0372784}",
			title:  "IMDb Movie",
			year:   2016,
			imdbID: "tt0372784",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hint := ParseHint(tt.path, tt.root)
			if hint.SourceName != tt.source {
				t.Fatalf("SourceName = %q", hint.SourceName)
			}
			if hint.Title != tt.title {
				t.Fatalf("Title = %q", hint.Title)
			}
			if hint.Year != tt.year {
				t.Fatalf("Year = %d", hint.Year)
			}
			if hint.TMDBID != tt.tmdbID {
				t.Fatalf("TMDBID = %q", hint.TMDBID)
			}
			if hint.IMDbID != tt.imdbID {
				t.Fatalf("IMDbID = %q", hint.IMDbID)
			}
			if hint.Edition != tt.edition {
				t.Fatalf("Edition = %q", hint.Edition)
			}
			for _, want := range tt.quality {
				if !containsString(hint.QualityTokens, want) {
					t.Fatalf("QualityTokens = %#v, missing %q", hint.QualityTokens, want)
				}
			}
			for _, want := range tt.residual {
				if !containsString(hint.Residual, want) {
					t.Fatalf("Residual = %#v, missing %q", hint.Residual, want)
				}
			}
		})
	}
}

func TestScoreCandidatesPrefersExactTitleAndYear(t *testing.T) {
	hint := Hint{Title: "Some Movie", Year: 2019}
	candidates := []Candidate{
		{TMDBID: 2, Title: "Unrelated", ReleaseDate: "2019-01-01"},
		{TMDBID: 1, Title: "Some Movie", ReleaseDate: "2019-04-05", PosterURL: "https://image.example/poster.jpg"},
	}

	scored := ScoreCandidates(hint, candidates)
	if scored[0].Candidate.TMDBID != 1 {
		t.Fatalf("best candidate = %#v", scored[0])
	}
	if scored[0].Score < 0.9 {
		t.Fatalf("score = %.3f", scored[0].Score)
	}
}

func TestScoreCandidatesUsesRuntimeHint(t *testing.T) {
	hint := Hint{Title: "Some Movie", Year: 2019, RuntimeSeconds: 90 * 60}
	candidates := []Candidate{
		{TMDBID: 1, Title: "Some Movie", ReleaseDate: "2019-04-05", RuntimeMinutes: 90},
		{TMDBID: 2, Title: "Some Movie", ReleaseDate: "2019-04-05", RuntimeMinutes: 150},
	}

	scored := ScoreCandidates(hint, candidates)
	if scored[0].Candidate.TMDBID != 1 {
		t.Fatalf("best candidate = %#v", scored[0])
	}
	if !containsString(scored[0].Reasons, "runtime=exact") {
		t.Fatalf("reasons = %#v", scored[0].Reasons)
	}
}

func TestScoreCandidatesDeclinesWeakTitleYearMatch(t *testing.T) {
	hint := Hint{Title: "Some Movie", Year: 2019}
	score, reasons := ScoreCandidate(hint, Candidate{
		TMDBID:      2,
		Title:       "Unrelated",
		ReleaseDate: "2024-01-01",
	})

	if score >= 0.9 {
		t.Fatalf("score = %.3f reasons = %#v", score, reasons)
	}
	if !containsString(reasons, "year=mismatch") {
		t.Fatalf("reasons = %#v", reasons)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
