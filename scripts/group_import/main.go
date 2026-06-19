package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type config struct {
	Endpoint      string
	APIKey        string
	InputPath     string
	Format        string
	DefaultSource string
	Apply         bool
	Overwrite     bool
	Timeout       time.Duration
}

type groupRecord struct {
	Line            int
	ID              string
	Name            string
	Aliases         string
	Duration        string
	Date            string
	Rating100       string
	StudioID        string
	Director        string
	Synopsis        string
	URLs            []string
	TagIDs          []string
	FrontImage      string
	BackImage       string
	Source          string
	LocalizedTitles []localizedTitleRecord
}

type localizedTitleRecord struct {
	LanguageCode string
	Title        string
	Source       string
}

type groupRef struct {
	ID   string
	Name string
}

type existingTitle struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Source *string `json:"source"`
}

type action string

const (
	actionWouldCreateGroup  action = "would_create_group"
	actionCreateGroup       action = "create_group"
	actionExistingGroup     action = "existing_group"
	actionWouldCreateTitle  action = "would_create_title"
	actionCreateTitle       action = "create_title"
	actionWouldUpdateTitle  action = "would_update_title"
	actionUpdateTitle       action = "update_title"
	actionUnchangedTitle    action = "unchanged_title"
	actionConflictTitle     action = "conflict_title"
	actionDuplicate         action = "duplicate"
	actionDuplicateConflict action = "duplicate_conflict"
	actionError             action = "error"
)

type result struct {
	Action action
	Line   int
	Object string
	Detail string
}

type summary struct {
	Total             int
	CreateGroup       int
	ExistingGroup     int
	CreateTitle       int
	UpdateTitle       int
	UnchangedTitle    int
	ConflictTitle     int
	Duplicate         int
	DuplicateConflict int
	Error             int
}

type stashClient interface {
	findGroup(ctx context.Context, id string) (*groupRef, error)
	findGroupByName(ctx context.Context, name string) (*groupRef, error)
	createGroup(ctx context.Context, rec groupRecord) (*groupRef, error)
	findLocalizedTitle(ctx context.Context, objectType, objectID, languageCode string) (*existingTitle, error)
	upsertLocalizedTitle(ctx context.Context, objectType, objectID string, title localizedTitleRecord) error
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

	records, err := loadRecords(cfg.InputPath, cfg.Format)
	if err != nil {
		fmt.Fprintf(stderr, "loading records: %v\n", err)
		return 1
	}

	client := &graphqlClient{
		endpoint: cfg.Endpoint,
		apiKey:   cfg.APIKey,
		http: &http.Client{
			Timeout: cfg.Timeout,
		},
	}

	importer := importer{
		cfg:    cfg,
		client: client,
	}

	s, err := importer.run(context.Background(), records, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if s.Error > 0 || s.ConflictTitle > 0 || s.DuplicateConflict > 0 {
		return 1
	}

	return 0
}

func parseConfig(args []string, stderr io.Writer) (config, error) {
	cfg := config{}
	flags := flag.NewFlagSet("group_import", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&cfg.Endpoint, "endpoint", "", "Stash GraphQL endpoint, for example http://nas:9999/graphql")
	flags.StringVar(&cfg.APIKey, "api-key", os.Getenv("STASH_API_KEY"), "Stash API key. Defaults to STASH_API_KEY.")
	flags.StringVar(&cfg.InputPath, "input", "", "CSV or JSON input file")
	flags.StringVar(&cfg.Format, "format", "", "Input format: csv or json. Defaults to file extension.")
	flags.StringVar(&cfg.DefaultSource, "source", "group-import", "Default localized title source for rows without a source")
	flags.BoolVar(&cfg.Apply, "apply", false, "Apply mutations. Without this flag, the script only reports planned changes.")
	flags.BoolVar(&cfg.Overwrite, "overwrite", false, "Allow replacing an existing different localized title value")
	flags.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "HTTP timeout")

	if err := flags.Parse(args); err != nil {
		return cfg, err
	}

	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.InputPath = strings.TrimSpace(cfg.InputPath)
	cfg.Format = strings.ToLower(strings.TrimSpace(cfg.Format))
	cfg.DefaultSource = strings.TrimSpace(cfg.DefaultSource)

	if cfg.Endpoint == "" {
		return cfg, errors.New("missing required --endpoint")
	}
	if cfg.InputPath == "" {
		return cfg, errors.New("missing required --input")
	}
	if cfg.Format == "" {
		cfg.Format = strings.TrimPrefix(strings.ToLower(filepath.Ext(cfg.InputPath)), ".")
	}
	if cfg.Format != "csv" && cfg.Format != "json" {
		return cfg, fmt.Errorf("unsupported --format %q; expected csv or json", cfg.Format)
	}

	return cfg, nil
}

