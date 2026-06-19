package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/stashapp/stash/pkg/moviematch"
)

type config struct {
	Endpoint      string
	APIKey        string
	Roots         multiFlag
	TMDBToken     string
	CacheDir      string
	Apply         bool
	Overwrite     bool
	IncludeAdult  bool
	MinConfidence float64
	Language      string
	Translations  multiFlag
	PerPage       int
	Timeout       time.Duration
	JSONOutput    bool
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value != "" {
		*m = append(*m, value)
	}
	return nil
}

type parsedHint = moviematch.Hint
type movieCandidate = moviematch.Candidate
type scoredCandidate = moviematch.ScoredCandidate

type movieProvider interface {
	FindCandidates(ctx context.Context, hint parsedHint) ([]movieCandidate, error)
}

type stashStore interface {
	ListScenes(ctx context.Context, roots []string, perPage int) ([]sceneRecord, error)
	FindGroupByName(ctx context.Context, name string) (*groupRecord, error)
	FindGroupByURL(ctx context.Context, targetURL string) (*groupRecord, error)
	CreateGroup(ctx context.Context, input map[string]any) (*groupRecord, error)
	UpdateGroup(ctx context.Context, id string, input map[string]any) (*groupRecord, error)
	FindLocalizedTitle(ctx context.Context, objectID, languageCode string) (*localizedTitle, error)
	UpsertLocalizedTitle(ctx context.Context, objectID string, title localizedTitleInput) error
	LinkGroupToScene(ctx context.Context, scene sceneRecord, groupID string) error
}

type sceneRecord struct {
	ID     string
	Title  string
	Files  []sceneFile
	Groups []sceneGroup
}

type sceneFile struct {
	Path     string
	Duration float64
}

type sceneGroup struct {
	Group      groupRecord
	SceneIndex *int
}

type sceneLinkGroupInput struct {
	GroupID    string `json:"group_id"`
	SceneIndex *int   `json:"scene_index,omitempty"`
}

type groupRecord struct {
	ID              string
	Name            string
	Aliases         string
	Duration        int
	Date            string
	Director        string
	Synopsis        string
	URLs            []string
	FrontImagePath  string
	BackImagePath   string
	CustomFields    map[string]any
	LocalizedTitles []localizedTitle
}

type localizedTitle struct {
	ID           string  `json:"id"`
	LanguageCode string  `json:"language_code"`
	Title        string  `json:"title"`
	Source       *string `json:"source"`
}

type localizedTitleInput struct {
	LanguageCode string `json:"language_code"`
	Title        string `json:"title"`
	Source       string `json:"source"`
}

type action string

const (
	actionWouldCreateGroup action = "would_create_group"
	actionCreateGroup      action = "create_group"
	actionWouldUpdateGroup action = "would_update_group"
	actionUpdateGroup      action = "update_group"
	actionUnchangedGroup   action = "unchanged_group"
	actionWouldLinkScene   action = "would_link_scene"
	actionLinkScene        action = "link_scene"
	actionSceneLinked      action = "scene_already_linked"
	actionWouldTitle       action = "would_upsert_title"
	actionUpsertTitle      action = "upsert_title"
	actionTitleConflict    action = "title_conflict"
	actionNoMatch          action = "no_match"
	actionError            action = "error"
)

type result struct {
	Action      action
	SceneID     string
	Path        string
	Hint        parsedHint
	Candidate   *scoredCandidate
	GroupID     string
	GroupName   string
	Planned     []string
	GroupInput  map[string]any
	TitleInput  *localizedTitleInput
	SceneGroups []sceneLinkGroupInput
	Alternates  []scoredCandidate
	Detail      string
}

type reviewReport struct {
	GeneratedAt   string   `json:"generated_at"`
	Mode          string   `json:"mode"`
	Roots         []string `json:"roots"`
	MinConfidence float64  `json:"min_confidence"`
	Items         []result `json:"items"`
	Summary       summary  `json:"summary"`
}

