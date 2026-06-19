package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseMovieHintPrefersMovieFolderAndProviderID(t *testing.T) {
	hint := ParseMovieHint("/data/Porn [FR]/Some Movie (2019) {tmdb-12345}/video.mkv", "/data/Porn [FR]")

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

func TestParseMovieHintUsesStandaloneFileAndStripsQualityTokens(t *testing.T) {
	hint := ParseMovieHint("/data/Porn [EN]/Another.Movie.2020.{imdb-tt1234567}.1080p.BluRay.x264.mkv", "/data/Porn [EN]")

	if hint.Title != "Another Movie" {
		t.Fatalf("Title = %q", hint.Title)
	}
	if hint.Year != 2020 {
		t.Fatalf("Year = %d", hint.Year)
	}
	if hint.IMDbID != "tt1234567" {
		t.Fatalf("IMDbID = %q", hint.IMDbID)
	}
	wantTokens := []string{"1080p", "BluRay", "x264"}
	for _, want := range wantTokens {
		if !containsString(hint.QualityTokens, want) {
			t.Fatalf("QualityTokens = %#v, missing %q", hint.QualityTokens, want)
		}
	}
}

func TestRootPathPatternUsesPrefixWildcard(t *testing.T) {
	got := rootPathPattern(`/data/Porn (Anime)/1. Bible Black Origins {tmdb-79641}/`)
	want := "/data/Porn (Anime)/1. Bible Black Origins {tmdb-79641}/%"
	if got != want {
		t.Fatalf("pattern = %q, want %q", got, want)
	}
}

func TestScoreCandidatePrefersExactTitleAndYear(t *testing.T) {
	hint := parsedHint{Title: "Some Movie", Year: 2019}
	candidates := []movieCandidate{
		{TMDBID: 2, Title: "Unrelated", ReleaseDate: "2019-01-01"},
		{TMDBID: 1, Title: "Some Movie", ReleaseDate: "2019-04-05", PosterURL: "https://image.example/poster.jpg"},
	}

	scored := scoreCandidates(hint, candidates)
	if scored[0].Candidate.TMDBID != 1 {
		t.Fatalf("best candidate = %#v", scored[0])
	}
	if scored[0].Score < 0.9 {
		t.Fatalf("score = %.3f", scored[0].Score)
	}
}

func TestMovieMatcherDryRunCreatesGroupAndReportsRequiredColumns(t *testing.T) {
	store := &fakeStore{
		scenes: []sceneRecord{
			{
				ID:    "scene-1",
				Title: "Some Movie",
				Files: []sceneFile{{Path: "/data/Movies/Some Movie (2019) {tmdb-12345}/video.mkv"}},
			},
		},
	}
	provider := fakeProvider{
		candidates: []movieCandidate{
			{
				TMDBID:         12345,
				IMDbID:         "tt1111111",
				Title:          "Some Movie",
				OriginalTitle:  "Some Movie Original",
				ReleaseDate:    "2019-04-05",
				RuntimeMinutes: 90,
				Overview:       "Overview",
				PosterURL:      "https://image.tmdb.org/t/p/original/poster.jpg",
				BackdropURL:    "https://image.tmdb.org/t/p/original/backdrop.jpg",
				QueryMode:      "tmdb_id",
			},
		},
	}
	matcher := movieMatcher{
		cfg:      config{Roots: multiFlag{"/data/Movies"}, MinConfidence: 0.9},
		store:    store,
		provider: provider,
		now:      fixedNow,
	}

	var out bytes.Buffer
	summary, err := matcher.Run(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CreateGroup != 1 || summary.LinkScene != 1 || summary.Title == 0 || summary.NoMatch != 0 {
		t.Fatalf("summary = %#v\n%s", summary, out.String())
	}
	output := out.String()
	for _, want := range []string{"ACTION", "HINT", "MATCH", "SCORE", "PLANNED", "would_create_group", "would_upsert_title", "would_link_scene", "tmdb:12345 Some Movie (2019)"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestMovieMatcherJSONReportIncludesApplyInputs(t *testing.T) {
	store := &fakeStore{
		scenes: []sceneRecord{
			{
				ID:    "scene-1",
				Title: "Some Movie",
				Files: []sceneFile{{Path: "/data/Movies/Some Movie (2019) {tmdb-12345}/video.mkv"}},
			},
		},
	}
	provider := fakeProvider{
		candidates: []movieCandidate{
			{
				TMDBID:         12345,
				Title:          "Some Movie",
				OriginalTitle:  "Some Movie Original",
				ReleaseDate:    "2019-04-05",
				RuntimeMinutes: 90,
				PosterURL:      "https://image.tmdb.org/t/p/original/poster.jpg",
				QueryMode:      "tmdb_id",
			},
		},
	}
	matcher := movieMatcher{
		cfg:      config{Roots: multiFlag{"/data/Movies"}, MinConfidence: 0.9, JSONOutput: true},
		store:    store,
		provider: provider,
		now:      fixedNow,
	}

	var out bytes.Buffer
	summary, err := matcher.Run(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CreateGroup != 1 || summary.Title == 0 || summary.LinkScene != 1 {
		t.Fatalf("summary = %#v\n%s", summary, out.String())
	}

	var report reviewReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid report json: %v\n%s", err, out.String())
	}
	if report.Items[0].GroupInput["name"] != "Some Movie" {
		t.Fatalf("group input = %#v", report.Items[0].GroupInput)
	}
	var foundTitle, foundLink bool
	for _, item := range report.Items {
		if item.TitleInput != nil && item.TitleInput.LanguageCode == "en" {
			foundTitle = true
		}
		if item.Action == actionWouldLinkScene && item.SceneID == "scene-1" {
			foundLink = true
		}
	}
	if !foundTitle || !foundLink {
		t.Fatalf("report items missing title/link: %#v", report.Items)
	}
}

func TestMovieMatcherApplyCreatesGroupTitlesAndSceneLink(t *testing.T) {
	store := &fakeStore{
		scenes: []sceneRecord{
			{
				ID:    "scene-1",
				Title: "Some Movie",
				Files: []sceneFile{{
					Path:     "/data/Movies/Some Movie (2019) {tmdb-12345}/video.mkv",
					Duration: 90 * 60,
				}},
			},
		},
	}
	provider := fakeProvider{
		candidates: []movieCandidate{
			{
				TMDBID:         12345,
				IMDbID:         "tt1111111",
				Title:          "Some Movie",
				OriginalTitle:  "Original Movie",
				ReleaseDate:    "2019-04-05",
				RuntimeMinutes: 90,
				Overview:       "Overview",
				PosterURL:      "https://image.tmdb.org/t/p/original/poster.jpg",
				BackdropURL:    "https://image.tmdb.org/t/p/original/backdrop.jpg",
				Director:       "Director Name",
				QueryMode:      "tmdb_id",
				Translations: map[string]string{
					"fr": "Film Exemple",
					"ja": "サンプル映画",
				},
			},
		},
	}
	matcher := movieMatcher{
		cfg:      config{Roots: multiFlag{"/data/Movies"}, Apply: true, MinConfidence: 0.9},
		store:    store,
		provider: provider,
		now:      fixedNow,
	}

	var out bytes.Buffer
	summary, err := matcher.Run(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CreateGroup != 1 || summary.Title != 4 || summary.LinkScene != 1 || summary.Error != 0 {
		t.Fatalf("summary = %#v\n%s", summary, out.String())
	}
	if len(store.createdGroups) != 1 {
		t.Fatalf("createdGroups = %#v", store.createdGroups)
	}
	created := store.createdGroups[0]
	for key, want := range map[string]any{
		"name":        "Some Movie",
		"date":        "2019-04-05",
		"duration":    5400,
		"director":    "Director Name",
		"synopsis":    "Overview",
		"front_image": "https://image.tmdb.org/t/p/original/poster.jpg",
		"back_image":  "https://image.tmdb.org/t/p/original/backdrop.jpg",
	} {
		if created[key] != want {
			t.Fatalf("created[%s] = %#v, want %#v; input=%#v", key, created[key], want, created)
		}
	}
	urls, ok := created["urls"].([]string)
	if !ok {
		t.Fatalf("urls = %#v", created["urls"])
	}
	for _, want := range []string{"https://www.themoviedb.org/movie/12345", "https://www.imdb.com/title/tt1111111/"} {
		if !containsString(urls, want) {
			t.Fatalf("urls = %#v, missing %q", urls, want)
		}
	}
	customFields, ok := created["custom_fields"].(map[string]any)
	if !ok {
		t.Fatalf("custom_fields = %#v", created["custom_fields"])
	}
	partial, ok := customFields["partial"].(map[string]any)
	if !ok {
		t.Fatalf("custom_fields partial = %#v", customFields)
	}
	for key, want := range map[string]any{
		"movie_match_source":       "tmdb",
		"movie_match_tmdb_id":      "12345",
		"movie_match_confidence":   1.0,
		"movie_match_applied_at":   "2026-06-19T00:00:00Z",
		"movie_match_query_mode":   "tmdb_id",
		"movie_match_imdb_id":      "tt1111111",
		"movie_match_release_date": "2019-04-05",
	} {
		if partial[key] != want {
			t.Fatalf("partial[%s] = %#v, want %#v; partial=%#v", key, partial[key], want, partial)
		}
	}
	for _, want := range []string{
		"created-1:en:Some Movie",
		"created-1:fr:Film Exemple",
		"created-1:ja:サンプル映画",
		"created-1:original:Original Movie",
	} {
		if !containsString(store.upsertedTitleKeys, want) {
			t.Fatalf("upsertedTitleKeys = %#v, missing %q", store.upsertedTitleKeys, want)
		}
	}
	if !containsString(store.linkedSceneKeys, "scene-1:created-1") {
		t.Fatalf("linkedSceneKeys = %#v", store.linkedSceneKeys)
	}
}

func TestParseConfigAcceptsExplicitDryRunAndRejectsApplyConflict(t *testing.T) {
	t.Setenv("TMDB_BEARER_TOKEN", "token")

	cfg, err := parseConfig([]string{
		"--endpoint", "http://stash/graphql",
		"--root", "/data/Movies",
		"--dry-run",
	}, ioDiscard{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Apply {
		t.Fatalf("Apply = true")
	}

	_, err = parseConfig([]string{
		"--endpoint", "http://stash/graphql",
		"--root", "/data/Movies",
		"--dry-run",
		"--apply",
	}, ioDiscard{})
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("err = %v", err)
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func TestMovieMatcherApplyUpdatesExistingGroupConservatively(t *testing.T) {
	store := &fakeStore{
		scenes: []sceneRecord{
			{
				ID:    "scene-1",
				Files: []sceneFile{{Path: "/data/Movies/Some Movie (2019)/video.mkv"}},
				Groups: []sceneGroup{{
					Group: groupRecord{
						ID:       "group-1",
						Name:     "Some Movie",
						Synopsis: "Manual synopsis",
					},
				}},
			},
		},
	}
	provider := fakeProvider{
		candidates: []movieCandidate{
			{
				TMDBID:      12345,
				Title:       "Some Movie",
				ReleaseDate: "2019-04-05",
				Overview:    "TMDB synopsis",
				QueryMode:   "title_search",
			},
		},
	}
	matcher := movieMatcher{
		cfg:      config{Roots: multiFlag{"/data/Movies"}, Apply: true, MinConfidence: 0.9},
		store:    store,
		provider: provider,
		now:      fixedNow,
	}

	var out bytes.Buffer
	summary, err := matcher.Run(context.Background(), &out)
	if err != nil {
		t.Fatal(err)
	}
	if summary.UpdateGroup != 1 {
		t.Fatalf("summary = %#v\n%s", summary, out.String())
	}
	if len(store.updatedGroups) != 1 {
		t.Fatalf("updatedGroups = %#v", store.updatedGroups)
	}
	update := store.updatedGroups[0]
	if _, ok := update["synopsis"]; ok {
		t.Fatalf("synopsis should not be overwritten without --overwrite: %#v", update)
	}
	if update["date"] != "2019-04-05" {
		t.Fatalf("date = %#v", update["date"])
	}
	if _, ok := update["custom_fields"]; !ok {
		t.Fatalf("missing custom_fields provenance: %#v", update)
	}
}

type fakeProvider struct {
	candidates []movieCandidate
	err        error
}

func (f fakeProvider) FindCandidates(context.Context, parsedHint) ([]movieCandidate, error) {
	return f.candidates, f.err
}

type fakeStore struct {
	scenes            []sceneRecord
	groupsByName      map[string]groupRecord
	groupsByURL       map[string]groupRecord
	titles            map[string]localizedTitle
	createdGroups     []map[string]any
	updatedGroups     []map[string]any
	upsertedTitleKeys []string
	linkedSceneKeys   []string
}

func (f *fakeStore) ListScenes(context.Context, []string, int) ([]sceneRecord, error) {
	return f.scenes, nil
}

func (f *fakeStore) FindGroupByName(_ context.Context, name string) (*groupRecord, error) {
	group, ok := f.groupsByName[strings.ToLower(name)]
	if !ok {
		return nil, nil
	}
	return &group, nil
}

func (f *fakeStore) FindGroupByURL(_ context.Context, targetURL string) (*groupRecord, error) {
	group, ok := f.groupsByURL[targetURL]
	if !ok {
		return nil, nil
	}
	return &group, nil
}

func (f *fakeStore) CreateGroup(_ context.Context, input map[string]any) (*groupRecord, error) {
	f.createdGroups = append(f.createdGroups, cloneMap(input))
	return &groupRecord{ID: "created-1", Name: input["name"].(string)}, nil
}

func (f *fakeStore) UpdateGroup(_ context.Context, id string, input map[string]any) (*groupRecord, error) {
	cloned := cloneMap(input)
	cloned["id"] = id
	f.updatedGroups = append(f.updatedGroups, cloned)
	return &groupRecord{ID: id, Name: "Some Movie"}, nil
}

func (f *fakeStore) FindLocalizedTitle(_ context.Context, objectID, languageCode string) (*localizedTitle, error) {
	if f.titles == nil {
		return nil, nil
	}
	title, ok := f.titles[objectID+":"+languageCode]
	if !ok {
		return nil, nil
	}
	return &title, nil
}

func (f *fakeStore) UpsertLocalizedTitle(_ context.Context, objectID string, title localizedTitleInput) error {
	if f.titles == nil {
		f.titles = make(map[string]localizedTitle)
	}
	f.titles[objectID+":"+title.LanguageCode] = localizedTitle{ID: "title-1", LanguageCode: title.LanguageCode, Title: title.Title, Source: &title.Source}
	f.upsertedTitleKeys = append(f.upsertedTitleKeys, objectID+":"+title.LanguageCode+":"+title.Title)
	return nil
}

func (f *fakeStore) LinkGroupToScene(_ context.Context, scene sceneRecord, groupID string) error {
	f.linkedSceneKeys = append(f.linkedSceneKeys, scene.ID+":"+groupID)
	return nil
}

func cloneMap(input map[string]any) map[string]any {
	ret := make(map[string]any, len(input))
	for k, v := range input {
		ret[k] = v
	}
	return ret
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 19, 0, 0, 0, 0, time.UTC)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
