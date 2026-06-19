package api

import (
	"strings"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/moviematch"
)

func TestMovieMatchPlanOptionsUsesEnvironmentToken(t *testing.T) {
	t.Setenv("TMDB_BEARER_TOKEN", " env-token ")
	cfg, err := movieMatchPlanOptions(MovieMatchPlanInput{
		Roots: []string{`C:\Media\Movies`},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.tmdbToken != "env-token" {
		t.Fatalf("tmdbToken = %q", cfg.tmdbToken)
	}
	if cfg.roots[0] != "C:/Media/Movies" {
		t.Fatalf("root = %q", cfg.roots[0])
	}
}

func TestMovieMatchPlanOptionsInputTokenOverridesEnvironment(t *testing.T) {
	t.Setenv("TMDB_BEARER_TOKEN", "env-token")
	token := "input-token"
	cfg, err := movieMatchPlanOptions(MovieMatchPlanInput{
		Roots:     []string{"/data/Movies"},
		TmdbToken: &token,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.tmdbToken != "input-token" {
		t.Fatalf("tmdbToken = %q", cfg.tmdbToken)
	}
}

func TestMovieMatchPlanOptionsRequiresToken(t *testing.T) {
	t.Setenv("TMDB_BEARER_TOKEN", "")
	t.Setenv("TMDB_API_READ_ACCESS_TOKEN", "")
	_, err := movieMatchPlanOptions(MovieMatchPlanInput{Roots: []string{"/data/Movies"}})
	if err == nil || !strings.Contains(err.Error(), "missing TMDB token") {
		t.Fatalf("err = %v", err)
	}
}

func TestMovieMatchPlanOptionsValidatesConfidence(t *testing.T) {
	t.Setenv("TMDB_BEARER_TOKEN", "token")
	confidence := 1.1
	_, err := movieMatchPlanOptions(MovieMatchPlanInput{
		Roots:         []string{"/data/Movies"},
		MinConfidence: &confidence,
	})
	if err == nil || !strings.Contains(err.Error(), "min_confidence") {
		t.Fatalf("err = %v", err)
	}
}

func TestMovieMatchGroupInputConservativeUpdateKeepsCuratedFields(t *testing.T) {
	duration := 3600
	existing := &models.Group{
		ID:       1,
		Name:     "Curated Name",
		Duration: &duration,
		Director: "Curated Director",
		Synopsis: "Curated synopsis",
		URLs:     models.NewRelatedStrings([]string{"https://example.test/manual"}),
	}
	candidate := moviematch.Candidate{
		TMDBID:         12345,
		IMDbID:         "tt1234567",
		Title:          "TMDB Title",
		ReleaseDate:    "2020-01-02",
		RuntimeMinutes: 90,
		Director:       "TMDB Director",
		Overview:       "TMDB overview",
		PosterURL:      "https://image.tmdb.org/t/p/original/poster.jpg",
		BackdropURL:    "https://image.tmdb.org/t/p/original/backdrop.jpg",
		QueryMode:      "title_search",
	}

	input, planned := movieMatchGroupInput(existing, candidate, 0.95, false, false, false, false)

	if _, ok := input["name"]; ok {
		t.Fatalf("name should not be overwritten: %#v", input)
	}
	if _, ok := input["duration"]; ok {
		t.Fatalf("duration should not be overwritten: %#v", input)
	}
	if _, ok := input["director"]; ok {
		t.Fatalf("director should not be overwritten: %#v", input)
	}
	if _, ok := input["synopsis"]; ok {
		t.Fatalf("synopsis should not be overwritten: %#v", input)
	}
	if input["date"] != "2020-01-02" {
		t.Fatalf("date = %#v", input["date"])
	}
	if input["front_image"] != candidate.PosterURL {
		t.Fatalf("front_image = %#v", input["front_image"])
	}
	if input["back_image"] != candidate.BackdropURL {
		t.Fatalf("back_image = %#v", input["back_image"])
	}
	if _, ok := input["custom_fields"]; !ok {
		t.Fatalf("missing provenance custom_fields: %#v", input)
	}
	if !containsString(planned, "custom_fields=movie_match_*") {
		t.Fatalf("planned = %#v", planned)
	}
}

func TestMovieMatchGroupInputOverwriteReplacesCuratedFields(t *testing.T) {
	duration := 3600
	existing := &models.Group{
		ID:       1,
		Name:     "Curated Name",
		Duration: &duration,
		Director: "Curated Director",
		Synopsis: "Curated synopsis",
		URLs:     models.NewRelatedStrings([]string{"https://example.test/manual"}),
	}
	candidate := moviematch.Candidate{
		TMDBID:         12345,
		Title:          "TMDB Title",
		ReleaseDate:    "2020-01-02",
		RuntimeMinutes: 90,
		Director:       "TMDB Director",
		Overview:       "TMDB overview",
		QueryMode:      "title_search",
	}

	input, _ := movieMatchGroupInput(existing, candidate, 0.95, false, true, true, true)

	if input["name"] != candidate.Title {
		t.Fatalf("name = %#v", input["name"])
	}
	if input["duration"] != candidate.RuntimeMinutes*60 {
		t.Fatalf("duration = %#v", input["duration"])
	}
	if input["director"] != candidate.Director {
		t.Fatalf("director = %#v", input["director"])
	}
	if input["synopsis"] != candidate.Overview {
		t.Fatalf("synopsis = %#v", input["synopsis"])
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