type summary struct {
	Total          int
	Matched        int
	CreateGroup    int
	UpdateGroup    int
	LinkScene      int
	Title          int
	UnchangedGroup int
	SceneLinked    int
	NoMatch        int
	Conflict       int
	Error          int
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

func runCLI(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseConfig(args, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}

	httpClient := &http.Client{Timeout: cfg.Timeout}
	store := &graphqlClient{endpoint: cfg.Endpoint, apiKey: cfg.APIKey, http: httpClient}
	provider := moviematch.NewTMDBClient(moviematch.TMDBOptions{
		Token:        cfg.TMDBToken,
		HTTPClient:   httpClient,
		CacheDir:     cfg.CacheDir,
		IncludeAdult: cfg.IncludeAdult,
		Language:     cfg.Language,
		Translations: cfg.Translations,
	})

	matcher := movieMatcher{
		cfg:      cfg,
		store:    store,
		provider: provider,
		now:      time.Now,
	}

	s, err := matcher.Run(context.Background(), stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if s.Error > 0 || s.Conflict > 0 || s.NoMatch > 0 {
		return 1
	}
	return 0
}

func parseConfig(args []string, stderr io.Writer) (config, error) {
	cfg := config{
		APIKey:        os.Getenv("STASH_API_KEY"),
		TMDBToken:     firstNonEmpty(os.Getenv("TMDB_BEARER_TOKEN"), os.Getenv("TMDB_API_READ_ACCESS_TOKEN")),
		MinConfidence: 0.9,
		Language:      "en-US",
		PerPage:       200,
		Timeout:       45 * time.Second,
	}
	cfg.Translations = multiFlag{"en-US", "ja-JP", "fr-FR"}

	flags := flag.NewFlagSet("movie_match", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var dryRun bool
	flags.StringVar(&cfg.Endpoint, "endpoint", "", "Stash GraphQL endpoint, for example http://nas:9999/graphql")
	flags.StringVar(&cfg.APIKey, "api-key", cfg.APIKey, "Stash API key. Defaults to STASH_API_KEY.")
	flags.Var(&cfg.Roots, "root", "Stash path root to scan. May be repeated.")
	flags.StringVar(&cfg.TMDBToken, "tmdb-token", cfg.TMDBToken, "TMDB API read access token. Defaults to TMDB_BEARER_TOKEN.")
	flags.StringVar(&cfg.CacheDir, "cache", ".cache/tmdb", "Directory for cached TMDB JSON responses. Empty disables cache.")
	flags.BoolVar(&dryRun, "dry-run", false, "Plan and report changes without writing to Stash. This is the default.")
	flags.BoolVar(&cfg.Apply, "apply", false, "Apply mutations. Without this flag, the script only reports planned changes.")
	flags.BoolVar(&cfg.Overwrite, "overwrite", false, "Overwrite existing group fields and localized titles. Default only fills missing fields and adds URLs/provenance.")
	flags.BoolVar(&cfg.IncludeAdult, "include-adult", true, "Include adult results in TMDB title searches.")
	flags.Float64Var(&cfg.MinConfidence, "min-confidence", cfg.MinConfidence, "Minimum score required to plan/apply a TMDB match.")
	flags.StringVar(&cfg.Language, "language", cfg.Language, "TMDB query language.")
	flags.Var(&cfg.Translations, "translation", "TMDB translation language to fetch for localized titles. May be repeated.")
	flags.IntVar(&cfg.PerPage, "per-page", cfg.PerPage, "Stash page size for scene discovery.")
	flags.DurationVar(&cfg.Timeout, "timeout", cfg.Timeout, "HTTP timeout.")
	flags.BoolVar(&cfg.JSONOutput, "json", false, "Write a structured JSON review report instead of a table.")

	if err := flags.Parse(args); err != nil {
		return cfg, err
	}

	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.TMDBToken = strings.TrimSpace(cfg.TMDBToken)
	cfg.CacheDir = strings.TrimSpace(cfg.CacheDir)
	cfg.Language = strings.TrimSpace(cfg.Language)
	for i := range cfg.Roots {
		cfg.Roots[i] = moviematch.CleanSlashPath(cfg.Roots[i])
	}
	if cfg.Endpoint == "" {
		return cfg, errors.New("missing required --endpoint")
	}
	if len(cfg.Roots) == 0 {
		return cfg, errors.New("missing required --root")
	}
	if cfg.TMDBToken == "" {
		return cfg, errors.New("missing TMDB token; set TMDB_BEARER_TOKEN or pass --tmdb-token")
	}
	if dryRun && cfg.Apply {
		return cfg, errors.New("--dry-run and --apply are mutually exclusive")
	}
	if cfg.MinConfidence < 0 || cfg.MinConfidence > 1 {
		return cfg, errors.New("--min-confidence must be between 0 and 1")
	}
	if cfg.PerPage <= 0 {
		return cfg, errors.New("--per-page must be positive")
	}
	return cfg, nil
}

type movieMatcher struct {
	cfg      config
	store    stashStore
	provider movieProvider
	now      func() time.Time
}

func (m movieMatcher) Run(ctx context.Context, out io.Writer) (summary, error) {
	scenes, err := m.store.ListScenes(ctx, m.cfg.Roots, m.cfg.PerPage)
	if err != nil {
		return summary{}, err
	}
	sort.Slice(scenes, func(i, j int) bool {
		return primaryPath(scenes[i]) < primaryPath(scenes[j])
	})

	var results []result
	for _, scene := range scenes {
		results = append(results, m.processScene(ctx, scene)...)
	}
	if m.cfg.JSONOutput {
		return writeJSONResults(out, m.cfg, results)
	}
	return writeResults(out, m.cfg.Apply, results), nil
}

func (m movieMatcher) processScene(ctx context.Context, scene sceneRecord) []result {
	sourcePath := primaryPath(scene)
	root := matchingRoot(sourcePath, m.cfg.Roots)
	hint := ParseMovieHint(sourcePath, root)
	hint.RuntimeSeconds = primaryDuration(scene)
	base := result{SceneID: scene.ID, Path: sourcePath, Hint: hint}
	if hint.Title == "" && hint.TMDBID == "" && hint.IMDbID == "" {
		base.Action = actionNoMatch
		base.Detail = "path did not produce a title, TMDB ID, or IMDb ID"
		return []result{base}
	}

	candidates, err := m.provider.FindCandidates(ctx, hint)
	if err != nil {
		base.Action = actionError
		base.Detail = err.Error()
		return []result{base}
	}
	scored := scoreCandidates(hint, candidates)
	if len(scored) == 0 {
		base.Action = actionNoMatch
		base.Detail = "TMDB returned no candidates"
		return []result{base}
	}

	best := scored[0]
	base.Candidate = &best
	if len(scored) > 1 {
		base.Alternates = scored[1:minInt(4, len(scored))]
	}
	if best.Score < m.cfg.MinConfidence {
		base.Action = actionNoMatch
		base.Detail = fmt.Sprintf("best score %.2f below threshold %.2f", best.Score, m.cfg.MinConfidence)
		return []result{base}
	}

	results := []result{}
	group, groupResults := m.planOrApplyGroup(ctx, scene, best, base)
	results = append(results, groupResults...)
	if group == nil {
		return results
	}

	results = append(results, m.planOrApplyTitles(ctx, *group, best, base)...)
	results = append(results, m.planOrApplySceneLink(ctx, scene, *group, base))
	return results
}

func (m movieMatcher) planOrApplyGroup(ctx context.Context, scene sceneRecord, scored scoredCandidate, base result) (*groupRecord, []result) {
	candidate := scored.Candidate
	existing := firstSceneGroup(scene)
	var err error
	if existing == nil {
		for _, candidateURL := range []string{candidate.TMDBURL(), candidate.IMDbURL()} {
			if candidateURL == "" {
				continue
			}
			existing, err = m.store.FindGroupByURL(ctx, candidateURL)
			if err != nil {
				return nil, []result{errorResult(base, "finding group by URL: "+err.Error())}
			}
			if existing != nil {
				break
			}
		}
	}
	if existing == nil && candidate.Title != "" {
		existing, err = m.store.FindGroupByName(ctx, candidate.Title)
		if err != nil {
			return nil, []result{errorResult(base, "finding group by name: "+err.Error())}
		}
	}

	input, planned := groupMutationInput(nil, candidate, scored.Score, m.now(), true)
	if existing == nil {
		r := base
		r.Candidate = &scored
		r.GroupName = candidate.Title
		r.Planned = planned
		r.GroupInput = input
		if !m.cfg.Apply {
			r.Action = actionWouldCreateGroup
			r.Detail = "missing group"
			return &groupRecord{Name: candidate.Title}, []result{r}
		}
		created, err := m.store.CreateGroup(ctx, input)
		if err != nil {
			return nil, []result{errorResult(base, "creating group: "+err.Error())}
		}
		r.Action = actionCreateGroup
		r.GroupID = created.ID
		r.GroupName = created.Name
		r.Detail = "created"
		return created, []result{r}
	}

	input, planned = groupMutationInput(existing, candidate, scored.Score, m.now(), m.cfg.Overwrite)
	r := base
	r.Candidate = &scored
	r.GroupID = existing.ID
	r.GroupName = existing.Name
	r.Planned = planned
	r.GroupInput = input
	if len(input) == 1 {
		r.Action = actionUnchangedGroup
		r.Detail = "group metadata already satisfies non-overwrite plan"
		return existing, []result{r}
	}
	if !m.cfg.Apply {
		r.Action = actionWouldUpdateGroup
		r.Detail = "planned group update"
		return existing, []result{r}
	}
	updated, err := m.store.UpdateGroup(ctx, existing.ID, input)
	if err != nil {
		return nil, []result{errorResult(base, "updating group: "+err.Error())}
	}
	r.Action = actionUpdateGroup
	r.GroupID = updated.ID
	r.GroupName = updated.Name
	r.Detail = "updated"
	return updated, []result{r}
}

func (m movieMatcher) planOrApplyTitles(ctx context.Context, group groupRecord, scored scoredCandidate, base result) []result {
	titles := localizedTitlesFromCandidate(scored.Candidate)
	results := make([]result, 0, len(titles))
	for _, title := range titles {
		r := base
		r.Candidate = &scored
		r.GroupID = group.ID
		r.GroupName = group.Name
		r.Planned = []string{fmt.Sprintf("localized_title[%s]=%q", title.LanguageCode, title.Title)}
		titleCopy := title
		r.TitleInput = &titleCopy
		if group.ID == "" {
			r.Action = actionWouldTitle
			r.Detail = "planned localized title upsert after group creation"
			results = append(results, r)
			continue
		}

		existing, err := m.store.FindLocalizedTitle(ctx, group.ID, title.LanguageCode)
		if err != nil {
			results = append(results, errorResult(r, "finding localized title: "+err.Error()))
			continue
		}
		if existing != nil && existing.Title == title.Title {
			continue
		}
		if existing != nil && !m.cfg.Overwrite {
			r.Action = actionTitleConflict
			r.Detail = fmt.Sprintf("existing localized title %q differs; pass --overwrite to replace", existing.Title)
			results = append(results, r)
			continue
		}
		if !m.cfg.Apply {
			r.Action = actionWouldTitle
			r.Detail = "planned localized title upsert"
			results = append(results, r)
			continue
		}
		if err := m.store.UpsertLocalizedTitle(ctx, group.ID, title); err != nil {
			results = append(results, errorResult(r, "upserting localized title: "+err.Error()))
			continue
		}
		r.Action = actionUpsertTitle
		r.Detail = "upserted localized title"
		results = append(results, r)
	}
	return results
}

func (m movieMatcher) planOrApplySceneLink(ctx context.Context, scene sceneRecord, group groupRecord, base result) result {
	r := base
	r.GroupID = group.ID
	r.GroupName = group.Name
	r.SceneGroups = sceneGroupInputs(scene)
	if group.ID == "" {
		r.Action = actionWouldLinkScene
		r.Detail = "planned scene group link after group creation"
		return r
	}
	if sceneHasGroup(scene, group.ID) {
		r.Action = actionSceneLinked
		r.Detail = "scene already has group"
		return r
	}
	if !m.cfg.Apply {
		r.Action = actionWouldLinkScene
		r.Detail = "planned scene group link"
		return r
	}
	if err := m.store.LinkGroupToScene(ctx, scene, group.ID); err != nil {
		return errorResult(r, "linking scene: "+err.Error())
	}
	r.Action = actionLinkScene
	r.Detail = "linked scene to group"
	return r
}

func sceneGroupInputs(scene sceneRecord) []sceneLinkGroupInput {
	ret := make([]sceneLinkGroupInput, 0, len(scene.Groups))
	for _, group := range scene.Groups {
		ret = append(ret, sceneLinkGroupInput{
			GroupID:    group.Group.ID,
			SceneIndex: group.SceneIndex,
		})
	}
	return ret
}

func groupMutationInput(existing *groupRecord, candidate movieCandidate, score float64, now time.Time, overwrite bool) (map[string]any, []string) {
	input := map[string]any{}
	if existing != nil {
		input["id"] = existing.ID
	}
	var planned []string
	addField := func(key string, value any, label string) {
		input[key] = value
		planned = append(planned, label)
	}

	if existing == nil || overwrite || strings.TrimSpace(existing.Name) == "" {
		if candidate.Title != "" {
			addField("name", candidate.Title, "name="+candidate.Title)
		}
	}
	if existing == nil || overwrite || strings.TrimSpace(existing.Date) == "" {
		if candidate.ReleaseDate != "" {
			addField("date", candidate.ReleaseDate, "date="+candidate.ReleaseDate)
		}
	}
	if existing == nil || overwrite || existing.Duration == 0 {
		if candidate.RuntimeMinutes > 0 {
			seconds := candidate.RuntimeMinutes * 60
			addField("duration", seconds, fmt.Sprintf("duration=%d", seconds))
		}
	}
	if existing == nil || overwrite || strings.TrimSpace(existing.Director) == "" {
		if candidate.Director != "" {
			addField("director", candidate.Director, "director="+candidate.Director)
		}
	}
	if existing == nil || overwrite || strings.TrimSpace(existing.Synopsis) == "" {
		if candidate.Overview != "" {
			addField("synopsis", candidate.Overview, "synopsis")
		}
	}
	urls := mergeStrings(existingURLs(existing), []string{candidate.TMDBURL(), candidate.IMDbURL()})
	if existing == nil || overwrite || !sameStringSet(existing.URLs, urls) {
		if len(urls) > 0 {
			addField("urls", urls, "urls="+strings.Join(urls, "|"))
		}
	}
	if existing == nil || overwrite || strings.TrimSpace(existing.FrontImagePath) == "" {
		if candidate.PosterURL != "" {
			addField("front_image", candidate.PosterURL, "front_image=tmdb_poster")
		}
	}
	if existing == nil || overwrite || strings.TrimSpace(existing.BackImagePath) == "" {
		if candidate.BackdropURL != "" {
			addField("back_image", candidate.BackdropURL, "back_image=tmdb_backdrop")
		}
	}

	provenance := map[string]any{
		"movie_match_source":       "tmdb",
		"movie_match_tmdb_id":      strconv.Itoa(candidate.TMDBID),
		"movie_match_confidence":   math.Round(score*1000) / 1000,
		"movie_match_applied_at":   now.UTC().Format(time.RFC3339),
		"movie_match_query_mode":   candidate.QueryMode,
		"movie_match_imdb_id":      candidate.IMDbID,
		"movie_match_release_date": candidate.ReleaseDate,
	}
	addField("custom_fields", map[string]any{"partial": provenance}, "custom_fields=movie_match_*")
	return input, planned
}

func localizedTitlesFromCandidate(candidate movieCandidate) []localizedTitleInput {
	seen := make(map[string]string)
	add := func(lang, title string) {
		lang = moviematch.LanguageCode(lang)
		title = strings.TrimSpace(title)
		if lang == "" || title == "" {
			return
		}
		if _, ok := seen[lang]; !ok {
			seen[lang] = title
		}
	}
	add("en", candidate.Title)
	add("original", candidate.OriginalTitle)
	for lang, title := range candidate.Translations {
		add(lang, title)
	}
	ret := make([]localizedTitleInput, 0, len(seen))
	for lang, title := range seen {
		ret = append(ret, localizedTitleInput{LanguageCode: lang, Title: title, Source: "tmdb"})
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].LanguageCode < ret[j].LanguageCode
	})
	return ret
}

func ParseMovieHint(filePath, root string) parsedHint {
	return moviematch.ParseHint(filePath, root)
}

func scoreCandidates(hint parsedHint, candidates []movieCandidate) []scoredCandidate {
	return moviematch.ScoreCandidates(hint, candidates)
}

type graphqlClient struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

func (c *graphqlClient) ListScenes(ctx context.Context, roots []string, perPage int) ([]sceneRecord, error) {
	var all []sceneRecord
	seen := make(map[string]struct{})
	for _, root := range roots {
		for page := 1; ; page++ {
			scenes, count, err := c.listScenesPage(ctx, root, page, perPage)
			if err != nil {
				return nil, err
			}
			for _, scene := range scenes {
				if _, ok := seen[scene.ID]; ok {
					continue
				}
				seen[scene.ID] = struct{}{}
				all = append(all, scene)
			}
			if page*perPage >= count || len(scenes) == 0 {
				break
			}
		}
	}
	return all, nil
}

func (c *graphqlClient) listScenesPage(ctx context.Context, root string, page, perPage int) ([]sceneRecord, int, error) {
	const query = `
query MovieMatchScenes($sceneFilter: SceneFilterType, $filter: FindFilterType) {
  findScenes(scene_filter: $sceneFilter, filter: $filter) {
    count
    scenes {
      id
      title
      files {
        path
        duration
      }
      groups {
        scene_index
        group {
          id
          name
          aliases
          duration
          date
          director
          synopsis
          urls
          front_image_path
          back_image_path
          custom_fields
          localized_titles {
            id
            language_code
            title
            source
          }
        }
      }
    }
  }
}`
	var out struct {
		FindScenes struct {
			Count  int `json:"count"`
			Scenes []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				Files []struct {
					Path     string  `json:"path"`
					Duration float64 `json:"duration"`
				} `json:"files"`
				Groups []struct {
					SceneIndex *int        `json:"scene_index"`
					Group      groupRecord `json:"group"`
				} `json:"groups"`
			} `json:"scenes"`
		} `json:"findScenes"`
	}
	vars := map[string]any{
		"sceneFilter": map[string]any{"path": map[string]any{"value": root, "modifier": "INCLUDES"}},
		"filter":      map[string]any{"page": page, "per_page": perPage, "sort": "path", "direction": "ASC"},
	}
	if err := c.do(ctx, query, vars, &out); err != nil {
		return nil, 0, fmt.Errorf("listing scenes under %q: %w", root, err)
	}
	ret := make([]sceneRecord, 0, len(out.FindScenes.Scenes))
	for _, scene := range out.FindScenes.Scenes {
		rec := sceneRecord{ID: scene.ID, Title: scene.Title}
		for _, file := range scene.Files {
			rec.Files = append(rec.Files, sceneFile{Path: file.Path, Duration: file.Duration})
		}
		for _, group := range scene.Groups {
			rec.Groups = append(rec.Groups, sceneGroup{Group: group.Group, SceneIndex: group.SceneIndex})
		}
		ret = append(ret, rec)
	}
	return ret, out.FindScenes.Count, nil
}

