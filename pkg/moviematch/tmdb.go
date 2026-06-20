package moviematch

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultTMDBBaseURL  = "https://api.themoviedb.org/3"
	defaultTMDBImageURL = "https://image.tmdb.org/t/p/original"
)

type TMDBOptions struct {
	Token        string
	HTTPClient   *http.Client
	BaseURL      string
	ImageBaseURL string
	CacheDir     string
	IncludeAdult bool
	Language     string
	Translations []string
}

type TMDBClient struct {
	token        string
	http         *http.Client
	baseURL      string
	imageBaseURL string
	cacheDir     string
	includeAdult bool
	language     string
	translations []string
}

func NewTMDBClient(options TMDBOptions) *TMDBClient {
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	baseURL := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultTMDBBaseURL
	}
	imageBaseURL := strings.TrimRight(strings.TrimSpace(options.ImageBaseURL), "/")
	if imageBaseURL == "" {
		imageBaseURL = defaultTMDBImageURL
	}
	return &TMDBClient{
		token:        strings.TrimSpace(options.Token),
		http:         httpClient,
		baseURL:      baseURL,
		imageBaseURL: imageBaseURL,
		cacheDir:     strings.TrimSpace(options.CacheDir),
		includeAdult: options.IncludeAdult,
		language:     strings.TrimSpace(options.Language),
		translations: NormalizeLanguages(options.Translations),
	}
}

func (c *TMDBClient) FindCandidates(ctx context.Context, hint Hint) ([]Candidate, error) {
	if hint.TMDBID != "" {
		id, err := strconv.Atoi(hint.TMDBID)
		if err != nil {
			return nil, fmt.Errorf("invalid TMDB ID %q", hint.TMDBID)
		}
		candidate, err := c.details(ctx, id, "tmdb_id")
		if err != nil {
			return nil, err
		}
		return []Candidate{candidate}, nil
	}
	if hint.IMDbID != "" {
		ids, err := c.findByExternalID(ctx, hint.IMDbID)
		if err != nil {
			return nil, err
		}
		return c.detailsForIDs(ctx, ids, "imdb_id")
	}
	if hint.Title == "" {
		return nil, nil
	}

	ids, err := c.searchMovie(ctx, hint.Title, hint.Year)
	if err != nil {
		return nil, err
	}
	if len(ids) > 8 {
		ids = ids[:8]
	}
	return c.detailsForIDs(ctx, ids, "title_search")
}

func (c *TMDBClient) detailsForIDs(ctx context.Context, ids []int, queryMode string) ([]Candidate, error) {
	ret := make([]Candidate, 0, len(ids))
	for _, id := range ids {
		candidate, err := c.details(ctx, id, queryMode)
		if err != nil {
			return nil, err
		}
		ret = append(ret, candidate)
	}
	return ret, nil
}