type importer struct {
	cfg    config
	client stashClient
}

func (i importer) run(ctx context.Context, records []groupRecord, out io.Writer) (summary, error) {
	var results []result
	seen := make(map[string]groupRecord)

	for _, raw := range records {
		rec := i.withDefaults(raw)
		key := rec.identityKey()
		if key == "" {
			results = append(results, result{Action: actionError, Line: rec.Line, Object: "?", Detail: "missing id or name"})
			continue
		}
		if previous, ok := seen[key]; ok {
			action, detail := duplicateAction(previous, rec)
			results = append(results, result{Action: action, Line: rec.Line, Object: rec.displayName(), Detail: detail})
			continue
		}
		seen[key] = rec

		ref, groupResults := i.resolveOrCreateGroup(ctx, rec)
		results = append(results, groupResults...)
		if ref == nil {
			continue
		}

		for _, title := range rec.LocalizedTitles {
			results = append(results, i.planOrApplyTitle(ctx, rec, *ref, title))
		}
	}

	return writeResults(out, i.cfg.Apply, results), nil
}

func (i importer) withDefaults(rec groupRecord) groupRecord {
	rec.ID = strings.TrimSpace(rec.ID)
	rec.Name = strings.TrimSpace(rec.Name)
	rec.Aliases = strings.TrimSpace(rec.Aliases)
	rec.Duration = strings.TrimSpace(rec.Duration)
	rec.Date = strings.TrimSpace(rec.Date)
	rec.Rating100 = strings.TrimSpace(rec.Rating100)
	rec.StudioID = strings.TrimSpace(rec.StudioID)
	rec.Director = strings.TrimSpace(rec.Director)
	rec.Synopsis = strings.TrimSpace(rec.Synopsis)
	rec.FrontImage = strings.TrimSpace(rec.FrontImage)
	rec.BackImage = strings.TrimSpace(rec.BackImage)
	rec.Source = firstNonEmpty(rec.Source, i.cfg.DefaultSource)
	rec.URLs = trimList(rec.URLs)
	rec.TagIDs = trimList(rec.TagIDs)

	titles := make([]localizedTitleRecord, 0, len(rec.LocalizedTitles))
	for _, title := range rec.LocalizedTitles {
		title.LanguageCode = strings.ToLower(strings.TrimSpace(title.LanguageCode))
		title.Title = strings.TrimSpace(title.Title)
		title.Source = firstNonEmpty(title.Source, rec.Source, i.cfg.DefaultSource)
		if title.LanguageCode == "" || title.Title == "" {
			continue
		}
		titles = append(titles, title)
	}
	rec.LocalizedTitles = titles
	return rec
}

