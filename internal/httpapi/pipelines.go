package httpapi

import (
	"context"
	"errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"go-transformer/internal/pipeline"
)

func RegisterPipelineRoutes(h *server.Hertz) {
	h.POST("/pipelines/validate", validatePipeline)
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

	c.JSON(consts.StatusOK, ValidatePipelineSuccessResponse{
		Valid:      true,
		PipelineID: spec.PipelineID,
		Version:    spec.Version,
		Nodes:      len(spec.Nodes),
		Sinks:      len(spec.Sinks),
	})
}
