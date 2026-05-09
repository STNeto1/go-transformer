package httpapi

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	swagno3 "github.com/go-swagno/swagno/v3"
	"github.com/go-swagno/swagno/v3/components/endpoint"
	"github.com/go-swagno/swagno/v3/components/http/response"
	"github.com/go-swagno/swagno/v3/components/mime"
)

func RegisterOpenAPIRoute(h *server.Hertz, addr string) {
	openAPI := buildOpenAPI(addr)
	h.GET("/swagger/doc.json", func(_ context.Context, c *app.RequestContext) {
		c.Response.Header.SetContentType("application/json")
		c.Write(openAPI)
	})
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
			endpoint.WithSuccessfulReturns([]response.Response{response.New(HealthResponse{}, "200", "OK")}),
			endpoint.WithProduce([]mime.MIME{mime.JSON}),
		),
		endpoint.New(
			endpoint.POST,
			"/pipelines/validate",
			endpoint.WithTags("pipelines"),
			endpoint.WithSummary("Validate pipeline"),
			endpoint.WithDescription("Validates a pipeline JSON document without executing it."),
			endpoint.WithBody(ValidatePipelineRequest{}),
			endpoint.WithSuccessfulReturns([]response.Response{response.New(ValidatePipelineSuccessResponse{}, "200", "Valid pipeline")}),
			endpoint.WithErrors([]response.Response{response.New(ValidatePipelineErrorResponse{}, "400", "Invalid pipeline")}),
			endpoint.WithConsume([]mime.MIME{mime.JSON}),
			endpoint.WithProduce([]mime.MIME{mime.JSON}),
		),
		endpoint.New(endpoint.POST, "/input-files/upload", endpoint.WithTags("input-files"), endpoint.WithSummary("Upload input file"), endpoint.WithDescription("Uploads a file to RustFS/S3 and records input file metadata."), endpoint.WithSuccessfulReturns([]response.Response{response.New(InputFileResponse{}, "201", "Created")}), endpoint.WithErrors([]response.Response{response.New(InputFileErrorResponse{}, "400", "Invalid request"), response.New(InputFileErrorResponse{}, "500", "Server error")}), endpoint.WithProduce([]mime.MIME{mime.JSON})),
		endpoint.New(endpoint.POST, "/input-files/register", endpoint.WithTags("input-files"), endpoint.WithSummary("Register input file"), endpoint.WithDescription("Registers metadata for an existing RustFS/S3 object after verifying it exists."), endpoint.WithBody(RegisterInputFileRequest{}), endpoint.WithSuccessfulReturns([]response.Response{response.New(InputFileResponse{}, "201", "Created")}), endpoint.WithErrors([]response.Response{response.New(InputFileErrorResponse{}, "400", "Invalid request"), response.New(InputFileErrorResponse{}, "409", "Duplicate file"), response.New(InputFileErrorResponse{}, "500", "Server error")}), endpoint.WithConsume([]mime.MIME{mime.JSON}), endpoint.WithProduce([]mime.MIME{mime.JSON})),
		endpoint.New(endpoint.GET, "/input-files", endpoint.WithTags("input-files"), endpoint.WithSummary("List input files"), endpoint.WithDescription("Lists registered input file metadata."), endpoint.WithSuccessfulReturns([]response.Response{response.New(InputFileListResponse{}, "200", "OK")}), endpoint.WithErrors([]response.Response{response.New(InputFileErrorResponse{}, "400", "Invalid request"), response.New(InputFileErrorResponse{}, "500", "Server error")}), endpoint.WithProduce([]mime.MIME{mime.JSON})),
		endpoint.New(endpoint.GET, "/input-files/{id}", endpoint.WithTags("input-files"), endpoint.WithSummary("Get input file"), endpoint.WithDescription("Gets one registered input file metadata record."), endpoint.WithSuccessfulReturns([]response.Response{response.New(InputFileResponse{}, "200", "OK")}), endpoint.WithErrors([]response.Response{response.New(InputFileErrorResponse{}, "404", "Not found"), response.New(InputFileErrorResponse{}, "500", "Server error")}), endpoint.WithProduce([]mime.MIME{mime.JSON})),
		endpoint.New(endpoint.GET, "/input-files/{id}/download", endpoint.WithTags("input-files"), endpoint.WithSummary("Get input file download URL"), endpoint.WithDescription("Creates a presigned RustFS/S3 GET URL for an input file."), endpoint.WithSuccessfulReturns([]response.Response{response.New(InputFileDownloadResponse{}, "200", "OK")}), endpoint.WithErrors([]response.Response{response.New(InputFileErrorResponse{}, "404", "Not found"), response.New(InputFileErrorResponse{}, "500", "Server error")}), endpoint.WithProduce([]mime.MIME{mime.JSON})),
		endpoint.New(endpoint.PATCH, "/input-files/{id}", endpoint.WithTags("input-files"), endpoint.WithSummary("Update input file metadata"), endpoint.WithDescription("Updates metadata only; the RustFS/S3 object is not renamed or moved."), endpoint.WithBody(UpdateInputFileRequest{}), endpoint.WithSuccessfulReturns([]response.Response{response.New(InputFileResponse{}, "200", "OK")}), endpoint.WithErrors([]response.Response{response.New(InputFileErrorResponse{}, "400", "Invalid request"), response.New(InputFileErrorResponse{}, "404", "Not found"), response.New(InputFileErrorResponse{}, "500", "Server error")}), endpoint.WithConsume([]mime.MIME{mime.JSON}), endpoint.WithProduce([]mime.MIME{mime.JSON})),
		endpoint.New(endpoint.DELETE, "/input-files/{id}", endpoint.WithTags("input-files"), endpoint.WithSummary("Delete input file metadata"), endpoint.WithDescription("Deletes only the database metadata row. The RustFS/S3 object is kept."), endpoint.WithSuccessfulReturns([]response.Response{response.New(map[string]bool{}, "200", "Deleted")}), endpoint.WithErrors([]response.Response{response.New(InputFileErrorResponse{}, "404", "Not found"), response.New(InputFileErrorResponse{}, "500", "Server error")}), endpoint.WithProduce([]mime.MIME{mime.JSON})),
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
	nodeSchema, _ := schemas["httpapi.ValidatePipelineNode"].(map[string]any)
	properties, _ := nodeSchema["properties"].(map[string]any)
	properties["config"] = map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"description":          "Node-specific configuration. Shape depends on the node type.",
	}
	properties["inputs"] = map[string]any{
		"description": "Input node references. Most nodes use an array of node IDs; joins use an object with left and right node IDs.",
		"oneOf": []any{
			map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			map[string]any{"type": "object", "additionalProperties": true},
		},
	}
	delete(schemas, "httpapi.ValidatePipelineNode.config")
	patchUploadInputFilePath(spec)

	patched, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return doc
	}
	return patched
}

func patchUploadInputFilePath(spec map[string]any) {
	paths, _ := spec["paths"].(map[string]any)
	pathItem, _ := paths["/input-files/upload"].(map[string]any)
	post, _ := pathItem["post"].(map[string]any)
	post["requestBody"] = map[string]any{
		"required": true,
		"content": map[string]any{
			"multipart/form-data": map[string]any{
				"schema": map[string]any{
					"type":     "object",
					"required": []any{"file", "format"},
					"properties": map[string]any{
						"file":       map[string]any{"type": "string", "format": "binary"},
						"name":       map[string]any{"type": "string"},
						"object_key": map[string]any{"type": "string"},
						"format":     map[string]any{"type": "string", "enum": []any{"csv", "parquet"}},
						"metadata":   map[string]any{"type": "string", "description": "JSON object encoded as a string"},
					},
				},
			},
		},
	}
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
