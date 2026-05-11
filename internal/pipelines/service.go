package pipelines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
	"github.com/stephenafamo/bob"
	"github.com/stephenafamo/bob/dialect/psql/dialect"
	"github.com/stephenafamo/bob/dialect/psql/sm"
	"github.com/stephenafamo/bob/types"

	"go-transformer/internal/db/models"
	"go-transformer/internal/pipeline"
)

const (
	StatusDraft  = "draft"
	StatusActive = "active"
)

var ErrNotFound = errors.New("pipeline not found")

type Service struct {
	db bob.DB
}

type Pipeline struct {
	ID              int64            `json:"id"`
	Name            string           `json:"name"`
	PipelineID      string           `json:"pipeline_id"`
	Version         int              `json:"version"`
	Spec            json.RawMessage  `json:"spec"`
	Status          string           `json:"status"`
	ValidationError *json.RawMessage `json:"validation_error,omitempty"`
	ActivatedAt     *time.Time       `json:"activated_at,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type SaveRequest struct {
	Name string          `json:"name"`
	Spec json.RawMessage `json:"spec"`
}

type ListOptions struct {
	Limit  int
	Offset int
	Status string
	Name   string
}

func NewService(db *sql.DB) *Service {
	return &Service{db: bob.NewDB(db)}
}

func (s *Service) List(ctx context.Context, opts ListOptions) ([]Pipeline, error) {
	if opts.Limit <= 0 || opts.Limit > 100 {
		opts.Limit = 50
	}
	if opts.Offset < 0 {
		opts.Offset = 0
	}
	mods := []bob.Mod[*dialect.SelectQuery]{}
	if opts.Status != "" {
		if opts.Status != StatusDraft && opts.Status != StatusActive {
			return nil, fmt.Errorf("status must be draft or active")
		}
		mods = append(mods, models.SelectWhere.Pipelines.Status.EQ(opts.Status))
	}
	if opts.Name != "" {
		mods = append(mods, models.SelectWhere.Pipelines.Name.ILike("%"+opts.Name+"%"))
	}
	mods = append(mods, sm.OrderBy(models.Pipelines.Columns.ID).Desc(), sm.Limit(opts.Limit), sm.Offset(opts.Offset))
	rows, err := models.Pipelines.Query(mods...).All(ctx, s.db)
	if err != nil {
		return nil, err
	}
	items := make([]Pipeline, 0, len(rows))
	for _, row := range rows {
		items = append(items, mapModel(row))
	}
	return items, nil
}

func (s *Service) Create(ctx context.Context, req SaveRequest) (*Pipeline, error) {
	prepared, err := prepareDraft(req)
	if err != nil {
		return nil, err
	}
	setter := setterFromPipeline(*prepared)
	created, err := models.Pipelines.Insert(setter).One(ctx, s.db)
	if isUniqueViolation(err) {
		return nil, fmt.Errorf("pipeline_id already exists: %w", err)
	}
	if err != nil {
		return nil, err
	}
	mapped := mapModel(created)
	return &mapped, nil
}

func (s *Service) Get(ctx context.Context, id int64) (*Pipeline, error) {
	row, err := models.FindPipeline(ctx, s.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	mapped := mapModel(row)
	return &mapped, nil
}

func (s *Service) Update(ctx context.Context, id int64, req SaveRequest) (*Pipeline, error) {
	prepared, err := prepareDraft(req)
	if err != nil {
		return nil, err
	}
	var updated Pipeline
	if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, exec bob.Executor) error {
		row, err := models.FindPipeline(ctx, exec, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		setter := setterFromPipeline(*prepared)
		now := time.Now().UTC()
		setter.UpdatedAt = &now
		if err := row.Update(ctx, exec, setter); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("pipeline_id already exists: %w", err)
			}
			return err
		}
		updated = mapModel(row)
		return nil
	}); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *Service) Delete(ctx context.Context, id int64) error {
	return s.db.RunInTx(ctx, nil, func(ctx context.Context, exec bob.Executor) error {
		row, err := models.FindPipeline(ctx, exec, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		return row.Delete(ctx, exec)
	})
}

func (s *Service) Activate(ctx context.Context, id int64) (*Pipeline, error) {
	var activated Pipeline
	var activationErr error
	if err := s.db.RunInTx(ctx, nil, func(ctx context.Context, exec bob.Executor) error {
		row, err := models.FindPipeline(ctx, exec, id)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := pipeline.ParseAndValidateJSON(row.Spec.Val); err != nil {
			validationJSON := validationErrorJSON(err)
			now := time.Now().UTC()
			status := StatusDraft
			setter := &models.PipelineSetter{Status: &status, ValidationError: &sql.Null[types.JSON[json.RawMessage]]{V: types.JSON[json.RawMessage]{Val: validationJSON}, Valid: true}, UpdatedAt: &now}
			if updateErr := row.Update(ctx, exec, setter); updateErr != nil {
				return updateErr
			}
			activationErr = err
			return nil
		}
		now := time.Now().UTC()
		status := StatusActive
		setter := &models.PipelineSetter{Status: &status, ValidationError: &sql.Null[types.JSON[json.RawMessage]]{}, ActivatedAt: &sql.Null[time.Time]{V: now, Valid: true}, UpdatedAt: &now}
		if err := row.Update(ctx, exec, setter); err != nil {
			return err
		}
		activated = mapModel(row)
		return nil
	}); err != nil {
		return nil, err
	}
	if activationErr != nil {
		return nil, activationErr
	}
	return &activated, nil
}

func prepareDraft(req SaveRequest) (*Pipeline, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if len(req.Spec) == 0 || !json.Valid(req.Spec) {
		return nil, fmt.Errorf("spec must be valid JSON")
	}
	var minimal struct {
		PipelineID string `json:"pipeline_id"`
		Version    int    `json:"version"`
	}
	if err := json.Unmarshal(req.Spec, &minimal); err != nil {
		return nil, fmt.Errorf("spec must be a JSON object")
	}
	minimal.PipelineID = strings.TrimSpace(minimal.PipelineID)
	if minimal.PipelineID == "" {
		return nil, fmt.Errorf("spec.pipeline_id is required")
	}
	if minimal.Version < 1 {
		return nil, fmt.Errorf("spec.version must be >= 1")
	}
	return &Pipeline{Name: name, PipelineID: minimal.PipelineID, Version: minimal.Version, Spec: req.Spec, Status: StatusDraft}, nil
}

func setterFromPipeline(p Pipeline) *models.PipelineSetter {
	status := StatusDraft
	version := int32(p.Version)
	return &models.PipelineSetter{Name: &p.Name, PipelineID: &p.PipelineID, Version: &version, Spec: &types.JSON[json.RawMessage]{Val: p.Spec}, Status: &status, ValidationError: &sql.Null[types.JSON[json.RawMessage]]{}, ActivatedAt: &sql.Null[time.Time]{}}
}

func mapModel(m *models.Pipeline) Pipeline {
	p := Pipeline{ID: m.ID, Name: m.Name, PipelineID: m.PipelineID, Version: int(m.Version), Spec: m.Spec.Val, Status: m.Status, CreatedAt: m.CreatedAt, UpdatedAt: m.UpdatedAt}
	if m.ValidationError.Valid && len(m.ValidationError.V.Val) > 0 {
		value := json.RawMessage(append([]byte(nil), m.ValidationError.V.Val...))
		p.ValidationError = &value
	}
	if m.ActivatedAt.Valid {
		p.ActivatedAt = &m.ActivatedAt.V
	}
	return p
}

func validationErrorJSON(err error) json.RawMessage {
	payload := map[string]any{"error": err.Error()}
	var validationErr *pipeline.ValidationError
	if errors.As(err, &validationErr) {
		payload["issues"] = validationErr.Issues
	}
	data, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return json.RawMessage(`{"error":"validation failed"}`)
	}
	return data
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "23505"
}
