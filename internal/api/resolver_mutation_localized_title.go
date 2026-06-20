package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
)

func localizedTitleFromCreateInput(input LocalizedTitleCreateInput) (*models.LocalizedTitle, error) {
	objectID, err := strconv.Atoi(input.ObjectID)
	if err != nil {
		return nil, fmt.Errorf("converting object id: %w", err)
	}

	title := models.NewLocalizedTitle()
	title.ObjectType = input.ObjectType
	title.ObjectID = objectID
	title.LanguageCode = input.LanguageCode
	title.Title = input.Title
	title.Source = input.Source

	return &title, nil
}

func localizedTitleFromUpdateInput(input LocalizedTitleUpdateInput) (*models.LocalizedTitle, error) {
	id, err := strconv.Atoi(input.ID)
	if err != nil {
		return nil, fmt.Errorf("converting id: %w", err)
	}

	title, err := localizedTitleFromCreateInput(LocalizedTitleCreateInput{
		ObjectType:   input.ObjectType,
		ObjectID:     input.ObjectID,
		LanguageCode: input.LanguageCode,
		Title:        input.Title,
		Source:       input.Source,
	})
	if err != nil {
		return nil, err
	}

	title.ID = id

	return title, nil
}

func (r *mutationResolver) LocalizedTitleCreate(ctx context.Context, input LocalizedTitleCreateInput) (ret *models.LocalizedTitle, err error) {
	title, err := localizedTitleFromCreateInput(input)
	if err != nil {
		return nil, err
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		if err := r.repository.LocalizedTitle.Create(ctx, title); err != nil {
			return err
		}
		ret = title
		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *mutationResolver) LocalizedTitleUpdate(ctx context.Context, input LocalizedTitleUpdateInput) (ret *models.LocalizedTitle, err error) {
	title, err := localizedTitleFromUpdateInput(input)
	if err != nil {
		return nil, err
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		if err := r.repository.LocalizedTitle.Update(ctx, title); err != nil {
			return err
		}
		ret, err = r.repository.LocalizedTitle.Find(ctx, title.ID)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *mutationResolver) LocalizedTitleUpsert(ctx context.Context, input LocalizedTitleCreateInput) (ret *models.LocalizedTitle, err error) {
	title, err := localizedTitleFromCreateInput(input)
	if err != nil {
		return nil, err
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		if err := r.repository.LocalizedTitle.Upsert(ctx, title); err != nil {
			return err
		}
		ret = title
		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *mutationResolver) LocalizedTitleDestroy(ctx context.Context, id string) (bool, error) {
	idInt, err := strconv.Atoi(id)
	if err != nil {
		return false, fmt.Errorf("converting id: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		return r.repository.LocalizedTitle.Destroy(ctx, idInt)
	}); err != nil {
		return false, err
	}

	return true, nil
}

func (r *mutationResolver) LocalizedTitlesDestroyForObject(ctx context.Context, objectType models.LocalizedTitleObjectType, objectID string) (bool, error) {
	idInt, err := strconv.Atoi(objectID)
	if err != nil {
		return false, fmt.Errorf("converting object id: %w", err)
	}

	if err := r.withTxn(ctx, func(ctx context.Context) error {
		return r.repository.LocalizedTitle.DestroyForObject(ctx, objectType, idInt)
	}); err != nil {
		return false, err
	}

	return true, nil
}