func (c *graphqlClient) FindGroupByName(ctx context.Context, name string) (*groupRecord, error) {
	const query = `
query MovieMatchFindGroupByName($filter: FindFilterType) {
  findGroups(filter: $filter) {
    groups {
      id
      name
      aliases
      duration
      date
      director
      synopsis
      urls
      front_image_path
      back_image_path
      custom_fields
      localized_titles {
        id
        language_code
        title
        source
      }
    }
  }
}`
	var out struct {
		FindGroups struct {
			Groups []groupRecord `json:"groups"`
		} `json:"findGroups"`
	}
	if err := c.do(ctx, query, map[string]any{"filter": map[string]any{"q": name, "per_page": 50}}, &out); err != nil {
		return nil, err
	}
	var exact []groupRecord
	for _, group := range out.FindGroups.Groups {
		if strings.EqualFold(group.Name, name) {
			exact = append(exact, group)
		}
	}
	if len(exact) == 0 {
		return nil, nil
	}
	if len(exact) > 1 {
		return nil, fmt.Errorf("ambiguous group name %q", name)
	}
	return &exact[0], nil
}

func (c *graphqlClient) FindGroupByURL(ctx context.Context, targetURL string) (*groupRecord, error) {
	if strings.TrimSpace(targetURL) == "" {
		return nil, nil
	}
	const query = `
query MovieMatchFindGroupByURL($groupFilter: GroupFilterType, $filter: FindFilterType) {
  findGroups(group_filter: $groupFilter, filter: $filter) {
    groups {
      id
      name
      aliases
      duration
      date
      director
      synopsis
      urls
      front_image_path
      back_image_path
      custom_fields
      localized_titles {
        id
        language_code
        title
        source
      }
    }
  }
}`
	var out struct {
		FindGroups struct {
			Groups []groupRecord `json:"groups"`
		} `json:"findGroups"`
	}
	vars := map[string]any{
		"groupFilter": map[string]any{"url": map[string]any{"value": targetURL, "modifier": "EQUALS"}},
		"filter":      map[string]any{"per_page": 2},
	}
	if err := c.do(ctx, query, vars, &out); err != nil {
		return nil, err
	}
	if len(out.FindGroups.Groups) == 0 {
		return nil, nil
	}
	if len(out.FindGroups.Groups) > 1 {
		return nil, fmt.Errorf("ambiguous group URL %q", targetURL)
	}
	return &out.FindGroups.Groups[0], nil
}

