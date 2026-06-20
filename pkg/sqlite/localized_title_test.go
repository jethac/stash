//go:build integration
// +build integration

package sqlite_test

import (
	"context"
	"testing"

	"github.com/stashapp/stash/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalizedTitleCreateFindAndDestroy(t *testing.T) {
	var id int
	source := "manual"

	withTxn(func(ctx context.Context) error {
		title := models.NewLocalizedTitle()
		title.ObjectType = models.LocalizedTitleObjectTypeGroup
		title.ObjectID = groupIDs[groupIdxWithScene]
		title.LanguageCode = "ja"
		title.Title = "日本語タイトル"
		title.Source = &source

		require.NoError(t, db.LocalizedTitle.Create(ctx, &title))
		require.NotZero(t, title.ID)
		id = title.ID

		found, err := db.LocalizedTitle.Find(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, found)
		assert.Equal(t, title.ObjectType, found.ObjectType)
		assert.Equal(t, title.ObjectID, found.ObjectID)
		assert.Equal(t, title.LanguageCode, found.LanguageCode)
		assert.Equal(t, title.Title, found.Title)
		require.NotNil(t, found.Source)
		assert.Equal(t, source, *found.Source)

		return nil
	})

	withTxn(func(ctx context.Context) error {
		require.NoError(t, db.LocalizedTitle.Destroy(ctx, id))

		found, err := db.LocalizedTitle.Find(ctx, id)
		require.NoError(t, err)
		assert.Nil(t, found)

		return nil
	})
}

func TestLocalizedTitleUpsertAndFindForObject(t *testing.T) {
	withTxn(func(ctx context.Context) error {
		groupID := groupIDs[groupIdxWithStudio]
		title := models.NewLocalizedTitle()
		title.ObjectType = models.LocalizedTitleObjectTypeGroup
		title.ObjectID = groupID
		title.LanguageCode = "fr"
		title.Title = "Titre francais"

		require.NoError(t, db.LocalizedTitle.Upsert(ctx, &title))
		firstID := title.ID
		require.NotZero(t, firstID)

		title.Title = "Titre francais corrige"
		require.NoError(t, db.LocalizedTitle.Upsert(ctx, &title))
		assert.Equal(t, firstID, title.ID)
		assert.Equal(t, "Titre francais corrige", title.Title)

		forObject, err := db.LocalizedTitle.FindForObject(ctx, models.LocalizedTitleObjectTypeGroup, groupID)
		require.NoError(t, err)
		require.Len(t, forObject, 1)
		assert.Equal(t, firstID, forObject[0].ID)

		forLanguage, err := db.LocalizedTitle.FindForObjectLanguage(ctx, models.LocalizedTitleObjectTypeGroup, groupID, "fr")
		require.NoError(t, err)
		require.NotNil(t, forLanguage)
		assert.Equal(t, "Titre francais corrige", forLanguage.Title)

		return nil
	})
}

func TestLocalizedTitleFindMany(t *testing.T) {
	var ids []int

	withTxn(func(ctx context.Context) error {
		for _, languageCode := range []string{"en", "ja"} {
			title := models.NewLocalizedTitle()
			title.ObjectType = models.LocalizedTitleObjectTypeScene
			title.ObjectID = sceneIDs[sceneIdxWithGroup]
			title.LanguageCode = languageCode
			title.Title = "localized title " + languageCode

			require.NoError(t, db.LocalizedTitle.Create(ctx, &title))
			ids = append(ids, title.ID)
		}

		found, err := db.LocalizedTitle.FindMany(ctx, ids, false)
		require.NoError(t, err)
		require.Len(t, found, 2)
		assert.Equal(t, ids[0], found[0].ID)
		assert.Equal(t, ids[1], found[1].ID)

		return nil
	})
}