func (i importer) resolveOrCreateGroup(ctx context.Context, rec groupRecord) (*groupRef, []result) {
	if rec.ID != "" {
		ref, err := i.client.findGroup(ctx, rec.ID)
		if err != nil {
			return nil, []result{{Action: actionError, Line: rec.Line, Object: rec.displayName(), Detail: err.Error()}}
		}
		if ref == nil {
			return nil, []result{{Action: actionError, Line: rec.Line, Object: rec.displayName(), Detail: "group id not found"}}
		}
		return ref, []result{{Action: actionExistingGroup, Line: rec.Line, Object: ref.ID + ":" + ref.Name, Detail: "matched by id"}}
	}

	ref, err := i.client.findGroupByName(ctx, rec.Name)
	if err != nil {
		return nil, []result{{Action: actionError, Line: rec.Line, Object: rec.displayName(), Detail: err.Error()}}
	}
	if ref != nil {
		return ref, []result{{Action: actionExistingGroup, Line: rec.Line, Object: ref.ID + ":" + ref.Name, Detail: "matched by exact name"}}
	}

	if !i.cfg.Apply {
		return &groupRef{Name: rec.Name}, []result{{Action: actionWouldCreateGroup, Line: rec.Line, Object: rec.displayName(), Detail: "missing group"}}
	}

	created, err := i.client.createGroup(ctx, rec)
	if err != nil {
		return nil, []result{{Action: actionError, Line: rec.Line, Object: rec.displayName(), Detail: err.Error()}}
	}
	return created, []result{{Action: actionCreateGroup, Line: rec.Line, Object: created.ID + ":" + created.Name, Detail: "created"}}
}

func (i importer) planOrApplyTitle(ctx context.Context, rec groupRecord, ref groupRef, title localizedTitleRecord) result {
	if ref.ID == "" {
		return result{
			Action: actionWouldCreateTitle,
			Line:   rec.Line,
			Object: rec.Name + " " + title.LanguageCode,
			Detail: "would apply after group creation",
		}
	}

	existing, err := i.client.findLocalizedTitle(ctx, "GROUP", ref.ID, title.LanguageCode)
	if err != nil {
		return result{Action: actionError, Line: rec.Line, Object: ref.ID + ":" + title.LanguageCode, Detail: err.Error()}
	}

	planned, detail := planTitleAction(title, existing, i.cfg.Overwrite)
	if !i.cfg.Apply || planned == actionUnchangedTitle || planned == actionConflictTitle {
		return result{Action: planned, Line: rec.Line, Object: ref.ID + ":" + title.LanguageCode, Detail: detail}
	}

	if err := i.client.upsertLocalizedTitle(ctx, "GROUP", ref.ID, title); err != nil {
		return result{Action: actionError, Line: rec.Line, Object: ref.ID + ":" + title.LanguageCode, Detail: err.Error()}
	}

	if planned == actionWouldCreateTitle {
		return result{Action: actionCreateTitle, Line: rec.Line, Object: ref.ID + ":" + title.LanguageCode, Detail: "applied"}
	}
	return result{Action: actionUpdateTitle, Line: rec.Line, Object: ref.ID + ":" + title.LanguageCode, Detail: "applied"}
}

func planTitleAction(title localizedTitleRecord, existing *existingTitle, overwrite bool) (action, string) {
	if existing == nil {
		return actionWouldCreateTitle, "missing localized title"
	}
	if existing.Title == title.Title {
		return actionUnchangedTitle, "already matches"
	}
	if !overwrite {
		return actionConflictTitle, fmt.Sprintf("existing title=%q source=%q; pass --overwrite to replace", existing.Title, stringPtrValue(existing.Source))
	}
	return actionWouldUpdateTitle, fmt.Sprintf("replacing existing title=%q source=%q", existing.Title, stringPtrValue(existing.Source))
}

func duplicateAction(previous, current groupRecord) (action, string) {
	if recordsEquivalent(previous, current) {
		return actionDuplicate, fmt.Sprintf("same group input already appeared on line %d", previous.Line)
	}
	return actionDuplicateConflict, fmt.Sprintf("same group identity appeared on line %d with different values", previous.Line)
}

func recordsEquivalent(a, b groupRecord) bool {
	a.Line = 0
	b.Line = 0
	return fmt.Sprintf("%#v", a) == fmt.Sprintf("%#v", b)
}

