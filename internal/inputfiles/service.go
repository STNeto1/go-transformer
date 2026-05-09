package inputfiles

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/types"

	"go-transformer/internal/db/models"
	"go-transformer/internal/storage"
)

var ErrNotFound = errors.New("input file not found")

type Service struct {
	db      bob.DB
	storage *storage.Client
}

type File struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Bucket      string         `json:"bucket"`
	ObjectKey   string         `json:"object_key"`
	URI         string         `json:"uri"`
	Format      string         `json:"format"`
	ContentType *string        `json:"content_type,omitempty"`
	SizeBytes   int64          `json:"size_bytes"`
	ETag        *string        `json:"etag,omitempty"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type RegisterRequest struct {
	Name      string         `json:"name,omitempty" example:"people.csv"`
	Bucket    string         `json:"bucket" example:"inputs"`
	ObjectKey string         `json:"object_key" example:"people.csv"`
	Format    string         `json:"format" example:"csv"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type UpdateRequest struct {
	Name     *string         `json:"name,omitempty" example:"people.csv"`
	Format   *string         `json:"format,omitempty" example:"csv"`
	Metadata *map[string]any `json:"metadata,omitempty"`
}

type ListOptions struct {
	Limit  int
	Offset int
	Bucket string
	Format string
	Name   string
}

func NewService(db *sql.DB, storageClient *storage.Client) *Service {
	return &Service{db: bob.NewDB(db), storage: storageClient}
}

func (s *Service) Register(ctx context.Context, req RegisterRequest) (*File, error) {
	if err := validateRegister(&req, s.storage.DefaultInputBucket()); err != nil {
		return nil, err
	}
	obj, err := s.storage.StatObject(ctx, req.Bucket, req.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("stat object: %w", err)
	}
	if strings.TrimSpace(req.Name) == "" {
		req.Name = filepath.Base(req.ObjectKey)
	}
	return s.insert(ctx, req, obj)
}

func (s *Service) Upload(ctx context.Context, name string, objectKey string, format string, metadata map[string]any, file multipart.File, size int64, contentType string) (*File, error) {
	bucket := s.storage.DefaultInputBucket()
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(objectKey)
	}
	if strings.TrimSpace(objectKey) == "" {
		objectKey = name
	}
	req := RegisterRequest{Name: name, Bucket: bucket, ObjectKey: objectKey, Format: format, Metadata: metadata}
	if err := validateRegister(&req, s.storage.DefaultInputBucket()); err != nil {
		return nil, err
	}
	obj, err := s.storage.PutObject(ctx, req.Bucket, req.ObjectKey, file, size, contentType)
	if err != nil {
		return nil, fmt.Errorf("put object: %w", err)
	}
	return s.insert(ctx, req, obj)
}

func (s *Service) List(ctx context.Context, opts ListOptions) ([]File, error) {
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 50
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	mods := []bob.Mod[*dialect.SelectQuery]{}
	if opts.Bucket != "" {
		mods = append(mods, models.SelectWhere.InputFiles.Bucket.EQ(opts.Bucket))
	}
	if opts.Format != "" {
		if err := validateFormat(opts.Format); err != nil {
			return nil, err
		}
		mods = append(mods, models.SelectWhere.InputFiles.Format.EQ(opts.Format))
	}
	if opts.Name != "" {
		mods = append(mods, models.SelectWhere.InputFiles.Name.ILike("%"+opts.Name+"%"))
	}
	mods = append(mods, sm.OrderBy(models.InputFiles.Columns.ID).Desc(), sm.Limit(opts.Limit), sm.Offset(opts.Offset))
	rows, err := models.InputFiles.Query(mods...).All(ctx, s.db)
	if err != nil {
		return nil, err
	}
	files := make([]File, 0, len(rows))
	for _, row := range rows {
		files = append(files, mapModel(row))
	}
	return files, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*File, error) {
	f, err := models.FindInputFile(ctx, s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	mapped := mapModel(f)
	return &mapped, nil
}

func (s *Service) Update(ctx context.Context, id int64, req UpdateRequest) (*File, error) {
	var updated File
	if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, exec bob.Executor) error {
		model, err := models.FindInputFile(ctx, exec, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}

		current := mapModel(model)
		if req.Name != nil {
			current.Name = strings.TrimSpace(*req.Name)
			if current.Name == "" {
				return fmt.Errorf("name is required")
			}
		}
		if req.Format != nil {
			format := strings.ToLower(strings.TrimSpace(*req.Format))
			if err := validateFormat(format); err != nil {
				return err
			}
			current.Format = format
		}
		if req.Metadata != nil {
			current.Metadata = nonNilMetadata(*req.Metadata)
		}

		now := time.Now().UTC()
		setter, err := setterFromFile(current)
		if err != nil {
			return err
		}
		setter.UpdatedAt = &now
		if err := model.Update(ctx, exec, setter); err != nil {
			return err
		}
		updated = mapModel(model)
		return nil
	}); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, exec bob.Executor) error {
		f, err := models.FindInputFile(ctx, exec, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return f.Delete(ctx, exec)
	})
}

