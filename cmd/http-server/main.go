package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	swagno3 "github.com/go-swagno/swagno/v3"
	"github.com/go-swagno/swagno/v3/components/endpoint"
	"github.com/go-swagno/swagno/v3/components/http/response"
	"github.com/go-swagno/swagno/v3/components/mime"

	"go-transformer/internal/pipeline"
)

type healthResponse struct {
	Status string `json:"status" example:"ok"`
}

type validatePipelineRequest struct {
	PipelineID string                 `json:"pipeline_id" example:"example_pipeline"`
	Version    int                    `json:"version" example:"1"`
	Defaults   validateDefaults       `json:"defaults,omitempty"`
	Nodes      []validatePipelineNode `json:"nodes"`
	Sinks      []validatePipelineSink `json:"sinks"`
}

type validateDefaults struct {
	TelemetryFormat string `json:"telemetry_format,omitempty" example:"json"`
	TelemetryLevel  string `json:"telemetry_level,omitempty" example:"basic"`
}

type validatePipelineNode struct {
	ID     string         `json:"id" example:"source"`
	Type   string         `json:"type" example:"DataSource"`
	Inputs any            `json:"inputs,omitempty"`
	Config map[string]any `json:"config"`
}

type validatePipelineSink struct {
	NodeID      string `json:"node_id" example:"limit_rows"`
	TargetTable string `json:"target_table" example:"output_rows"`
}

type validatePipelineSuccessResponse struct {
	Valid      bool   `json:"valid" example:"true"`
	PipelineID string `json:"pipeline_id" example:"example_pipeline"`
	Version    int    `json:"version" example:"1"`
	Nodes      int    `json:"nodes" example:"3"`
	Sinks      int    `json:"sinks" example:"1"`
}

type validatePipelineErrorResponse struct {
	Valid  bool                       `json:"valid" example:"false"`
	Error  string                     `json:"error" example:"pipeline validation failed"`
	Issues []pipeline.ValidationIssue `json:"issues,omitempty"`
}

func main() {
	addr := flag.String("addr", ":8888", "HTTP listen address")
	flag.Parse()

	h := server.Default(server.WithHostPorts(*addr))

	h.GET("/healthz", func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]string{"status": "ok"})
	})

	h.POST("/pipelines/validate", validatePipeline)

	openAPI := buildOpenAPI(*addr)
	h.GET("/swagger/doc.json", func(_ context.Context, c *app.RequestContext) {
		c.Response.Header.SetContentType("application/json")
		c.Write(openAPI)
	})

	log.Printf("serving HTTP on %s", *addr)
	h.Spin()
}

func buildOpenAPI(addr string) []byte {
	openAPI := swagno3.New(swagno3.Config{
		Title:       "Go Transformer HTTP API",
		Version:     "v1",
		Description: "HTTP API for validating go-transformer pipeline specifications.",
	})
	openAPI.AddServer(serverURL(addr), "HTTP server")
	openAPI.AddEndpoints([]*endpoint.EndPoint{
		endpoint.New(
			endpoint.GET,
			"/healthz",
			endpoint.WithTags("system"),
			endpoint.WithSummary("Health check"),
			endpoint.WithDescription("Reports whether the HTTP server is running."),
			endpoint.WithSuccessfulReturns([]response.Response{response.New(healthResponse{}, "200", "OK")}),
			endpoint.WithProduce([]mime.MIME{mime.JSON}),
		),
		endpoint.New(
			endpoint.POST,
			"/pipelines/validate",
			endpoint.WithTags("pipelines"),
			endpoint.WithSummary("Validate pipeline"),
			endpoint.WithDescription("Validates a pipeline JSON document without executing it."),
			endpoint.WithBody(validatePipelineRequest{}),
			endpoint.WithSuccessfulReturns([]response.Response{response.New(validatePipelineSuccessResponse{}, "200", "Valid pipeline")}),
			endpoint.WithErrors([]response.Response{response.New(validatePipelineErrorResponse{}, "400", "Invalid pipeline")}),
			endpoint.WithConsume([]mime.MIME{mime.JSON}),
			endpoint.WithProduce([]mime.MIME{mime.JSON}),
		),
	})

	return patchOpenAPI(openAPI.MustToJson())
}

func patchOpenAPI(doc []byte) []byte {
	var spec map[string]any
	if err := json.Unmarshal(doc, &spec); err != nil {
		return doc
	}

	components, _ := spec["components"].(map[string]any)
	schemas, _ := components["schemas"].(map[string]any)
	nodeSchema, _ := schemas["main.validatePipelineNode"].(map[string]any)
	properties, _ := nodeSchema["properties"].(map[string]any)
	properties["config"] = map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"description":          "Node-specific configuration. Shape depends on the node type.",
	}
	properties["inputs"] = map[string]any{
		"description": "Input node references. Most nodes use an array of node IDs; joins use an object with left and right node IDs.",
		"oneOf": []any{
			map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
			map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
		},
	}
	delete(schemas, "main.validatePipelineNode.config")

	patched, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return doc
	}
	return patched
}

func serverURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "http://localhost:8888"
	}
	if strings.HasPrefix(addr, ":") {
		return "http://localhost" + addr
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return addr
	}
	return "http://" + addr
}

func validatePipeline(_ context.Context, c *app.RequestContext) {
	body := c.Request.Body()
	if len(body) == 0 {
		c.JSON(consts.StatusBadRequest, map[string]any{"valid": false, "error": "request body is required"})
		return
	}

	spec, err := pipeline.ParseAndValidateJSON(body)
	if err != nil {
		var validationErr *pipeline.ValidationError
		if errors.As(err, &validationErr) {
			c.JSON(consts.StatusBadRequest, map[string]any{"valid": false, "error": err.Error(), "issues": validationErr.Issues})
			return
		}
		c.JSON(consts.StatusBadRequest, map[string]any{"valid": false, "error": err.Error()})
		return
	}

	c.JSON(consts.StatusOK, map[string]any{
		"valid":       true,
		"pipeline_id": spec.PipelineID,
		"version":     spec.Version,
		"nodes":       len(spec.Nodes),
		"sinks":       len(spec.Sinks),
	})
}