func writeResults(out io.Writer, apply bool, results []result) summary {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ACTION\tLINE\tOBJECT\tDETAIL")

	s := summary{Total: len(results)}
	for _, r := range results {
		switch r.Action {
		case actionCreateGroup, actionWouldCreateGroup:
			s.CreateGroup++
		case actionExistingGroup:
			s.ExistingGroup++
		case actionCreateTitle, actionWouldCreateTitle:
			s.CreateTitle++
		case actionUpdateTitle, actionWouldUpdateTitle:
			s.UpdateTitle++
		case actionUnchangedTitle:
			s.UnchangedTitle++
		case actionConflictTitle:
			s.ConflictTitle++
		case actionDuplicate:
			s.Duplicate++
		case actionDuplicateConflict:
			s.DuplicateConflict++
		case actionError:
			s.Error++
		}

		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", r.Action, r.Line, firstNonEmpty(r.Object, "?"), r.Detail)
	}

	_ = tw.Flush()
	mode := "dry-run"
	if apply {
		mode = "apply"
	}
	fmt.Fprintf(
		out,
		"\nSummary (%s): total=%d create_group=%d existing_group=%d create_title=%d update_title=%d unchanged_title=%d conflict_title=%d duplicate=%d duplicate_conflict=%d error=%d\n",
		mode,
		s.Total,
		s.CreateGroup,
		s.ExistingGroup,
		s.CreateTitle,
		s.UpdateTitle,
		s.UnchangedTitle,
		s.ConflictTitle,
		s.Duplicate,
		s.DuplicateConflict,
		s.Error,
	)

	return s
}

func loadRecords(path, format string) ([]groupRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	switch format {
	case "csv":
		return loadCSVRecords(f)
	case "json":
		return loadJSONRecords(f)
	default:
		return nil, fmt.Errorf("unsupported format %q", format)
	}
}

func loadCSVRecords(r io.Reader) ([]groupRecord, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	headers, err := reader.Read()
	if err != nil {
		return nil, err
	}

	index := make(map[int]string)
	for idx, header := range headers {
		index[idx] = normalizeHeader(header)
	}

	var records []groupRecord
	line := 1
	for {
		line++
		row, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", line, err)
		}

		values := make(map[string]string)
		for idx, value := range row {
			if header := index[idx]; header != "" {
				values[header] = strings.TrimSpace(value)
			}
		}

		rec := recordFromValues(values)
		rec.Line = line
		if rec.isEmpty() {
			continue
		}
		records = append(records, rec)
	}

	return records, nil
}

func loadJSONRecords(r io.Reader) ([]groupRecord, error) {
	var rows []map[string]any
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return nil, err
	}

	records := make([]groupRecord, 0, len(rows))
	for idx, row := range rows {
		values := make(map[string]string)
		for k, v := range row {
			if v == nil {
				continue
			}
			values[normalizeHeader(k)] = strings.TrimSpace(fmt.Sprint(v))
		}

		rec := recordFromValues(values)
		rec.Line = idx + 1
		if rec.isEmpty() {
			continue
		}
		records = append(records, rec)
	}

	return records, nil
}

func recordFromValues(values map[string]string) groupRecord {
	rec := groupRecord{
		ID:         firstValue(values, "id", "group_id", "movie_id"),
		Name:       firstValue(values, "name", "group_name", "movie_name", "title"),
		Aliases:    firstValue(values, "aliases", "alias"),
		Duration:   firstValue(values, "duration", "duration_seconds"),
		Date:       firstValue(values, "date", "release_date", "year"),
		Rating100:  firstValue(values, "rating100", "rating_100", "rating"),
		StudioID:   firstValue(values, "studio_id", "studioid"),
		Director:   firstValue(values, "director"),
		Synopsis:   firstValue(values, "synopsis", "description", "summary"),
		URLs:       splitList(firstValue(values, "urls", "url")),
		TagIDs:     splitList(firstValue(values, "tag_ids", "tags")),
		FrontImage: firstValue(values, "front_image", "poster", "poster_url"),
		BackImage:  firstValue(values, "back_image", "back_poster", "back_image_url"),
		Source:     firstValue(values, "source"),
	}

	for key, value := range values {
		if value == "" {
			continue
		}
		if language, ok := localizedTitleLanguage(key); ok {
			rec.LocalizedTitles = append(rec.LocalizedTitles, localizedTitleRecord{
				LanguageCode: language,
				Title:        value,
				Source:       rec.Source,
			})
		}
	}
	sort.Slice(rec.LocalizedTitles, func(a, b int) bool {
		return rec.LocalizedTitles[a].LanguageCode < rec.LocalizedTitles[b].LanguageCode
	})

	return rec
}

