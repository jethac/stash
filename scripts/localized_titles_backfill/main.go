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
	"strings"
	"text/tabwriter"
	"time"
)

const defaultObjectType = "GROUP"

type config struct {
	Endpoint      string
	APIKey        string
	InputPath     string
	Format        string
	DefaultType   string
	DefaultSource string
	Apply         bool
	Overwrite     bool
	Timeout       time.Duration
}

type titleRecord struct {
	Line         int
	ObjectType   string
	ObjectID     string
	ObjectName   string
	LanguageCode string
	Title        string
	Source       string
}

type resolvedRecord struct {
	titleRecord
	ObjectID string
}

type existingTitle struct {
	ID     string  `json:"id"`
	Title  string  `json:"title"`
	Source *string `json:"source"`
}

type action string

const (
	actionCreate            action = "create"
	actionUpdate            action = "update"
	actionUnchanged         action = "unchanged"
	actionConflict          action = "conflict"
	actionDuplicate         action = "duplicate"
	actionDuplicateConflict action = "duplicate_conflict"
	actionError             action = "error"
)

type result struct {
	Action action
	Record resolvedRecord
	Detail string
}

type summary struct {
	Total             int
	Create            int
	Update            int
	Unchanged         int
	Conflict          int
	Duplicate         int
	DuplicateConflict int
	Error             int
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

	ctx := context.Background()
	s, err := importer.run(ctx, records, stdout)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}

	if s.Error > 0 || s.Conflict > 0 || s.DuplicateConflict > 0 {
		return 1
	}

	return 0
}

func parseConfig(args []string, stderr io.Writer) (config, error) {
	cfg := config{}
	flags := flag.NewFlagSet("localized_titles_backfill", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&cfg.Endpoint, "endpoint", "", "Stash GraphQL endpoint, for example http://nas:9999/graphql")
	flags.StringVar(&cfg.APIKey, "api-key", os.Getenv("STASH_API_KEY"), "Stash API key. Defaults to STASH_API_KEY.")
	flags.StringVar(&cfg.InputPath, "input", "", "CSV or JSON input file")
	flags.StringVar(&cfg.Format, "format", "", "Input format: csv or json. Defaults to file extension.")
	flags.StringVar(&cfg.DefaultType, "object-type", defaultObjectType, "Default localized title object type")
	flags.StringVar(&cfg.DefaultSource, "source", "localized-title-backfill", "Default source for rows without a source")
	flags.BoolVar(&cfg.Apply, "apply", false, "Apply mutations. Without this flag, the script only reports planned changes.")
	flags.BoolVar(&cfg.Overwrite, "overwrite", false, "Allow replacing an existing different title/source value")
	flags.DurationVar(&cfg.Timeout, "timeout", 30*time.Second, "HTTP timeout")

	if err := flags.Parse(args); err != nil {
		return cfg, err
	}

	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.InputPath = strings.TrimSpace(cfg.InputPath)
	cfg.Format = strings.ToLower(strings.TrimSpace(cfg.Format))
	cfg.DefaultType = normalizeObjectType(cfg.DefaultType)
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
	if cfg.DefaultType == "" {
		return cfg, errors.New("missing --object-type")
	}

	return cfg, nil
}

type importer struct {
	cfg    config
	client stashClient
}

type stashClient interface {
	findGroupByName(ctx context.Context, name string) (string, string, error)
	findLocalizedTitle(ctx context.Context, objectType, objectID, languageCode string) (*existingTitle, error)
	upsertLocalizedTitle(ctx context.Context, rec resolvedRecord) error
}

func (i importer) run(ctx context.Context, records []titleRecord, out io.Writer) (summary, error) {
	var results []result
	seen := make(map[string]resolvedRecord)

	for _, raw := range records {
		rec := i.withDefaults(raw)
		resolved, err := i.resolve(ctx, rec)
		if err != nil {
			results = append(results, result{Action: actionError, Record: resolvedRecord{titleRecord: rec}, Detail: err.Error()})
			continue
		}

		key := dedupeKey(resolved)
		if previous, ok := seen[key]; ok {
			action, detail := duplicateAction(previous, resolved)
			results = append(results, result{Action: action, Record: resolved, Detail: detail})
			continue
		}
		seen[key] = resolved

		existing, err := i.client.findLocalizedTitle(ctx, resolved.ObjectType, resolved.ObjectID, resolved.LanguageCode)
		if err != nil {
			results = append(results, result{Action: actionError, Record: resolved, Detail: err.Error()})
			continue
		}

		nextAction, detail := planAction(resolved, existing, i.cfg.Overwrite)
		if i.cfg.Apply && (nextAction == actionCreate || nextAction == actionUpdate) {
			if err := i.client.upsertLocalizedTitle(ctx, resolved); err != nil {
				results = append(results, result{Action: actionError, Record: resolved, Detail: err.Error()})
				continue
			}
			detail = "applied"
		}

		results = append(results, result{Action: nextAction, Record: resolved, Detail: detail})
	}

	s := writeResults(out, i.cfg.Apply, results)
	return s, nil
}