func (c *graphqlClient) CreateGroup(ctx context.Context, input map[string]any) (*groupRecord, error) {
	const mutation = `
mutation MovieMatchCreateGroup($input: GroupCreateInput!) {
  groupCreate(input: $input) {
    id
    name
    aliases
    duration
    date
    director
    synopsis
    urls
    front_image_path
    back_image_path
    custom_fields
    localized_titles {
      id
      language_code
      title
      source
    }
  }
}`
	delete(input, "id")
	var out struct {
		Group *groupRecord `json:"groupCreate"`
	}
	if err := c.do(ctx, mutation, map[string]any{"input": input}, &out); err != nil {
		return nil, err
	}
	if out.Group == nil {
		return nil, errors.New("groupCreate returned null")
	}
	return out.Group, nil
}

func (c *graphqlClient) UpdateGroup(ctx context.Context, id string, input map[string]any) (*groupRecord, error) {
	const mutation = `
mutation MovieMatchUpdateGroup($input: GroupUpdateInput!) {
  groupUpdate(input: $input) {
    id
    name
    aliases
    duration
    date
    director
    synopsis
    urls
    front_image_path
    back_image_path
    custom_fields
    localized_titles {
      id
      language_code
      title
      source
    }
  }
}`
	input["id"] = id
	var out struct {
		Group *groupRecord `json:"groupUpdate"`
	}
	if err := c.do(ctx, mutation, map[string]any{"input": input}, &out); err != nil {
		return nil, err
	}
	if out.Group == nil {
		return nil, errors.New("groupUpdate returned null")
	}
	return out.Group, nil
}

