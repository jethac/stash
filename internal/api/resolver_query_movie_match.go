package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stashapp/stash/pkg/moviematch"
)

const (
	movieMatchActionWouldCreateGroup = "would_create_group"
	movieMatchActionWouldUpdateGroup = "would_update_group"
	movieMatchActionWouldTitle       = "would_upsert_title"
	movieMatchActionWouldLinkScene   = "would_link_scene"
	movieMatchActionUnchangedGroup   = "unchanged_group"
	movieMatchActionSceneLinked      = "scene_linked"
	movieMatchActionTitleConflict    = "title_conflict"
	movieMatchActionNoMatch          = "no_match"
	movieMatchActionError            = "error"
)

type movieMatchScenePlanRecord struct {
	scene  *models.Scene
	files  []*models.VideoFile
	groups []*models.Group
}

func (r *queryResolver) MovieMatchPlan(ctx context.Context, input MovieMatchPlanInput) (*MovieMatchPlanResult, error) {
	options, err := movieMatchPlanOptions(input)
	if err != nil {
		return nil, err
	}

	var records []movieMatchScenePlanRecord
	if len(options.roots) > 0 {
		rootRecords, err := r.movieMatchPlanScenes(ctx, options.roots, options.perPage)
		if err != nil {
			return nil, err
		}
		records = append(records, rootRecords...)
	}
	if len(options.sceneIDs) > 0 {
		sceneRecords, err := r.movieMatchPlanScenesByID(ctx, options.sceneIDs)
		if err != nil {
			return nil, err
		}
		records = append(records, sceneRecords...)
	}
	records = movieMatchDeduplicateSceneRecords(records)

	provider := moviematch.NewTMDBClient(moviematch.TMDBOptions{
		Token:        options.tmdbToken,
		HTTPClient:   &http.Client{Timeout: 45 * time.Second},
		CacheDir:     options.cacheDir,
		IncludeAdult: options.includeAdult,
		Language:     options.language,
		Translations: options.translations,
	})

	var items []*MovieMatchPlanItem
	for _, record := range records {
		items = append(items, r.movieMatchPlanScene(ctx, record, provider, options)...)
	}

	return &MovieMatchPlanResult{
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Mode:          "dry-run",
		Roots:         options.roots,
		MinConfidence: options.minConfidence,
		Items:         items,
		Summary:       movieMatchSummary(items),
	}, nil
}

type movieMatchPlanConfig struct {
	roots         []string
	sceneIDs      []int
	tmdbToken     string
	cacheDir      string
	includeAdult  bool
	minConfidence float64
	language      string
	translations  []string
	perPage       int
	overwrite     bool
}

func movieMatchPlanOptions(input MovieMatchPlanInput) (movieMatchPlanConfig, error) {
	ret := movieMatchPlanConfig{
		tmdbToken:     movieMatchTMDBToken(input.TmdbToken),
		includeAdult:  true,
		minConfidence: 0.9,
		language:      "en-US",
		translations:  []string{"en-US", "ja-JP", "fr-FR"},
		perPage:       200,
	}
	if input.Roots != nil {
		ret.roots = make([]string, 0, len(input.Roots))
		for _, root := range input.Roots {
			root = moviematch.CleanSlashPath(root)
			if root != "" {
				ret.roots = append(ret.roots, root)
			}
		}
	}
	if input.SceneIds != nil {
		ret.sceneIDs = make([]int, 0, len(input.SceneIds))
		seenSceneIDs := make(map[int]struct{})
		for _, id := range input.SceneIds {
			sceneID, err := strconv.Atoi(id)
			if err != nil || sceneID <= 0 {
				return ret, fmt.Errorf("invalid scene id %q", id)
			}
			if _, ok := seenSceneIDs[sceneID]; ok {
				continue
			}
			seenSceneIDs[sceneID] = struct{}{}
			ret.sceneIDs = append(ret.sceneIDs, sceneID)
		}
	}
	if input.CacheDir != nil {
		ret.cacheDir = strings.TrimSpace(*input.CacheDir)
	}
	if input.IncludeAdult != nil {
		ret.includeAdult = *input.IncludeAdult
	}
	if input.MinConfidence != nil {
		ret.minConfidence = *input.MinConfidence
	}
	if input.Language != nil && strings.TrimSpace(*input.Language) != "" {
		ret.language = strings.TrimSpace(*input.Language)
	}
	if len(input.Translations) > 0 {
		ret.translations = input.Translations
	}
	if input.PerPage != nil && *input.PerPage > 0 {
		ret.perPage = *input.PerPage
	}
	if input.Overwrite != nil {
		ret.overwrite = *input.Overwrite
	}

	if len(ret.roots) == 0 && len(ret.sceneIDs) == 0 {
		return ret, errors.New("missing roots or scene_ids")
	}
	if ret.tmdbToken == "" {
		return ret, errors.New("missing TMDB token; set TMDB_BEARER_TOKEN on the server or pass tmdb_token")
	}
	if ret.minConfidence < 0 || ret.minConfidence > 1 {
		return ret, errors.New("min_confidence must be between 0 and 1")
	}
	return ret, nil
}

