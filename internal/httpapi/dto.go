package httpapi

import (
	"go-transformer/internal/inputfiles"
	"go-transformer/internal/pipeline"
	"go-transformer/internal/pipelines"
)

type HealthResponse struct {
	Status string `json:"status" example:"ok"`
}

type ValidatePipelineRequest struct {
	PipelineID string                 `json:"pipeline_id" example:"example_pipeline"`
	Version    int                    `json:"version" example:"1"`
	Defaults   ValidateDefaults       `json:"defaults,omitempty"`
	Nodes      []ValidatePipelineNode `json:"nodes"`
	Sinks      []ValidatePipelineSink `json:"sinks"`
}

type ValidateDefaults struct {
	TelemetryFormat string `json:"telemetry_format,omitempty" example:"json"`
	TelemetryLevel  string `json:"telemetry_level,omitempty" example:"basic"`
}

type ValidatePipelineNode struct {
	ID     string         `json:"id" example:"source"`
	Type   string         `json:"type" example:"DataSource"`
	Inputs any            `json:"inputs,omitempty"`
	Config map[string]any `json:"config"`
}

type ValidatePipelineSink struct {
	NodeID      string `json:"node_id" example:"limit_rows"`
	TargetTable string `json:"target_table" example:"output_rows"`
}

type ValidatePipelineSuccessResponse struct {
	Valid      bool   `json:"valid" example:"true"`
	PipelineID string `json:"pipeline_id" example:"example_pipeline"`
	Version    int    `json:"version" example:"1"`
	Nodes      int    `json:"nodes" example:"3"`
	Sinks      int    `json:"sinks" example:"1"`
}

type ValidatePipelineErrorResponse struct {
	Valid  bool                       `json:"valid" example:"false"`
	Error  string                     `json:"error" example:"pipeline validation failed"`
	Issues []pipeline.ValidationIssue `json:"issues,omitempty"`
}

type PipelineResponse = pipelines.Pipeline
type SavePipelineRequest = pipelines.SaveRequest

type PipelineListResponse struct {
	Pipelines []pipelines.Pipeline `json:"pipelines"`
}

type PipelineErrorResponse struct {
	Error string `json:"error" example:"pipeline not found"`
}

type InputFileResponse = inputfiles.File
type RegisterInputFileRequest = inputfiles.RegisterRequest
type UpdateInputFileRequest = inputfiles.UpdateRequest

type InputFileListResponse struct {
	Files []inputfiles.File `json:"files"`
}

type InputFileDownloadResponse struct {
	URL string `json:"url" example:"http://localhost:9000/inputs/people.csv?..."`
}

type InputFileErrorResponse struct {
	Error string `json:"error" example:"input file not found"`
}
