package main

import (
	"strings"
	"testing"
)

func TestLoadCSVRecordsUsesAliases(t *testing.T) {
	input := `group_name,lang,localized_title,source
Movie A,ja,日本語タイトル,plex
`

	records, err := loadCSVRecords(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	got := records[0]
	if got.ObjectName != "Movie A" {
		t.Fatalf("ObjectName = %q", got.ObjectName)
	}
	if got.LanguageCode != "ja" {
		t.Fatalf("LanguageCode = %q", got.LanguageCode)
	}
	if got.Title != "日本語タイトル" {
		t.Fatalf("Title = %q", got.Title)
	}
	if got.Source != "plex" {
		t.Fatalf("Source = %q", got.Source)
	}
}

func TestPlanActionIsConservativeWithoutOverwrite(t *testing.T) {
	rec := resolvedRecord{
		titleRecord: titleRecord{
			ObjectType:   "GROUP",
			ObjectID:     "12",
			LanguageCode: "ja",
			Title:        "New Title",
			Source:       "plex",
		},
		ObjectID: "12",
	}
	oldSource := "manual"
	existing := &existingTitle{ID: "99", Title: "Old Title", Source: &oldSource}

	gotAction, _ := planAction(rec, existing, false)
	if gotAction != actionConflict {
		t.Fatalf("expected conflict, got %s", gotAction)
	}

	gotAction, _ = planAction(rec, existing, true)
	if gotAction != actionUpdate {
		t.Fatalf("expected update, got %s", gotAction)
	}
}

func TestPlanActionIgnoresSourceWhenTitleMatches(t *testing.T) {
	rec := resolvedRecord{
		titleRecord: titleRecord{
			ObjectType:   "GROUP",
			ObjectID:     "12",
			LanguageCode: "ja",
			Title:        "Same Title",
			Source:       "plex",
		},
		ObjectID: "12",
	}
	oldSource := "manual"
	existing := &existingTitle{ID: "99", Title: "Same Title", Source: &oldSource}

	gotAction, _ := planAction(rec, existing, false)
	if gotAction != actionUnchanged {
		t.Fatalf("expected unchanged, got %s", gotAction)
	}
}

func TestDuplicateAction(t *testing.T) {
	rec := resolvedRecord{
		titleRecord: titleRecord{
			ObjectType:   "GROUP",
			ObjectID:     "12",
			LanguageCode: "ja",
			Title:        "Title",
			Source:       "plex",
		},
		ObjectID: "12",
	}

	gotAction, _ := duplicateAction(rec, rec)
	if gotAction != actionDuplicate {
		t.Fatalf("expected duplicate, got %s", gotAction)
	}

	changed := rec
	changed.Title = "Different"
	gotAction, _ = duplicateAction(rec, changed)
	if gotAction != actionDuplicateConflict {
		t.Fatalf("expected duplicate conflict, got %s", gotAction)
	}
}