func (c *graphqlClient) FindLocalizedTitle(ctx context.Context, objectID, languageCode string) (*localizedTitle, error) {
	const query = `
query MovieMatchLocalizedTitle($objectID: ID!, $languageCode: String!) {
  findLocalizedTitleForObjectLanguage(object_type: GROUP, object_id: $objectID, language_code: $languageCode) {
    id
    language_code
    title
    source
  }
}`
	var out struct {
		Title *localizedTitle `json:"findLocalizedTitleForObjectLanguage"`
	}
	if err := c.do(ctx, query, map[string]any{"objectID": objectID, "languageCode": languageCode}, &out); err != nil {
		return nil, err
	}
	return out.Title, nil
}

func (c *graphqlClient) UpsertLocalizedTitle(ctx context.Context, objectID string, title localizedTitleInput) error {
	const mutation = `
mutation MovieMatchUpsertTitle($input: LocalizedTitleCreateInput!) {
  localizedTitleUpsert(input: $input) {
    id
  }
}`
	input := map[string]any{
		"object_type":   "GROUP",
		"object_id":     objectID,
		"language_code": title.LanguageCode,
		"title":         title.Title,
		"source":        title.Source,
	}
	var out struct {
		LocalizedTitleUpsert struct {
			ID string `json:"id"`
		} `json:"localizedTitleUpsert"`
	}
	return c.do(ctx, mutation, map[string]any{"input": input}, &out)
}