func localizedTitleLanguage(key string) (string, bool) {
	for _, prefix := range []string{"localized_title_", "title_", "name_"} {
		if strings.HasPrefix(key, prefix) {
			lang := strings.TrimSpace(strings.TrimPrefix(key, prefix))
			if lang != "" {
				return strings.ToLower(lang), true
			}
		}
	}
	return "", false
}

func (r groupRecord) isEmpty() bool {
	return r.ID == "" && r.Name == "" && len(r.LocalizedTitles) == 0
}

func (r groupRecord) identityKey() string {
	if r.ID != "" {
		return "id:" + r.ID
	}
	if r.Name != "" {
		return "name:" + strings.ToLower(r.Name)
	}
	return ""
}

func (r groupRecord) displayName() string {
	return firstNonEmpty(r.ID, r.Name, "?")
}

type graphqlClient struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

func (c *graphqlClient) findGroup(ctx context.Context, id string) (*groupRef, error) {
	const query = `
query FindGroupForGroupImport($id: ID!) {
  findGroup(id: $id) {
    id
    name
  }
}`

	var out struct {
		Group *groupRef `json:"findGroup"`
	}
	if err := c.do(ctx, query, map[string]any{"id": id}, &out); err != nil {
		return nil, fmt.Errorf("finding group id %q: %w", id, err)
	}
	return out.Group, nil
}

func (c *graphqlClient) findGroupByName(ctx context.Context, name string) (*groupRef, error) {
	const query = `
query FindGroupsForGroupImport($filter: FindFilterType) {
  findGroups(filter: $filter) {
    groups {
      id
      name
    }
  }
}`

	var out struct {
		FindGroups struct {
			Groups []groupRef `json:"groups"`
		} `json:"findGroups"`
	}
	variables := map[string]any{
		"filter": map[string]any{
			"q":        name,
			"per_page": 50,
		},
	}
	if err := c.do(ctx, query, variables, &out); err != nil {
		return nil, fmt.Errorf("finding group %q: %w", name, err)
	}

	var exact []groupRef
	for _, group := range out.FindGroups.Groups {
		if strings.EqualFold(group.Name, name) {
			exact = append(exact, group)
		}
	}
	if len(exact) == 0 {
		return nil, nil
	}
	if len(exact) > 1 {
		names := make([]string, 0, len(exact))
		for _, match := range exact {
			names = append(names, match.ID+":"+match.Name)
		}
		sort.Strings(names)
		return nil, fmt.Errorf("ambiguous group name %q matched %s", name, strings.Join(names, ", "))
	}

	return &exact[0], nil
}

func (c *graphqlClient) createGroup(ctx context.Context, rec groupRecord) (*groupRef, error) {
	const mutation = `
mutation CreateGroupForGroupImport($input: GroupCreateInput!) {
  groupCreate(input: $input) {
    id
    name
  }
}`

	input, err := groupCreateInput(rec)
	if err != nil {
		return nil, err
	}

	var out struct {
		GroupCreate *groupRef `json:"groupCreate"`
	}
	if err := c.do(ctx, mutation, map[string]any{"input": input}, &out); err != nil {
		return nil, fmt.Errorf("creating group %q: %w", rec.Name, err)
	}
	if out.GroupCreate == nil {
		return nil, errors.New("groupCreate returned null")
	}

	return out.GroupCreate, nil
}

