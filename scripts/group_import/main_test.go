package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestLoadCSVRecordsUsesAliasesAndLocalizedTitles(t *testing.T) {
	input := `movie_name,release_date,poster_url,url,file_path,title_ja,title_en,source
Movie A,2024,http://poster.example/a.jpg,http://example/a,/medialibrary/Ero/Movie A.mp4,日本語タイトル,English Title,plex
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
	if got.ScenePath != "/medialibrary/Ero/Movie A.mp4" {
		t.Fatalf("ScenePath = %q", got.ScenePath)
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

func TestImporterRewritesScenePath(t *testing.T) {
	importer := importer{
		cfg: config{
			PathFrom: "/medialibrary/Ero",
			PathTo:   "/data",
		},
	}

	got := importer.withDefaults(groupRecord{
		Name:      "Movie A",
		ScenePath: "/medialibrary/Ero/Porn [EN]/Movie A.mp4",
	})
	if got.ScenePath != "/data/Porn [EN]/Movie A.mp4" {
		t.Fatalf("ScenePath = %q", got.ScenePath)
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

func TestImporterDryRunLinksExistingScene(t *testing.T) {
	client := &fakeClient{
		groups: map[string]groupRef{
			"Movie A": {ID: "12", Name: "Movie A"},
		},
		scenes: map[string]sceneRef{
			"/data/Movie A.mp4": {ID: "99", Title: "Movie A", Path: "/data/Movie A.mp4"},
		},
	}
	importer := importer{cfg: config{}, client: client}
	records := []groupRecord{
		{Line: 2, Name: "Movie A", ScenePath: "/data/Movie A.mp4"},
	}

	var out bytes.Buffer
	summary, err := importer.run(context.Background(), records, &out)
	if err != nil {
		t.Fatal(err)
	}
	if summary.ExistingGroup != 1 || summary.LinkScene != 1 {
		t.Fatalf("unexpected summary %#v\n%s", summary, out.String())
	}
}

func TestImporterDryRunAllowsMultipleScenesForOneGroup(t *testing.T) {
	client := &fakeClient{
		scenes: map[string]sceneRef{
			"/data/Movie A - Part 1.mp4": {ID: "91", Title: "Movie A Part 1", Path: "/data/Movie A - Part 1.mp4"},
			"/data/Movie A - Part 2.mp4": {ID: "92", Title: "Movie A Part 2", Path: "/data/Movie A - Part 2.mp4"},
		},
	}
	importer := importer{cfg: config{}, client: client}
	records := []groupRecord{
		{Line: 2, Name: "Movie A", Date: "2024", ScenePath: "/data/Movie A - Part 1.mp4"},
		{Line: 3, Name: "Movie A", Date: "2024", ScenePath: "/data/Movie A - Part 2.mp4"},
	}

	var out bytes.Buffer
	summary, err := importer.run(context.Background(), records, &out)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CreateGroup != 1 || summary.LinkScene != 2 || summary.DuplicateConflict != 0 {
		t.Fatalf("unexpected summary %#v\n%s", summary, out.String())
	}
}

func TestImporterRejectsMultipleRowsForOneGroupWithDifferentMetadata(t *testing.T) {
	client := &fakeClient{
		scenes: map[string]sceneRef{
			"/data/Movie A - Part 1.mp4": {ID: "91", Title: "Movie A Part 1", Path: "/data/Movie A - Part 1.mp4"},
			"/data/Movie A - Part 2.mp4": {ID: "92", Title: "Movie A Part 2", Path: "/data/Movie A - Part 2.mp4"},
		},
	}
	importer := importer{cfg: config{}, client: client}
	records := []groupRecord{
		{Line: 2, Name: "Movie A", Date: "2024", ScenePath: "/data/Movie A - Part 1.mp4"},
		{Line: 3, Name: "Movie A", Date: "2025", ScenePath: "/data/Movie A - Part 2.mp4"},
	}

	var out bytes.Buffer
	summary, err := importer.run(context.Background(), records, &out)
	if err != nil {
		t.Fatal(err)
	}
	if summary.DuplicateConflict != 1 {
		t.Fatalf("expected duplicate conflict, got %#v\n%s", summary, out.String())
	}
}

func TestImporterApplyLinksScene(t *testing.T) {
	client := &fakeClient{
		groups: map[string]groupRef{
			"Movie A": {ID: "12", Name: "Movie A"},
		},
		scenes: map[string]sceneRef{
			"/data/Movie A.mp4": {ID: "99", Title: "Movie A", Path: "/data/Movie A.mp4"},
		},
	}
	importer := importer{cfg: config{Apply: true}, client: client}
	records := []groupRecord{
		{Line: 2, Name: "Movie A", ScenePath: "/data/Movie A.mp4"},
	}

	var out bytes.Buffer
	summary, err := importer.run(context.Background(), records, &out)
	if err != nil {
		t.Fatal(err)
	}
	if summary.LinkScene != 1 {
		t.Fatalf("unexpected summary %#v\n%s", summary, out.String())
	}
	scene := client.scenes["/data/Movie A.mp4"]
	if len(scene.Groups) != 1 || scene.Groups[0].ID != "12" {
		t.Fatalf("scene groups = %#v", scene.Groups)
	}
}

type fakeClient struct {
	groups map[string]groupRef
	titles map[string]existingTitle
	scenes map[string]sceneRef
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

func (f *fakeClient) findSceneByPath(_ context.Context, path string) (*sceneRef, error) {
	if f.scenes == nil {
		return nil, nil
	}
	scene, ok := f.scenes[path]
	if !ok {
		return nil, nil
	}
	return &scene, nil
}

func (f *fakeClient) addGroupToScene(_ context.Context, sceneID, groupID string, _ *int) error {
	for path, scene := range f.scenes {
		if scene.ID == sceneID {
			scene.Groups = append(scene.Groups, groupRef{ID: groupID})
			f.scenes[path] = scene
			return nil
		}
	}
	return nil
}