func (c *graphqlClient) LinkGroupToScene(ctx context.Context, scene sceneRecord, groupID string) error {
	const mutation = `
mutation MovieMatchLinkScene($input: SceneUpdateInput!) {
  sceneUpdate(input: $input) {
    id
  }
}`
	groups := make([]map[string]any, 0, len(scene.Groups)+1)
	for _, existing := range scene.Groups {
		item := map[string]any{"group_id": existing.Group.ID}
		if existing.SceneIndex != nil {
			item["scene_index"] = *existing.SceneIndex
		}
		groups = append(groups, item)
	}
	groups = append(groups, map[string]any{"group_id": groupID})
	input := map[string]any{"id": scene.ID, "groups": groups}
	var out struct {
		Scene *struct {
			ID string `json:"id"`
		} `json:"sceneUpdate"`
	}
	return c.do(ctx, mutation, map[string]any{"input": input}, &out)
}

func (c *graphqlClient) do(ctx context.Context, query string, variables map[string]any, out any) error {
	payload := map[string]any{"query": query, "variables": variables}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(payload); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("ApiKey", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return err
	}
	if len(decoded.Errors) > 0 {
		messages := make([]string, 0, len(decoded.Errors))
		for _, gqlErr := range decoded.Errors {
			messages = append(messages, gqlErr.Message)
		}
		return errors.New(strings.Join(messages, "; "))
	}
	return json.Unmarshal(decoded.Data, out)
}