func (i importer) withDefaults(rec titleRecord) titleRecord {
	rec.ObjectType = normalizeObjectType(firstNonEmpty(rec.ObjectType, i.cfg.DefaultType))
	rec.ObjectID = strings.TrimSpace(rec.ObjectID)
	rec.ObjectName = strings.TrimSpace(rec.ObjectName)
	rec.LanguageCode = strings.ToLower(strings.TrimSpace(rec.LanguageCode))
	rec.Title = strings.TrimSpace(rec.Title)
	rec.Source = strings.TrimSpace(firstNonEmpty(rec.Source, i.cfg.DefaultSource))
	return rec
}

func (i importer) resolve(ctx context.Context, rec titleRecord) (resolvedRecord, error) {
	resolved := resolvedRecord{titleRecord: rec, ObjectID: rec.ObjectID}

	if rec.LanguageCode == "" {
		return resolved, errors.New("missing language_code")
	}
	if rec.Title == "" {
		return resolved, errors.New("missing title")
	}
	if rec.ObjectID != "" {
		return resolved, nil
	}
	if rec.ObjectType != "GROUP" {
		return resolved, fmt.Errorf("missing object_id; name lookup is only supported for GROUP rows, got %s", rec.ObjectType)
	}
	if rec.ObjectName == "" {
		return resolved, errors.New("missing object_id or object_name")
	}

	id, matchedName, err := i.client.findGroupByName(ctx, rec.ObjectName)
	if err != nil {
		return resolved, err
	}

	resolved.ObjectID = id
	resolved.ObjectName = matchedName
	return resolved, nil
}

func duplicateAction(previous, current resolvedRecord) (action, string) {
	if previous.Title == current.Title && previous.Source == current.Source {
		return actionDuplicate, fmt.Sprintf("same %s/%s/%s already appeared earlier", current.ObjectType, current.ObjectID, current.LanguageCode)
	}

	return actionDuplicateConflict, fmt.Sprintf(
		"duplicate %s/%s/%s has a different value; earlier title=%q source=%q",
		current.ObjectType,
		current.ObjectID,
		current.LanguageCode,
		previous.Title,
		previous.Source,
	)
}

func planAction(rec resolvedRecord, existing *existingTitle, overwrite bool) (action, string) {
	if existing == nil {
		return actionCreate, "missing localized title"
	}

	if existing.Title == rec.Title {
		if rec.Source != "" && stringPtrValue(existing.Source) != rec.Source {
			return actionUnchanged, fmt.Sprintf("title matches; existing source=%q", stringPtrValue(existing.Source))
		}

		return actionUnchanged, "already matches"
	}

	if !overwrite {
		return actionConflict, fmt.Sprintf("existing title=%q source=%q; pass --overwrite to replace", existing.Title, stringPtrValue(existing.Source))
	}

	return actionUpdate, fmt.Sprintf("replacing existing title=%q source=%q", existing.Title, stringPtrValue(existing.Source))
}

func writeResults(out io.Writer, apply bool, results []result) summary {
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ACTION\tOBJECT\tLANG\tTITLE\tDETAIL")

	s := summary{Total: len(results)}
	for _, r := range results {
		switch r.Action {
		case actionCreate:
			s.Create++
		case actionUpdate:
			s.Update++
		case actionUnchanged:
			s.Unchanged++
		case actionConflict:
			s.Conflict++
		case actionDuplicate:
			s.Duplicate++
		case actionDuplicateConflict:
			s.DuplicateConflict++
		case actionError:
			s.Error++
		}

		label := string(r.Action)
		if !apply && (r.Action == actionCreate || r.Action == actionUpdate) {
			label = "would_" + label
		}

		fmt.Fprintf(
			tw,
			"%s\t%s:%s\t%s\t%s\t%s\n",
			label,
			firstNonEmpty(r.Record.ObjectType, "?"),
			firstNonEmpty(r.Record.ObjectID, "?"),
			firstNonEmpty(r.Record.LanguageCode, "?"),
			r.Record.Title,
			r.Detail,
		)
	}

	_ = tw.Flush()
	mode := "dry-run"
	if apply {
		mode = "apply"
	}
	fmt.Fprintf(
		out,
		"\nSummary (%s): total=%d create=%d update=%d unchanged=%d conflict=%d duplicate=%d duplicate_conflict=%d error=%d\n",
		mode,
		s.Total,
		s.Create,
		s.Update,
		s.Unchanged,
		s.Conflict,
		s.Duplicate,
		s.DuplicateConflict,
		s.Error,
	)

	return s
}

