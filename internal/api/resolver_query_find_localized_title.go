package api

import (
	"context"
	"fmt"
	"strconv"

	"github.com/stashapp/stash/pkg/models"
)

func (r *queryResolver) FindLocalizedTitle(ctx context.Context, id string) (ret *models.LocalizedTitle, err error) {
	idInt, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("converting id: %w", err)
	}

	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.LocalizedTitle.Find(ctx, idInt)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *queryResolver) FindLocalizedTitlesForObject(ctx context.Context, objectType models.LocalizedTitleObjectType, objectID string) (ret []*models.LocalizedTitle, err error) {
	idInt, err := strconv.Atoi(objectID)
	if err != nil {
		return nil, fmt.Errorf("converting object id: %w", err)
	}

	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.LocalizedTitle.FindForObject(ctx, objectType, idInt)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}

func (r *queryResolver) FindLocalizedTitleForObjectLanguage(ctx context.Context, objectType models.LocalizedTitleObjectType, objectID string, languageCode string) (ret *models.LocalizedTitle, err error) {
	idInt, err := strconv.Atoi(objectID)
	if err != nil {
		return nil, fmt.Errorf("converting object id: %w", err)
	}

	if err := r.withReadTxn(ctx, func(ctx context.Context) error {
		ret, err = r.repository.LocalizedTitle.FindForObjectLanguage(ctx, objectType, idInt, languageCode)
		return err
	}); err != nil {
		return nil, err
	}

	return ret, nil
}