func (c *TMDBClient) searchMovie(ctx context.Context, title string, year int) ([]int, error) {
	values := url.Values{}
	values.Set("query", title)
	values.Set("include_adult", strconv.FormatBool(c.includeAdult))
	values.Set("language", firstNonEmpty(c.language, "en-US"))
	if year > 0 {
		values.Set("year", strconv.Itoa(year))
		values.Set("primary_release_year", strconv.Itoa(year))
	}

	var out struct {
		Results []struct {
			ID int `json:"id"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/movie?"+values.Encode(), &out); err != nil {
		return nil, fmt.Errorf("TMDB movie search: %w", err)
	}
	ids := make([]int, 0, len(out.Results))
	for _, result := range out.Results {
		if result.ID > 0 {
			ids = append(ids, result.ID)
		}
	}
	return ids, nil
}

func (c *TMDBClient) findByExternalID(ctx context.Context, imdbID string) ([]int, error) {
	values := url.Values{}
	values.Set("external_source", "imdb_id")
	values.Set("language", firstNonEmpty(c.language, "en-US"))
	var out struct {
		MovieResults []struct {
			ID int `json:"id"`
		} `json:"movie_results"`
	}
	if err := c.get(ctx, "/find/"+url.PathEscape(imdbID)+"?"+values.Encode(), &out); err != nil {
		return nil, fmt.Errorf("TMDB external ID lookup: %w", err)
	}
	ids := make([]int, 0, len(out.MovieResults))
	for _, result := range out.MovieResults {
		if result.ID > 0 {
			ids = append(ids, result.ID)
		}
	}
	return ids, nil
}

func (c *TMDBClient) details(ctx context.Context, id int, queryMode string) (Candidate, error) {
	values := url.Values{}
	values.Set("append_to_response", "credits,external_ids,alternative_titles,translations")
	values.Set("language", firstNonEmpty(c.language, "en-US"))

	var out tmdbMovieDetails
	if err := c.get(ctx, fmt.Sprintf("/movie/%d?%s", id, values.Encode()), &out); err != nil {
		return Candidate{}, fmt.Errorf("TMDB movie details %d: %w", id, err)
	}
	return out.toCandidate(queryMode, c.translations, c.imageBaseURL), nil
}

func (c *TMDBClient) get(ctx context.Context, endpoint string, out any) error {
	cacheKey := sha1.Sum([]byte(endpoint))
	if c.cacheDir != "" {
		cachePath := filepath.Join(c.cacheDir, hex.EncodeToString(cacheKey[:])+".json")
		if data, err := os.ReadFile(cachePath); err == nil {
			return json.Unmarshal(data, out)
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if c.cacheDir != "" {
		if err := os.MkdirAll(c.cacheDir, 0o755); err == nil {
			cachePath := filepath.Join(c.cacheDir, hex.EncodeToString(cacheKey[:])+".json")
			_ = os.WriteFile(cachePath, body, 0o644)
		}
	}
	return json.Unmarshal(body, out)
}

type tmdbMovieDetails struct {
	ID                  int    `json:"id"`
	IMDbID              string `json:"imdb_id"`
	Title               string `json:"title"`
	OriginalTitle       string `json:"original_title"`
	ReleaseDate         string `json:"release_date"`
	Runtime             int    `json:"runtime"`
	Overview            string `json:"overview"`
	PosterPath          string `json:"poster_path"`
	BackdropPath        string `json:"backdrop_path"`
	ProductionCompanies []struct {
		Name string `json:"name"`
	} `json:"production_companies"`
	Credits struct {
		Crew []struct {
			Job  string `json:"job"`
			Name string `json:"name"`
		} `json:"crew"`
	} `json:"credits"`
	ExternalIDs struct {
		IMDbID string `json:"imdb_id"`
	} `json:"external_ids"`
	AlternativeTitles struct {
		Titles []struct {
			Title string `json:"title"`
		} `json:"titles"`
	} `json:"alternative_titles"`
	Translations struct {
		Translations []struct {
			ISO6391  string `json:"iso_639_1"`
			ISO31661 string `json:"iso_3166_1"`
			Data     struct {
				Title string `json:"title"`
			} `json:"data"`
		} `json:"translations"`
	} `json:"translations"`
}

func (m tmdbMovieDetails) toCandidate(queryMode string, wantedTranslations []string, imageBaseURL string) Candidate {
	c := Candidate{
		TMDBID:         m.ID,
		IMDbID:         firstNonEmpty(m.ExternalIDs.IMDbID, m.IMDbID),
		Title:          strings.TrimSpace(m.Title),
		OriginalTitle:  strings.TrimSpace(m.OriginalTitle),
		ReleaseDate:    strings.TrimSpace(m.ReleaseDate),
		RuntimeMinutes: m.Runtime,
		Overview:       strings.TrimSpace(m.Overview),
		QueryMode:      queryMode,
		Translations:   make(map[string]string),
	}
	if m.PosterPath != "" {
		c.PosterURL = imageBaseURL + m.PosterPath
	}
	if m.BackdropPath != "" {
		c.BackdropURL = imageBaseURL + m.BackdropPath
	}
	for _, company := range m.ProductionCompanies {
		if strings.TrimSpace(company.Name) != "" {
			c.ProductionCompanies = append(c.ProductionCompanies, strings.TrimSpace(company.Name))
		}
	}
	for _, crew := range m.Credits.Crew {
		if strings.EqualFold(crew.Job, "Director") && strings.TrimSpace(crew.Name) != "" {
			c.Director = appendCSV(c.Director, strings.TrimSpace(crew.Name))
		}
	}
	for _, title := range m.AlternativeTitles.Titles {
		if strings.TrimSpace(title.Title) != "" {
			c.AlternativeTitles = append(c.AlternativeTitles, strings.TrimSpace(title.Title))
		}
	}
	wanted := make(map[string]struct{})
	for _, lang := range wantedTranslations {
		wanted[LanguageCode(lang)] = struct{}{}
	}
	for _, translation := range m.Translations.Translations {
		lang := LanguageCode(translation.ISO6391)
		if _, ok := wanted[lang]; !ok {
			continue
		}
		if strings.TrimSpace(translation.Data.Title) != "" {
			c.Translations[lang] = strings.TrimSpace(translation.Data.Title)
		}
	}
	return c
}

func appendCSV(existing, value string) string {
	if existing == "" {
		return value
	}
	return existing + ", " + value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