func groupCreateInput(rec groupRecord) (map[string]any, error) {
	if rec.Name == "" {
		return nil, errors.New("missing name")
	}

	input := map[string]any{"name": rec.Name}
	addString(input, "aliases", rec.Aliases)
	addString(input, "date", normalizeDate(rec.Date))
	addString(input, "studio_id", rec.StudioID)
	addString(input, "director", rec.Director)
	addString(input, "synopsis", rec.Synopsis)
	addString(input, "front_image", rec.FrontImage)
	addString(input, "back_image", rec.BackImage)
	if len(rec.URLs) > 0 {
		input["urls"] = rec.URLs
	}
	if len(rec.TagIDs) > 0 {
		input["tag_ids"] = rec.TagIDs
	}
	if rec.Duration != "" {
		duration, err := strconv.Atoi(rec.Duration)
		if err != nil {
			return nil, fmt.Errorf("invalid duration %q", rec.Duration)
		}
		input["duration"] = duration
	}
	if rec.Rating100 != "" {
		rating, err := strconv.Atoi(rec.Rating100)
		if err != nil {
			return nil, fmt.Errorf("invalid rating100 %q", rec.Rating100)
		}
		input["rating100"] = rating
	}

	return input, nil
}

func (c *graphqlClient) findLocalizedTitle(ctx context.Context, objectType, objectID, languageCode string) (*existingTitle, error) {
	const query = `
query FindLocalizedTitleForGroupImport($objectType: LocalizedTitleObjectType!, $objectID: ID!, $languageCode: String!) {
  findLocalizedTitleForObjectLanguage(object_type: $objectType, object_id: $objectID, language_code: $languageCode) {
    id
    title
    source
  }
}`

	var out struct {
		Title *existingTitle `json:"findLocalizedTitleForObjectLanguage"`
	}
	variables := map[string]any{
		"objectType":   objectType,
		"objectID":     objectID,
		"languageCode": languageCode,
	}
	if err := c.do(ctx, query, variables, &out); err != nil {
		return nil, fmt.Errorf("finding localized title for %s:%s %s: %w", objectType, objectID, languageCode, err)
	}

	return out.Title, nil
}

func (c *graphqlClient) upsertLocalizedTitle(ctx context.Context, objectType, objectID string, title localizedTitleRecord) error {
	const mutation = `
mutation UpsertLocalizedTitleForGroupImport($input: LocalizedTitleCreateInput!) {
  localizedTitleUpsert(input: $input) {
    id
  }
}`

	input := map[string]any{
		"object_type":   objectType,
		"object_id":     objectID,
		"language_code": title.LanguageCode,
		"title":         title.Title,
	}
	if title.Source != "" {
		input["source"] = title.Source
	}

	var out struct {
		LocalizedTitleUpsert struct {
			ID string `json:"id"`
		} `json:"localizedTitleUpsert"`
	}
	if err := c.do(ctx, mutation, map[string]any{"input": input}, &out); err != nil {
		return fmt.Errorf("upserting localized title for %s:%s %s: %w", objectType, objectID, title.LanguageCode, err)
	}

	return nil
}

func (c *graphqlClient) do(ctx context.Context, query string, variables map[string]any, out any) error {
	payload := map[string]any{
		"query":     query,
		"variables": variables,
	}

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
	if len(decoded.Data) == 0 {
		return errors.New("missing GraphQL data")
	}

	return json.Unmarshal(decoded.Data, out)
}

func firstValue(values map[string]string, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(values[key]); value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizeHeader(header string) string {
	header = strings.TrimPrefix(header, "\ufeff")
	header = strings.TrimSpace(strings.ToLower(header))
	header = strings.ReplaceAll(header, "-", "_")
	header = strings.ReplaceAll(header, " ", "_")
	return header
}

func splitList(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == '|' || r == ';'
	})
	return trimList(parts)
}

func trimList(values []string) []string {
	var ret []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			ret = append(ret, value)
		}
	}
	return ret
}

func normalizeDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 4 {
		if _, err := strconv.Atoi(value); err == nil {
			return value + "-01-01"
		}
	}
	return value
}

func addString(input map[string]any, key, value string) {
	if strings.TrimSpace(value) != "" {
		input[key] = strings.TrimSpace(value)
	}
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