func movieMatchTMDBToken(input *string) string {
	if input != nil && strings.TrimSpace(*input) != "" {
		return strings.TrimSpace(*input)
	}
	for _, key := range []string{"TMDB_BEARER_TOKEN", "TMDB_API_READ_ACCESS_TOKEN"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}

func (r *queryResolver) movieMatchPlanScenes(ctx context.Context, roots []string, perPage int) ([]movieMatchScenePlanRecord, error) {
	var ret []movieMatchScenePlanRecord
	seen := make(map[int]struct{})

	err := r.withReadTxn(ctx, func(ctx context.Context) error {
		for _, root := range roots {
			for page := 1; ; page++ {
				filter := &models.FindFilterType{Page: &page, PerPage: &perPage}
				sceneFilter := &models.SceneFilterType{
					Path: &models.StringCriterionInput{
						Value:    movieMatchRootPathPattern(root),
						Modifier: models.CriterionModifierEquals,
					},
				}
				result, err := r.repository.Scene.Query(ctx, models.SceneQueryOptions{
					QueryOptions: models.QueryOptions{FindFilter: filter},
					SceneFilter:  sceneFilter,
				})
				if err != nil {
					return err
				}
				scenes, err := result.Resolve(ctx)
				if err != nil {
					return err
				}
				for _, scene := range scenes {
					if _, ok := seen[scene.ID]; ok {
						continue
					}
					seen[scene.ID] = struct{}{}
					files, groups, err := r.movieMatchLoadSceneRelationships(ctx, scene)
					if err != nil {
						return err
					}
					ret = append(ret, movieMatchScenePlanRecord{scene: scene, files: files, groups: groups})
				}
				if page*perPage >= result.Count || len(scenes) == 0 {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(ret, func(i, j int) bool {
		return movieMatchPrimaryPath(ret[i].files) < movieMatchPrimaryPath(ret[j].files)
	})
	return ret, nil
}

func (r *queryResolver) movieMatchPlanScenesByID(ctx context.Context, sceneIDs []int) ([]movieMatchScenePlanRecord, error) {
	var ret []movieMatchScenePlanRecord
	if len(sceneIDs) == 0 {
		return ret, nil
	}

	err := r.withReadTxn(ctx, func(ctx context.Context) error {
		for _, sceneID := range sceneIDs {
			scene, err := r.repository.Scene.Find(ctx, sceneID)
			if err != nil {
				return err
			}
			if scene == nil {
				return fmt.Errorf("scene %d not found", sceneID)
			}
			files, groups, err := r.movieMatchLoadSceneRelationships(ctx, scene)
			if err != nil {
				return err
			}
			ret = append(ret, movieMatchScenePlanRecord{scene: scene, files: files, groups: groups})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(ret, func(i, j int) bool {
		return movieMatchPrimaryPath(ret[i].files) < movieMatchPrimaryPath(ret[j].files)
	})
	return ret, nil
}

func movieMatchDeduplicateSceneRecords(records []movieMatchScenePlanRecord) []movieMatchScenePlanRecord {
	ret := make([]movieMatchScenePlanRecord, 0, len(records))
	seen := make(map[int]struct{})
	for _, record := range records {
		if record.scene == nil {
			continue
		}
		if _, ok := seen[record.scene.ID]; ok {
			continue
		}
		seen[record.scene.ID] = struct{}{}
		ret = append(ret, record)
	}
	sort.Slice(ret, func(i, j int) bool {
		return movieMatchPrimaryPath(ret[i].files) < movieMatchPrimaryPath(ret[j].files)
	})
	return ret
}

func (r *queryResolver) movieMatchLoadSceneRelationships(ctx context.Context, scene *models.Scene) ([]*models.VideoFile, []*models.Group, error) {
	if err := scene.LoadFiles(ctx, r.repository.Scene); err != nil {
		return nil, nil, err
	}
	if err := scene.LoadGroups(ctx, r.repository.Scene); err != nil {
		return nil, nil, err
	}

	files := scene.Files.List()
	var groups []*models.Group
	for _, related := range scene.Groups.List() {
		group, err := r.repository.Group.Find(ctx, related.GroupID)
		if err != nil {
			return nil, nil, err
		}
		if group == nil {
			continue
		}
		if err := movieMatchLoadGroup(ctx, r.repository, group); err != nil {
			return nil, nil, err
		}
		groups = append(groups, group)
	}
	return files, groups, nil
}

func (r *queryResolver) movieMatchPlanScene(ctx context.Context, record movieMatchScenePlanRecord, provider *moviematch.TMDBClient, cfg movieMatchPlanConfig) []*MovieMatchPlanItem {
	sourcePath := movieMatchPrimaryPath(record.files)
	root := movieMatchMatchingRoot(sourcePath, cfg.roots)
	hint := moviematch.ParseHint(sourcePath, root)
	hint.RuntimeSeconds = movieMatchPrimaryDuration(record.files)
	base := &MovieMatchPlanItem{
		SceneID: strconv.Itoa(record.scene.ID),
		Path:    sourcePath,
		Hint:    movieMatchHint(hint),
	}
	if hint.Title == "" && hint.TMDBID == "" && hint.IMDbID == "" {
		return []*MovieMatchPlanItem{movieMatchErrorLike(base, movieMatchActionNoMatch, "path did not produce a title, TMDB ID, or IMDb ID")}
	}

	candidates, err := provider.FindCandidates(ctx, hint)
	if err != nil {
		return []*MovieMatchPlanItem{movieMatchErrorLike(base, movieMatchActionError, err.Error())}
	}
	scored := moviematch.ScoreCandidates(hint, candidates)
	if len(scored) == 0 {
		return []*MovieMatchPlanItem{movieMatchErrorLike(base, movieMatchActionNoMatch, "TMDB returned no candidates")}
	}

	best := scored[0]
	base.Candidate = movieMatchScoredCandidate(best)
	if len(scored) > 1 {
		base.Alternates = movieMatchScoredCandidates(scored[1:movieMatchMin(4, len(scored))])
	}
	if best.Score < cfg.minConfidence {
		return []*MovieMatchPlanItem{movieMatchErrorLike(base, movieMatchActionNoMatch, fmt.Sprintf("best score %.2f below threshold %.2f", best.Score, cfg.minConfidence))}
	}

	group, groupItems := r.movieMatchPlanGroup(ctx, record.groups, best, base, cfg)
	items := append([]*MovieMatchPlanItem{}, groupItems...)
	if group == nil {
		return items
	}
	items = append(items, r.movieMatchPlanTitles(ctx, group, best, base, cfg)...)
	items = append(items, movieMatchPlanSceneLink(record.scene, group, base))
	return items
}

func (r *queryResolver) movieMatchPlanGroup(ctx context.Context, sceneGroups []*models.Group, scored moviematch.ScoredCandidate, base *MovieMatchPlanItem, cfg movieMatchPlanConfig) (*models.Group, []*MovieMatchPlanItem) {
	candidate := scored.Candidate
	existing := movieMatchFirstGroup(sceneGroups)
	var err error
	if existing == nil {
		for _, candidateURL := range []string{candidate.TMDBURL(), candidate.IMDbURL()} {
			if candidateURL == "" {
				continue
			}
			existing, err = r.movieMatchFindGroupByURL(ctx, candidateURL)
			if err != nil {
				return nil, []*MovieMatchPlanItem{movieMatchErrorLike(base, movieMatchActionError, "finding group by URL: "+err.Error())}
			}
			if existing != nil {
				break
			}
		}
	}
	if existing == nil && candidate.Title != "" {
		err = r.withReadTxn(ctx, func(ctx context.Context) error {
			existing, err = r.repository.Group.FindByName(ctx, candidate.Title, true)
			if existing != nil {
				err = movieMatchLoadGroup(ctx, r.repository, existing)
			}
			return err
		})
		if err != nil {
			return nil, []*MovieMatchPlanItem{movieMatchErrorLike(base, movieMatchActionError, "finding group by name: "+err.Error())}
		}
	}

	input, planned := movieMatchGroupInput(nil, candidate, scored.Score, true, true, false, false)
	if existing == nil {
		item := movieMatchCloneItem(base)
		item.Action = movieMatchActionWouldCreateGroup
		item.GroupName = movieMatchStringPtr(candidate.Title)
		item.Planned = planned
		item.GroupInput = input
		item.Detail = movieMatchStringPtr("missing group")
		return &models.Group{Name: candidate.Title}, []*MovieMatchPlanItem{item}
	}

	hasFrontImage, hasBackImage, err := r.movieMatchGroupImages(ctx, existing.ID)
	if err != nil {
		return nil, []*MovieMatchPlanItem{movieMatchErrorLike(base, movieMatchActionError, "checking group images: "+err.Error())}
	}
	input, planned = movieMatchGroupInput(existing, candidate, scored.Score, false, cfg.overwrite, hasFrontImage, hasBackImage)
	item := movieMatchCloneItem(base)
	item.GroupID = movieMatchStringPtr(strconv.Itoa(existing.ID))
	item.GroupName = movieMatchStringPtr(existing.Name)
	item.Planned = planned
	item.GroupInput = input
	if len(input) == 1 {
		item.Action = movieMatchActionUnchangedGroup
		item.Detail = movieMatchStringPtr("group metadata already satisfies non-overwrite plan")
		return existing, []*MovieMatchPlanItem{item}
	}
	item.Action = movieMatchActionWouldUpdateGroup
	item.Detail = movieMatchStringPtr("planned group update")
	return existing, []*MovieMatchPlanItem{item}
}

func (r *queryResolver) movieMatchFindGroupByURL(ctx context.Context, targetURL string) (*models.Group, error) {
	var ret *models.Group
	err := r.withReadTxn(ctx, func(ctx context.Context) error {
		filter := &models.FindFilterType{PerPage: movieMatchIntPtr(1)}
		groups, _, err := r.repository.Group.Query(ctx, &models.GroupFilterType{
			URL: &models.StringCriterionInput{
				Value:    targetURL,
				Modifier: models.CriterionModifierIncludes,
			},
		}, filter)
		if err != nil {
			return err
		}
		if len(groups) > 0 {
			ret = groups[0]
			return movieMatchLoadGroup(ctx, r.repository, ret)
		}
		return nil
	})
	return ret, err
}

func (r *queryResolver) movieMatchGroupImages(ctx context.Context, groupID int) (bool, bool, error) {
	var hasFrontImage, hasBackImage bool
	err := r.withReadTxn(ctx, func(ctx context.Context) error {
		var err error
		hasFrontImage, err = r.repository.Group.HasFrontImage(ctx, groupID)
		if err != nil {
			return err
		}
		hasBackImage, err = r.repository.Group.HasBackImage(ctx, groupID)
		return err
	})
	return hasFrontImage, hasBackImage, err
}

func (r *queryResolver) movieMatchPlanTitles(ctx context.Context, group *models.Group, scored moviematch.ScoredCandidate, base *MovieMatchPlanItem, cfg movieMatchPlanConfig) []*MovieMatchPlanItem {
	titles := movieMatchLocalizedTitles(scored.Candidate)
	items := make([]*MovieMatchPlanItem, 0, len(titles))
	for _, title := range titles {
		item := movieMatchCloneItem(base)
		if group.ID > 0 {
			item.GroupID = movieMatchStringPtr(strconv.Itoa(group.ID))
		}
		item.GroupName = movieMatchStringPtr(group.Name)
		item.Planned = []string{fmt.Sprintf("localized_title[%s]=%q", title.LanguageCode, title.Title)}
		titleCopy := title
		item.TitleInput = &titleCopy
		if group.ID == 0 {
			item.Action = movieMatchActionWouldTitle
			item.Detail = movieMatchStringPtr("planned localized title upsert after group creation")
			items = append(items, item)
			continue
		}

		var existing *models.LocalizedTitle
		err := r.withReadTxn(ctx, func(ctx context.Context) error {
			var err error
			existing, err = r.repository.LocalizedTitle.FindForObjectLanguage(ctx, models.LocalizedTitleObjectTypeGroup, group.ID, title.LanguageCode)
			return err
		})
		if err != nil {
			items = append(items, movieMatchErrorLike(item, movieMatchActionError, "finding localized title: "+err.Error()))
			continue
		}
		if existing != nil && existing.Title == title.Title {
			continue
		}
		if existing != nil && !cfg.overwrite {
			item.Action = movieMatchActionTitleConflict
			item.Detail = movieMatchStringPtr(fmt.Sprintf("existing localized title %q differs; enable overwrite to replace", existing.Title))
			items = append(items, item)
			continue
		}
		item.Action = movieMatchActionWouldTitle
		item.Detail = movieMatchStringPtr("planned localized title upsert")
		items = append(items, item)
	}
	return items
}

func movieMatchPlanSceneLink(scene *models.Scene, group *models.Group, base *MovieMatchPlanItem) *MovieMatchPlanItem {
	item := movieMatchCloneItem(base)
	if group.ID > 0 {
		item.GroupID = movieMatchStringPtr(strconv.Itoa(group.ID))
	}
	item.GroupName = movieMatchStringPtr(group.Name)
	item.SceneGroups = movieMatchSceneGroups(scene)
	if group.ID == 0 {
		item.Action = movieMatchActionWouldLinkScene
		item.Detail = movieMatchStringPtr("planned scene group link after group creation")
		return item
	}
	if scene.Groups.ForID(group.ID) != nil {
		item.Action = movieMatchActionSceneLinked
		item.Detail = movieMatchStringPtr("scene already has group")
		return item
	}
	item.Action = movieMatchActionWouldLinkScene
	item.Detail = movieMatchStringPtr("planned scene group link")
	return item
}

func movieMatchLoadGroup(ctx context.Context, repo models.Repository, group *models.Group) error {
	if err := group.LoadURLs(ctx, repo.Group); err != nil {
		return err
	}
	return nil
}

func movieMatchGroupInput(existing *models.Group, candidate moviematch.Candidate, score float64, create bool, overwrite bool, hasFrontImage bool, hasBackImage bool) (map[string]any, []string) {
	input := map[string]any{}
	var planned []string
	addField := func(key string, value any, label string) {
		input[key] = value
		planned = append(planned, label)
	}

	if create || overwrite || existing.Name == "" {
		if candidate.Title != "" {
			addField("name", candidate.Title, "name="+candidate.Title)
		}
	}
	if create || overwrite || existing.Date == nil {
		if candidate.ReleaseDate != "" {
			addField("date", candidate.ReleaseDate, "date="+candidate.ReleaseDate)
		}
	}
	if create || overwrite || existing.Duration == nil || *existing.Duration == 0 {
		if candidate.RuntimeMinutes > 0 {
			seconds := candidate.RuntimeMinutes * 60
			addField("duration", seconds, fmt.Sprintf("duration=%d", seconds))
		}
	}
	if create || overwrite || strings.TrimSpace(existing.Director) == "" {
		if candidate.Director != "" {
			addField("director", candidate.Director, "director="+candidate.Director)
		}
	}
	if create || overwrite || strings.TrimSpace(existing.Synopsis) == "" {
		if candidate.Overview != "" {
			addField("synopsis", candidate.Overview, "synopsis")
		}
	}
	var existingURLs []string
	if existing != nil && existing.URLs.Loaded() {
		existingURLs = existing.URLs.List()
	}
	urls := movieMatchMergeStrings(existingURLs, []string{candidate.TMDBURL(), candidate.IMDbURL()})
	if create || overwrite || !movieMatchSameStringSet(existingURLs, urls) {
		if len(urls) > 0 {
			addField("urls", urls, "urls="+strings.Join(urls, "|"))
		}
	}
	if create || overwrite || !hasFrontImage {
		if candidate.PosterURL != "" {
			addField("front_image", candidate.PosterURL, "front_image=tmdb_poster")
		}
	}
	if create || overwrite || !hasBackImage {
		if candidate.BackdropURL != "" {
			addField("back_image", candidate.BackdropURL, "back_image=tmdb_backdrop")
		}
	}
	addField("custom_fields", map[string]any{"partial": map[string]any{
		"movie_match_source":       "tmdb",
		"movie_match_tmdb_id":      strconv.Itoa(candidate.TMDBID),
		"movie_match_confidence":   math.Round(score*1000) / 1000,
		"movie_match_applied_at":   time.Now().UTC().Format(time.RFC3339),
		"movie_match_query_mode":   candidate.QueryMode,
		"movie_match_imdb_id":      candidate.IMDbID,
		"movie_match_release_date": candidate.ReleaseDate,
	}}, "custom_fields=movie_match_*")
	return input, planned
}

func movieMatchLocalizedTitles(candidate moviematch.Candidate) []MovieMatchLocalizedTitleInput {
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
	ret := make([]MovieMatchLocalizedTitleInput, 0, len(seen))
	source := "tmdb"
	for lang, title := range seen {
		ret = append(ret, MovieMatchLocalizedTitleInput{LanguageCode: lang, Title: title, Source: &source})
	}
	sort.Slice(ret, func(i, j int) bool {
		return ret[i].LanguageCode < ret[j].LanguageCode
	})
	return ret
}

func movieMatchPrimaryPath(files []*models.VideoFile) string {
	if len(files) == 0 || files[0] == nil {
		return ""
	}
	return moviematch.CleanSlashPath(files[0].Base().Path)
}

func movieMatchPrimaryDuration(files []*models.VideoFile) float64 {
	if len(files) == 0 || files[0] == nil {
		return 0
	}
	return files[0].Duration
}

func movieMatchMatchingRoot(target string, roots []string) string {
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

func movieMatchRootPathPattern(root string) string {
	root = strings.TrimRight(moviematch.CleanSlashPath(root), "/")
	return root + "/%"
}

func movieMatchFirstGroup(groups []*models.Group) *models.Group {
	if len(groups) == 0 {
		return nil
	}
	return groups[0]
}

func movieMatchSceneGroups(scene *models.Scene) []*MovieMatchSceneGroupInput {
	ret := make([]*MovieMatchSceneGroupInput, 0, len(scene.Groups.List()))
	for _, group := range scene.Groups.List() {
		ret = append(ret, &MovieMatchSceneGroupInput{
			GroupID:    strconv.Itoa(group.GroupID),
			SceneIndex: group.SceneIndex,
		})
	}
	return ret
}

func movieMatchHint(h moviematch.Hint) *MovieMatchHint {
	return &MovieMatchHint{
		Path:           h.Path,
		SourceFolder:   h.SourceFolder,
		SourceName:     h.SourceName,
		Title:          h.Title,
		Year:           h.Year,
		TmdbID:         h.TMDBID,
		ImdbID:         h.IMDbID,
		Edition:        h.Edition,
		QualityTokens:  h.QualityTokens,
		Residual:       h.Residual,
		RuntimeSeconds: h.RuntimeSeconds,
	}
}

func movieMatchCandidate(c moviematch.Candidate) *MovieMatchCandidate {
	translations := make(map[string]any, len(c.Translations))
	for k, v := range c.Translations {
		translations[k] = v
	}
	return &MovieMatchCandidate{
		TmdbID:              c.TMDBID,
		ImdbID:              c.IMDbID,
		Title:               c.Title,
		OriginalTitle:       c.OriginalTitle,
		ReleaseDate:         c.ReleaseDate,
		RuntimeMinutes:      c.RuntimeMinutes,
		Overview:            c.Overview,
		PosterURL:           c.PosterURL,
		BackdropURL:         c.BackdropURL,
		Director:            c.Director,
		ProductionCompanies: c.ProductionCompanies,
		Translations:        translations,
		AlternativeTitles:   c.AlternativeTitles,
		QueryMode:           c.QueryMode,
		TmdbURL:             c.TMDBURL(),
		ImdbURL:             c.IMDbURL(),
	}
}

func movieMatchScoredCandidate(scored moviematch.ScoredCandidate) *MovieMatchScoredCandidate {
	return &MovieMatchScoredCandidate{
		Candidate: movieMatchCandidate(scored.Candidate),
		Score:     scored.Score,
		Reasons:   scored.Reasons,
	}
}

func movieMatchScoredCandidates(scored []moviematch.ScoredCandidate) []*MovieMatchScoredCandidate {
	ret := make([]*MovieMatchScoredCandidate, 0, len(scored))
	for _, candidate := range scored {
		ret = append(ret, movieMatchScoredCandidate(candidate))
	}
	return ret
}

func movieMatchCloneItem(item *MovieMatchPlanItem) *MovieMatchPlanItem {
	clone := *item
	return &clone
}

func movieMatchErrorLike(base *MovieMatchPlanItem, action, detail string) *MovieMatchPlanItem {
	item := movieMatchCloneItem(base)
	item.Action = action
	item.Detail = &detail
	return item
}

func movieMatchSummary(items []*MovieMatchPlanItem) *MovieMatchSummary {
	ret := &MovieMatchSummary{Total: len(items)}
	for _, item := range items {
		switch item.Action {
		case movieMatchActionWouldCreateGroup:
			ret.Matched++
			ret.CreateGroup++
		case movieMatchActionWouldUpdateGroup:
			ret.Matched++
			ret.UpdateGroup++
		case movieMatchActionWouldLinkScene:
			ret.LinkScene++
		case movieMatchActionWouldTitle:
			ret.Title++
		case movieMatchActionUnchangedGroup:
			ret.Matched++
			ret.UnchangedGroup++
		case movieMatchActionSceneLinked:
			ret.SceneLinked++
		case movieMatchActionTitleConflict:
			ret.Conflict++
		case movieMatchActionNoMatch:
			ret.NoMatch++
		case movieMatchActionError:
			ret.Error++
		}
	}
	return ret
}

func movieMatchMergeStrings(a, b []string) []string {
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

func movieMatchSameStringSet(a, b []string) bool {
	a = movieMatchMergeStrings(nil, a)
	b = movieMatchMergeStrings(nil, b)
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

func movieMatchStringPtr(value string) *string {
	return &value
}

func movieMatchIntPtr(value int) *int {
	return &value
}

func movieMatchMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}
