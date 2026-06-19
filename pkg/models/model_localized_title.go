package models

import (
	"time"
)

type LocalizedTitleObjectType string

const (
	LocalizedTitleObjectTypeScene     LocalizedTitleObjectType = "SCENE"
	LocalizedTitleObjectTypeGroup     LocalizedTitleObjectType = "GROUP"
	LocalizedTitleObjectTypePerformer LocalizedTitleObjectType = "PERFORMER"
	LocalizedTitleObjectTypeStudio    LocalizedTitleObjectType = "STUDIO"
	LocalizedTitleObjectTypeTag       LocalizedTitleObjectType = "TAG"
	LocalizedTitleObjectTypeGallery   LocalizedTitleObjectType = "GALLERY"
	LocalizedTitleObjectTypeImage     LocalizedTitleObjectType = "IMAGE"
)

func (e LocalizedTitleObjectType) IsValid() bool {
	switch e {
	case LocalizedTitleObjectTypeScene,
		LocalizedTitleObjectTypeGroup,
		LocalizedTitleObjectTypePerformer,
		LocalizedTitleObjectTypeStudio,
		LocalizedTitleObjectTypeTag,
		LocalizedTitleObjectTypeGallery,
		LocalizedTitleObjectTypeImage:
		return true
	}
	return false
}

type LocalizedTitle struct {
	ID           int                      `db:"id" json:"id"`
	ObjectType   LocalizedTitleObjectType `db:"object_type" json:"object_type"`
	ObjectID     int                      `db:"object_id" json:"object_id"`
	LanguageCode string                   `db:"language_code" json:"language_code"`
	Title        string                   `db:"title" json:"title"`
	Source       *string                  `db:"source" json:"source"`
	CreatedAt    time.Time                `db:"created_at" json:"created_at"`
	UpdatedAt    time.Time                `db:"updated_at" json:"updated_at"`
}

func NewLocalizedTitle() LocalizedTitle {
	currentTime := time.Now()
	return LocalizedTitle{
		CreatedAt: currentTime,
		UpdatedAt: currentTime,
	}
}
