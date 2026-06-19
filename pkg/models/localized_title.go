package models

import "context"

type LocalizedTitleReader interface {
	Find(ctx context.Context, id int) (*LocalizedTitle, error)
	FindMany(ctx context.Context, ids []int, ignoreNotFound bool) ([]*LocalizedTitle, error)
	FindForObject(ctx context.Context, objectType LocalizedTitleObjectType, objectID int) ([]*LocalizedTitle, error)
	FindForObjectLanguage(ctx context.Context, objectType LocalizedTitleObjectType, objectID int, languageCode string) (*LocalizedTitle, error)
}

type LocalizedTitleWriter interface {
	Create(ctx context.Context, obj *LocalizedTitle) error
	Update(ctx context.Context, obj *LocalizedTitle) error
	Upsert(ctx context.Context, obj *LocalizedTitle) error
	Destroy(ctx context.Context, id int) error
	DestroyForObject(ctx context.Context, objectType LocalizedTitleObjectType, objectID int) error
}

type LocalizedTitleReaderWriter interface {
	LocalizedTitleReader
	LocalizedTitleWriter
}
