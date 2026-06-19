package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLoadCSVRecordsUsesAliasesAndLocalizedTitles(t *testing.T) {
	input := `movie_name,release_date,poster_url,url,title_ja,title_en,source
Movie A,2024,http://poster.example/a.jpg,http://example/a,日本語タイトル,English Title,plex
`

	records, err := loadCSVRecords(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	got := records[0]
	if got.Name != "Movie A" {
		t.Fatalf("Name = %q", got.Name)
	}
	if got.Date != "2024" {
		t.Fatalf("Date = %q", got.Date)
	}
	if got.FrontImage != "http://poster.example/a.jpg" {
		t.Fatalf("FrontImage = %q", got.FrontImage)
	}
	if len(got.URLs) != 1 || got.URLs[0] != "http://example/a" {
		t.Fatalf("URLs = %#v", got.URLs)
	}
	if len(got.LocalizedTitles) != 2 {
		t.Fatalf("LocalizedTitles = %#v", got.LocalizedTitles)
	}
	if got.LocalizedTitles[0].LanguageCode != "en" || got.LocalizedTitles[0].Title != "English Title" {
		t.Fatalf("first localized title = %#v", got.LocalizedTitles[0])
	}
	if got.LocalizedTitles[1].LanguageCode != "ja" || got.LocalizedTitles[1].Title != "日本語タイトル" {
		t.Fatalf("second localized title = %#v", got.LocalizedTitles[1])
	}
}

func TestGroupCreateInputNormalizesYearAndNumbers(t *testing.T) {
	input, err := groupCreateInput(groupRecord{
		Name:      "Movie A",
		Date:      "2024",
		Duration:  "7200",
		Rating100: "80",
		URLs:      []string{"http://example/a"},
	})
	if err != nil {
		t.Fatal(err)
	}

	if input["date"] != "2024-01-01" {
		t.Fatalf("date = %#v", input["date"])
	}
	if input["duration"] != 7200 {
		t.Fatalf("duration = %#v", input["duration"])
	}
	if input["rating100"] != 80 {
		t.Fatalf("rating100 = %#v", input["rating100"])
	}
}

func TestPlanTitleActionIsConservativeWithoutOverwrite(t *testing.T) {
	title := localizedTitleRecord{LanguageCode: "ja", Title: "New Title", Source: "plex"}
	source := "manual"
	existing := &existingTitle{ID: "1", Title: "Old Title", Source: &source}

	gotAction, _ := planTitleAction(title, existing, false)
	if gotAction != actionConflictTitle {
		t.Fatalf("expected conflict, got %s", gotAction)
	}

	gotAction, _ = planTitleAction(title, existing, true)
	if gotAction != actionWouldUpdateTitle {
		t.Fatalf("expected update, got %s", gotAction)
	}
}

func TestImporterDetectsDuplicateConflicts(t *testing.T) {
	client := &fakeClient{}
	importer := importer{cfg: config{}, client: client}
	records := []groupRecord{
		{Line: 2, Name: "Movie A"},
		{Line: 3, Name: "Movie A", Synopsis: "Different"},
	}

	var out bytes.Buffer
	summary, err := importer.run(context.Background(), records, &out)
	if err != nil {
		t.Fatal(err)
	}
	if summary.DuplicateConflict != 1 {
		t.Fatalf("expected one duplicate conflict, got %#v\n%s", summary, out.String())
	}
}

func TestImporterDryRunCreatesGroupAndDefersTitles(t *testing.T) {
	client := &fakeClient{}
	importer := importer{cfg: config{}, client: client}
	records := []groupRecord{
		{
			Line: 2,
			Name: "Movie A",
			LocalizedTitles: []localizedTitleRecord{
				{LanguageCode: "ja", Title: "日本語タイトル"},
			},
		},
	}

	var out bytes.Buffer
	summary, err := importer.run(context.Background(), records, &out)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CreateGroup != 1 || summary.CreateTitle != 1 {
		t.Fatalf("unexpected summary %#v\n%s", summary, out.String())
	}
	if !strings.Contains(out.String(), "would apply after group creation") {
		t.Fatalf("missing deferred title detail:\n%s", out.String())
	}
}

type fakeClient struct {
	groups map[string]groupRef
	titles map[string]existingTitle
	nextID int
}

func (f *fakeClient) findGroup(_ context.Context, id string) (*groupRef, error) {
	if f.groups == nil {
		return nil, nil
	}
	for _, group := range f.groups {
		if group.ID == id {
			return &group, nil
		}
	}
	return nil, nil
}

func (f *fakeClient) findGroupByName(_ context.Context, name string) (*groupRef, error) {
	if f.groups == nil {
		return nil, nil
	}
	for _, group := range f.groups {
		if strings.EqualFold(group.Name, name) {
			return &group, nil
		}
	}
	return nil, nil
}

func (f *fakeClient) createGroup(_ context.Context, rec groupRecord) (*groupRef, error) {
	if f.groups == nil {
		f.groups = make(map[string]groupRef)
	}
	f.nextID++
	group := groupRef{ID: "new-id", Name: rec.Name}
	f.groups[rec.Name] = group
	return &group, nil
}

func (f *fakeClient) findLocalizedTitle(_ context.Context, objectType, objectID, languageCode string) (*existingTitle, error) {
	if f.titles == nil {
		return nil, nil
	}
	title, ok := f.titles[objectType+":"+objectID+":"+languageCode]
	if !ok {
		return nil, nil
	}
	return &title, nil
}

func (f *fakeClient) upsertLocalizedTitle(_ context.Context, objectType, objectID string, title localizedTitleRecord) error {
	if f.titles == nil {
		f.titles = make(map[string]existingTitle)
	}
	f.titles[objectType+":"+objectID+":"+title.LanguageCode] = existingTitle{ID: "title-id", Title: title.Title, Source: &title.Source}
	return nil
}