func (s *Service) PresignedDownload(ctx context.Context, id int64, expiry time.Duration) (string, error) {
	f, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	return s.storage.PresignedGetObject(ctx, f.Bucket, f.ObjectKey, expiry)
}

func (s *Service) insert(ctx context.Context, req RegisterRequest, obj storage.ObjectInfo) (*File, error) {
	f := File{Name: req.Name, Bucket: req.Bucket, ObjectKey: req.ObjectKey, URI: storage.URI(req.Bucket, req.ObjectKey), Format: req.Format, SizeBytes: obj.Size, Metadata: nonNilMetadata(req.Metadata)}
	if obj.ContentType != "" {
		f.ContentType = &obj.ContentType
	}
	if obj.ETag != "" {
		f.ETag = &obj.ETag
	}
	setter, err := setterFromFile(f)
	if err != nil {
		return nil, err
	}
	created, err := models.InputFiles.Insert(setter).One(ctx, s.db)
	if isUniqueViolation(err) {
		return nil, fmt.Errorf("input file already exists: %w", err)
	}
	if err != nil {
		return nil, err
	}
	mapped := mapModel(created)
	return &mapped, nil
}

func validateRegister(req *RegisterRequest, defaultBucket string) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Bucket = strings.Trim(strings.TrimSpace(req.Bucket), "/")
	req.ObjectKey = strings.TrimLeft(strings.TrimSpace(req.ObjectKey), "/")
	req.Format = strings.ToLower(strings.TrimSpace(req.Format))
	if req.Bucket == "" {
		req.Bucket = defaultBucket
	}
	if req.ObjectKey == "" {
		return fmt.Errorf("object_key is required")
	}
	if req.Name == "" {
		req.Name = filepath.Base(req.ObjectKey)
	}
	if req.Name == "." || req.Name == "/" || req.Name == "" {
		return fmt.Errorf("name is required")
	}
	return validateFormat(req.Format)
}

func validateFormat(format string) error {
	if format != "csv" && format != "parquet" {
		return fmt.Errorf("format must be csv or parquet")
	}
	return nil
}

func nonNilMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}
	return metadata
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}

func setterFromFile(f File) (*models.InputFileSetter, error) {
	metadata, err := json.Marshal(nonNilMetadata(f.Metadata))
	if err != nil {
		return nil, err
	}
	setter := &models.InputFileSetter{
		Name:      &f.Name,
		Bucket:    &f.Bucket,
		ObjectKey: &f.ObjectKey,
		URI:       &f.URI,
		Format:    &f.Format,
		SizeBytes: &f.SizeBytes,
		Metadata:  &types.JSON[json.RawMessage]{Val: metadata},
	}
	if f.ContentType != nil {
		setter.ContentType = &sql.Null[string]{V: *f.ContentType, Valid: true}
	}
	if f.ETag != nil {
		setter.Etag = &sql.Null[string]{V: *f.ETag, Valid: true}
	}
	return setter, nil
}

func mapModel(m *models.InputFile) File {
	f := File{ID: m.ID, Name: m.Name, Bucket: m.Bucket, ObjectKey: m.ObjectKey, URI: m.URI, Format: m.Format, SizeBytes: m.SizeBytes, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
	if m.ContentType.Valid {
		f.ContentType = &m.ContentType.V
	}
	if m.Etag.Valid {
		f.ETag = &m.Etag.V
	}
	if len(m.Metadata.Val) > 0 {
		_ = json.Unmarshal(m.Metadata.Val, &f.Metadata)
	}
	f.Metadata = nonNilMetadata(f.Metadata)
	return f
}

func DecodeMetadata(value string) (map[string]any, error) {
	if strings.TrimSpace(value) == "" {
		return map[string]any{}, nil
	}
	var metadata map[string]any
	if err := json.Unmarshal([]byte(value), &metadata); err != nil {
		return nil, fmt.Errorf("metadata must be a JSON object")
	}
	return nonNilMetadata(metadata), nil
}