func writeResults(out io.Writer, apply bool, results []result) summary {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ACTION\tSCENE\tPATH\tHINT\tMATCH\tSCORE\tGROUP\tPLANNED\tDETAIL")
	s := summarizeResults(results)
	for _, r := range results {
		match, score := "", ""
		if r.Candidate != nil {
			c := r.Candidate.Candidate
			match = fmt.Sprintf("tmdb:%d %s (%d)", c.TMDBID, c.Title, c.Year())
			score = fmt.Sprintf("%.2f", r.Candidate.Score)
		}
		fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.Action,
			r.SceneID,
			r.Path,
			formatHint(r.Hint),
			match,
			score,
			firstNonEmpty(joinIDName(r.GroupID, r.GroupName), "-"),
			strings.Join(r.Planned, "; "),
			r.Detail,
		)
	}
	_ = tw.Flush()
	mode := "dry-run"
	if apply {
		mode = "apply"
	}
	fmt.Fprintf(out, "\nSummary (%s): total=%d matched=%d create_group=%d update_group=%d unchanged_group=%d link_scene=%d scene_already_linked=%d title=%d no_match=%d conflict=%d error=%d\n",
		mode, s.Total, s.Matched, s.CreateGroup, s.UpdateGroup, s.UnchangedGroup, s.LinkScene, s.SceneLinked, s.Title, s.NoMatch, s.Conflict, s.Error)
	return s
}

