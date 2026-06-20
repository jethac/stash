package moviematch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestTMDBClientFindCandidatesByTMDBIDSkipsSearch(t *testing.T) {
	var searchRequests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearer(t, r)
		switch r.URL.Path {
		case "/movie/12345":
			writeMovieDetails(t, w, 12345, "Some Movie", "2019-04-05")
		case "/search/movie":
			searchRequests++
			t.Fatalf("explicit TMDB ID should not call title search")
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := NewTMDBClient(TMDBOptions{
		Token:        "test-token",
		BaseURL:      server.URL,
		ImageBaseURL: "https://images.example/original",
	})
	candidates, err := client.FindCandidates(context.Background(), Hint{TMDBID: "12345"})
	if err != nil {
		t.Fatal(err)
	}
	if searchRequests != 0 {
		t.Fatalf("searchRequests = %d", searchRequests)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	candidate := candidates[0]
	if candidate.TMDBID != 12345 || candidate.QueryMode != "tmdb_id" {
		t.Fatalf("candidate = %#v", candidate)
	}
	if candidate.PosterURL != "https://images.example/original/poster.jpg" {
		t.Fatalf("PosterURL = %q", candidate.PosterURL)
	}
}

func TestTMDBClientFindCandidatesByIMDbIDUsesExternalLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearer(t, r)
		switch r.URL.Path {
		case "/find/tt1234567":
			if r.URL.Query().Get("external_source") != "imdb_id" {
				t.Fatalf("external_source = %q", r.URL.Query().Get("external_source"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"movie_results": []map[string]any{{"id": 2468}},
			})
		case "/movie/2468":
			writeMovieDetails(t, w, 2468, "IMDb Found Movie", "2020-01-02")
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := NewTMDBClient(TMDBOptions{Token: "test-token", BaseURL: server.URL})
	candidates, err := client.FindCandidates(context.Background(), Hint{IMDbID: "tt1234567"})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].TMDBID != 2468 || candidates[0].QueryMode != "imdb_id" {
		t.Fatalf("candidate = %#v", candidates[0])
	}
}

func TestTMDBClientFindCandidatesByTitleAndYearUsesSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertBearer(t, r)
		switch r.URL.Path {
		case "/search/movie":
			if r.URL.Query().Get("query") != "Some Movie" {
				t.Fatalf("query = %q", r.URL.Query().Get("query"))
			}
			if r.URL.Query().Get("year") != "2019" {
				t.Fatalf("year = %q", r.URL.Query().Get("year"))
			}
			if r.URL.Query().Get("primary_release_year") != "2019" {
				t.Fatalf("primary_release_year = %q", r.URL.Query().Get("primary_release_year"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{{"id": 12345}},
			})
		case "/movie/12345":
			writeMovieDetails(t, w, 12345, "Some Movie", "2019-04-05")
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))
	defer server.Close()

	client := NewTMDBClient(TMDBOptions{
		Token:        "test-token",
		BaseURL:      server.URL,
		Language:     "fr-FR",
		Translations: []string{"ja-JP"},
	})
	candidates, err := client.FindCandidates(context.Background(), Hint{Title: "Some Movie", Year: 2019})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].QueryMode != "title_search" {
		t.Fatalf("QueryMode = %q", candidates[0].QueryMode)
	}
	if candidates[0].Translations["ja"] != "Japanese Title" {
		t.Fatalf("Translations = %#v", candidates[0].Translations)
	}
}

func TestTMDBClientCachesResponses(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		assertBearer(t, r)
		switch r.URL.Path {
		case "/movie/12345":
			writeMovieDetails(t, w, 12345, "Cached Movie", "2021-03-04")
		default:
			t.Fatalf("unexpected path %s", r.URL.String())
		}
	}))

	cacheDir := t.TempDir()
	client := NewTMDBClient(TMDBOptions{
		Token:    "test-token",
		BaseURL:  server.URL,
		CacheDir: cacheDir,
	})
	candidates, err := client.FindCandidates(context.Background(), Hint{TMDBID: "12345"})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Title != "Cached Movie" {
		t.Fatalf("candidates = %#v", candidates)
	}
	if requests != 1 {
		t.Fatalf("requests = %d", requests)
	}
	server.Close()

	cachedClient := NewTMDBClient(TMDBOptions{
		Token:    "test-token",
		BaseURL:  server.URL,
		CacheDir: cacheDir,
	})
	candidates, err = cachedClient.FindCandidates(context.Background(), Hint{TMDBID: "12345"})
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Title != "Cached Movie" {
		t.Fatalf("cached candidates = %#v", candidates)
	}
	if requests != 1 {
		t.Fatalf("cache miss caused requests = %d", requests)
	}
}

func assertBearer(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("Authorization = %q", got)
	}
}

func writeMovieDetails(t *testing.T, w http.ResponseWriter, id int, title, releaseDate string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id":             id,
		"imdb_id":        "tt" + strconv.Itoa(id),
		"title":          title,
		"original_title": title + " Original",
		"release_date":   releaseDate,
		"runtime":        90,
		"overview":       "Overview",
		"poster_path":    "/poster.jpg",
		"backdrop_path":  "/backdrop.jpg",
		"production_companies": []map[string]any{
			{"name": "Studio"},
		},
		"credits": map[string]any{
			"crew": []map[string]any{{"job": "Director", "name": "Director Name"}},
		},
		"external_ids": map[string]any{
			"imdb_id": "tt" + strconv.Itoa(id),
		},
		"alternative_titles": map[string]any{
			"titles": []map[string]any{{"title": title + " Alt"}},
		},
		"translations": map[string]any{
			"translations": []map[string]any{
				{
					"iso_639_1":  "ja",
					"iso_3166_1": "JP",
					"data":       map[string]any{"title": "Japanese Title"},
				},
			},
		},
	})
}
