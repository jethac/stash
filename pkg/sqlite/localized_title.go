package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/doug-martin/goqu/v9"
	"github.com/doug-martin/goqu/v9/exp"
	"github.com/jmoiron/sqlx"
	"gopkg.in/guregu/null.v4"

	"github.com/stashapp/stash/pkg/models"
)

const localizedTitleTable = "localized_titles"

type localizedTitleRow struct {
	ID           int         `db:"id" goqu:"skipinsert"`
	ObjectType   string      `db:"object_type"`
	ObjectID     int         `db:"object_id"`
	LanguageCode string      `db:"language_code"`
	Title        string      `db:"title"`
	Source       null.String `db:"source"`
	CreatedAt    Timestamp   `db:"created_at"`
	UpdatedAt    Timestamp   `db:"updated_at"`
}

func nullableStringFromPtr(v *string) null.String {
	if v == nil {
		return null.String{}
	}
	return null.StringFrom(*v)
}

func (r *localizedTitleRow) fromLocalizedTitle(o models.LocalizedTitle) {
	r.ID = o.ID
	r.ObjectType = string(o.ObjectType)
	r.ObjectID = o.ObjectID
	r.LanguageCode = o.LanguageCode
	r.Title = o.Title
	r.Source = nullableStringFromPtr(o.Source)
	r.CreatedAt = Timestamp{Timestamp: o.CreatedAt}
	r.UpdatedAt = Timestamp{Timestamp: o.UpdatedAt}
}

func (r *localizedTitleRow) resolve() *models.LocalizedTitle {
	ret := &models.LocalizedTitle{
		ID:           r.ID,
		ObjectType:   models.LocalizedTitleObjectType(r.ObjectType),
		ObjectID:     r.ObjectID,
		LanguageCode: r.LanguageCode,
		Title:        r.Title,
		CreatedAt:    r.CreatedAt.Timestamp,
		UpdatedAt:    r.UpdatedAt.Timestamp,
	}

	if r.Source.Valid {
		ret.Source = &r.Source.String
	}

	return ret
}

type LocalizedTitleStore struct {
	repository
	tableMgr *table
}

func NewLocalizedTitleStore() *LocalizedTitleStore {
	return &LocalizedTitleStore{
		repository: repository{
			tableName: localizedTitleTable,
			idColumn:  idColumn,
		},
		tableMgr: localizedTitleTableMgr,
	}
}

func (qb *LocalizedTitleStore) table() exp.IdentifierExpression {
	return qb.tableMgr.table
}

func (qb *LocalizedTitleStore) selectDataset() *goqu.SelectDataset {
	return dialect.From(qb.table()).Select(qb.table().All())
}

func (qb *LocalizedTitleStore) validate(o models.LocalizedTitle) error {
	if !o.ObjectType.IsValid() {
		return fmt.Errorf("invalid localized title object type %q", o.ObjectType)
	}
	if o.ObjectID == 0 {
		return fmt.Errorf("object id is required")
	}
	if strings.TrimSpace(o.LanguageCode) == "" {
		return fmt.Errorf("language code is required")
	}
	if strings.TrimSpace(o.Title) == "" {
		return fmt.Errorf("title is required")
	}

	return nil
}

func (qb *LocalizedTitleStore) prepareForWrite(o *models.LocalizedTitle, newRecord bool) error {
	if err := qb.validate(*o); err != nil {
		return err
	}

	currentTime := time.Now()
	if newRecord && o.CreatedAt.IsZero() {
		o.CreatedAt = currentTime
	}
	if o.UpdatedAt.IsZero() {
		o.UpdatedAt = currentTime
	}

	return nil
}

func (qb *LocalizedTitleStore) Create(ctx context.Context, newObject *models.LocalizedTitle) error {
	if err := qb.prepareForWrite(newObject, true); err != nil {
		return err
	}

	var r localizedTitleRow
	r.fromLocalizedTitle(*newObject)

	id, err := qb.tableMgr.insertID(ctx, r)
	if err != nil {
		return err
	}

	updated, err := qb.find(ctx, id)
	if err != nil {
		return fmt.Errorf("finding after create: %w", err)
	}

	*newObject = *updated

	return nil
}

func (qb *LocalizedTitleStore) Update(ctx context.Context, updatedObject *models.LocalizedTitle) error {
	if err := qb.prepareForWrite(updatedObject, false); err != nil {
		return err
	}

	var r localizedTitleRow
	r.fromLocalizedTitle(*updatedObject)

	if err := qb.tableMgr.updateByID(ctx, updatedObject.ID, r); err != nil {
		return err
	}

	return nil
}