func loadRecords(path, format string) ([]titleRecord, error) {
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

func loadCSVRecords(r io.Reader) ([]titleRecord, error) {
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

	var records []titleRecord
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
		if rec == (titleRecord{Line: line}) {
			continue
		}
		records = append(records, rec)
	}

	return records, nil
}

func loadJSONRecords(r io.Reader) ([]titleRecord, error) {
	var rows []map[string]any
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return nil, err
	}

	records := make([]titleRecord, 0, len(rows))
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
		records = append(records, rec)
	}

	return records, nil
}

func recordFromValues(values map[string]string) titleRecord {
	return titleRecord{
		ObjectType:   firstValue(values, "object_type", "objecttype", "type"),
		ObjectID:     firstValue(values, "object_id", "objectid", "group_id", "movie_id", "id"),
		ObjectName:   firstValue(values, "object_name", "objectname", "group_name", "movie_name", "name"),
		LanguageCode: firstValue(values, "language_code", "languagecode", "lang", "language"),
		Title:        firstValue(values, "title", "localized_title", "localizedtitle"),
		Source:       firstValue(values, "source"),
	}
}

type graphqlClient struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

func (c *graphqlClient) findGroupByName(ctx context.Context, name string) (string, string, error) {
	const query = `
query FindGroupsForLocalizedTitleBackfill($filter: FindFilterType) {
  findGroups(filter: $filter) {
    groups {
      id
      name
    }
  }
}`

	var out struct {
		FindGroups struct {
			Groups []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"groups"`
		} `json:"findGroups"`
	}

	variables := map[string]any{
		"filter": map[string]any{
			"q":        name,
			"per_page": 50,
		},
	}
	if err := c.do(ctx, query, variables, &out); err != nil {
		return "", "", fmt.Errorf("finding group %q: %w", name, err)
	}

	var exact []struct {
		ID   string
		Name string
	}
	for _, group := range out.FindGroups.Groups {
		if strings.EqualFold(group.Name, name) {
			exact = append(exact, struct {
				ID   string
				Name string
			}{ID: group.ID, Name: group.Name})
		}
	}

	if len(exact) == 0 {
		return "", "", fmt.Errorf("no exact group name match for %q", name)
	}
	if len(exact) > 1 {
		names := make([]string, 0, len(exact))
		for _, match := range exact {
			names = append(names, match.ID+":"+match.Name)
		}
		sort.Strings(names)
		return "", "", fmt.Errorf("ambiguous group name %q matched %s", name, strings.Join(names, ", "))
	}

	return exact[0].ID, exact[0].Name, nil
}

func (c *graphqlClient) findLocalizedTitle(ctx context.Context, objectType, objectID, languageCode string) (*existingTitle, error) {
	const query = `
query FindLocalizedTitleForBackfill($objectType: LocalizedTitleObjectType!, $objectID: ID!, $languageCode: String!) {
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

func (c *graphqlClient) upsertLocalizedTitle(ctx context.Context, rec resolvedRecord) error {
	const mutation = `
mutation UpsertLocalizedTitleBackfill($input: LocalizedTitleCreateInput!) {
  localizedTitleUpsert(input: $input) {
    id
  }
}`

	input := map[string]any{
		"object_type":   rec.ObjectType,
		"object_id":     rec.ObjectID,
		"language_code": rec.LanguageCode,
		"title":         rec.Title,
	}
	if rec.Source != "" {
		input["source"] = rec.Source
	}

	var out struct {
		LocalizedTitleUpsert struct {
			ID string `json:"id"`
		} `json:"localizedTitleUpsert"`
	}
	if err := c.do(ctx, mutation, map[string]any{"input": input}, &out); err != nil {
		return fmt.Errorf("upserting localized title for %s:%s %s: %w", rec.ObjectType, rec.ObjectID, rec.LanguageCode, err)
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

func normalizeObjectType(objectType string) string {
	return strings.ToUpper(strings.TrimSpace(objectType))
}

func stringPtrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func dedupeKey(rec resolvedRecord) string {
	return rec.ObjectType + "\x00" + rec.ObjectID + "\x00" + rec.LanguageCode
}