func writeJSONResults(out io.Writer, cfg config, results []result) (summary, error) {
	s := summarizeResults(results)
	mode := "dry-run"
	if cfg.Apply {
		mode = "apply"
	}
	report := reviewReport{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Mode:          mode,
		Roots:         append([]string(nil), cfg.Roots...),
		MinConfidence: cfg.MinConfidence,
		Items:         results,
		Summary:       s,
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return s, err
	}
	return s, nil
}

func summarizeResults(results []result) summary {
	s := summary{Total: len(results)}
	for _, r := range results {
		switch r.Action {
		case actionWouldCreateGroup, actionCreateGroup:
			s.CreateGroup++
			s.Matched++
		case actionWouldUpdateGroup, actionUpdateGroup:
			s.UpdateGroup++
			s.Matched++
		case actionUnchangedGroup:
			s.UnchangedGroup++
			s.Matched++
		case actionWouldLinkScene, actionLinkScene:
			s.LinkScene++
		case actionSceneLinked:
			s.SceneLinked++
		case actionWouldTitle, actionUpsertTitle:
			s.Title++
		case actionTitleConflict:
			s.Conflict++
		case actionNoMatch:
			s.NoMatch++
		case actionError:
			s.Error++
		}
	}
	return s
}

func formatHint(h parsedHint) string {
	parts := []string{}
	if h.Title != "" {
		parts = append(parts, "title="+h.Title)
	}
	if h.Year > 0 {
		parts = append(parts, fmt.Sprintf("year=%d", h.Year))
	}
	if h.TMDBID != "" {
		parts = append(parts, "tmdb="+h.TMDBID)
	}
	if h.IMDbID != "" {
		parts = append(parts, "imdb="+h.IMDbID)
	}
	if h.Edition != "" {
		parts = append(parts, "edition="+h.Edition)
	}
	return strings.Join(parts, ",")
}

func errorResult(base result, detail string) result {
	base.Action = actionError
	base.Detail = detail
	return base
}

func primaryPath(scene sceneRecord) string {
	if len(scene.Files) == 0 {
		return ""
	}
	return moviematch.CleanSlashPath(scene.Files[0].Path)
}

func primaryDuration(scene sceneRecord) float64 {
	if len(scene.Files) == 0 {
		return 0
	}
	return scene.Files[0].Duration
}

func matchingRoot(target string, roots []string) string {
	target = moviematch.CleanSlashPath(target)
	best := ""
	for _, root := range roots {
		root = moviematch.CleanSlashPath(root)
		if strings.HasPrefix(target, root) && len(root) > len(best) {
			best = root
		}
	}
	return best
}

func firstSceneGroup(scene sceneRecord) *groupRecord {
	if len(scene.Groups) == 0 {
		return nil
	}
	group := scene.Groups[0].Group
	return &group
}

func sceneHasGroup(scene sceneRecord, groupID string) bool {
	for _, group := range scene.Groups {
		if group.Group.ID == groupID {
			return true
		}
	}
	return false
}

func existingURLs(existing *groupRecord) []string {
	if existing == nil {
		return nil
	}
	return existing.URLs
}

func mergeStrings(a, b []string) []string {
	var ret []string
	seen := make(map[string]struct{})
	for _, values := range [][]string{a, b} {
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			key := strings.ToLower(value)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			ret = append(ret, value)
		}
	}
	return ret
}

func sameStringSet(a, b []string) bool {
	a = mergeStrings(nil, a)
	b = mergeStrings(nil, b)
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func joinIDName(id, name string) string {
	if id == "" && name == "" {
		return ""
	}
	if id == "" {
		return name
	}
	if name == "" {
		return id
	}
	return id + ":" + name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