func (qb *LocalizedTitleStore) Upsert(ctx context.Context, obj *models.LocalizedTitle) error {
	if err := qb.prepareForWrite(obj, true); err != nil {
		return err
	}

	var r localizedTitleRow
	r.fromLocalizedTitle(*obj)

	q := dialect.Insert(qb.table()).
		Prepared(true).
		Rows(r).
		OnConflict(goqu.DoUpdate("object_type, object_id, language_code", goqu.Record{
			"title":      r.Title,
			"source":     r.Source,
			"updated_at": r.UpdatedAt,
		}))

	if _, err := exec(ctx, q); err != nil {
		return fmt.Errorf("upserting localized title: %w", err)
	}

	updated, err := qb.FindForObjectLanguage(ctx, obj.ObjectType, obj.ObjectID, obj.LanguageCode)
	if err != nil {
		return fmt.Errorf("finding after upsert: %w", err)
	}

	*obj = *updated

	return nil
}

func (qb *LocalizedTitleStore) Destroy(ctx context.Context, id int) error {
	return qb.destroyExisting(ctx, []int{id})
}

func (qb *LocalizedTitleStore) DestroyForObject(ctx context.Context, objectType models.LocalizedTitleObjectType, objectID int) error {
	table := qb.table()
	q := dialect.Delete(table).Where(
		table.Col("object_type").Eq(objectType),
		table.Col("object_id").Eq(objectID),
	)

	if _, err := exec(ctx, q); err != nil {
		return fmt.Errorf("destroying localized titles for object: %w", err)
	}

	return nil
}

func (qb *LocalizedTitleStore) Find(ctx context.Context, id int) (*models.LocalizedTitle, error) {
	ret, err := qb.find(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func (qb *LocalizedTitleStore) FindMany(ctx context.Context, ids []int, ignoreNotFound bool) ([]*models.LocalizedTitle, error) {
	ret := make([]*models.LocalizedTitle, len(ids))

	table := qb.table()
	q := qb.selectDataset().Prepared(true).Where(table.Col(idColumn).In(ids))
	unsorted, err := qb.getMany(ctx, q)
	if err != nil {
		return nil, err
	}

	for _, s := range unsorted {
		i := slices.Index(ids, s.ID)
		ret[i] = s
	}

	if !ignoreNotFound {
		for i := range ret {
			if ret[i] == nil {
				return nil, fmt.Errorf("localized title with id %d not found", ids[i])
			}
		}
	}

	return ret, nil
}

func (qb *LocalizedTitleStore) FindForObject(ctx context.Context, objectType models.LocalizedTitleObjectType, objectID int) ([]*models.LocalizedTitle, error) {
	table := qb.table()
	q := qb.selectDataset().
		Prepared(true).
		Where(
			table.Col("object_type").Eq(objectType),
			table.Col("object_id").Eq(objectID),
		).
		Order(table.Col("language_code").Asc())

	return qb.getMany(ctx, q)
}

func (qb *LocalizedTitleStore) FindForObjectLanguage(ctx context.Context, objectType models.LocalizedTitleObjectType, objectID int, languageCode string) (*models.LocalizedTitle, error) {
	table := qb.table()
	q := qb.selectDataset().
		Prepared(true).
		Where(
			table.Col("object_type").Eq(objectType),
			table.Col("object_id").Eq(objectID),
			table.Col("language_code").Eq(languageCode),
		)

	ret, err := qb.get(ctx, q)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ret, err
}

func (qb *LocalizedTitleStore) find(ctx context.Context, id int) (*models.LocalizedTitle, error) {
	q := qb.selectDataset().Where(qb.tableMgr.byID(id))

	ret, err := qb.get(ctx, q)
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (qb *LocalizedTitleStore) get(ctx context.Context, q *goqu.SelectDataset) (*models.LocalizedTitle, error) {
	ret, err := qb.getMany(ctx, q)
	if err != nil {
		return nil, err
	}

	if len(ret) == 0 {
		return nil, sql.ErrNoRows
	}

	return ret[0], nil
}

func (qb *LocalizedTitleStore) getMany(ctx context.Context, q *goqu.SelectDataset) ([]*models.LocalizedTitle, error) {
	const single = false
	var ret []*models.LocalizedTitle
	if err := queryFunc(ctx, q, single, func(r *sqlx.Rows) error {
		var f localizedTitleRow
		if err := r.StructScan(&f); err != nil {
			return err
		}

		ret = append(ret, f.resolve())
		return nil
	}); err != nil {
		return nil, err
	}

	return ret, nil
}
