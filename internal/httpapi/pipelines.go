package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"go-transformer/internal/pipeline"
	"go-transformer/internal/pipelines"
)

func RegisterPipelineRoutes(h *server.Hertz, svc *pipelines.Service) {
	h.GET("/pipelines", listPipelines(svc))
	h.POST("/pipelines", createPipeline(svc))
	h.POST("/pipelines/validate", validatePipeline)
	h.GET("/pipelines/:id", getPipeline(svc))
	h.PATCH("/pipelines/:id", updatePipeline(svc))
	h.DELETE("/pipelines/:id", deletePipeline(svc))
	h.POST("/pipelines/:id/activate", activatePipeline(svc))
}

func listPipelines(svc *pipelines.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		offset, _ := strconv.Atoi(c.Query("offset"))
		items, err := svc.List(ctx, pipelines.ListOptions{Limit: limit, Offset: offset, Status: c.Query("status"), Name: c.Query("name")})
		writeServiceResult(c, map[string]any{"pipelines": items}, err, consts.StatusOK)
	}
}

func createPipeline(svc *pipelines.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req pipelines.SaveRequest
		if err := json.Unmarshal(c.Request.Body(), &req); err != nil {
			writeError(c, consts.StatusBadRequest, "invalid JSON body")
			return
		}
		created, err := svc.Create(ctx, req)
		writeServiceResult(c, created, err, consts.StatusCreated)
	}
}

func getPipeline(svc *pipelines.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		item, err := svc.Get(ctx, id)
		writeServiceResult(c, item, err, consts.StatusOK)
	}
}

func updatePipeline(svc *pipelines.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var req pipelines.SaveRequest
		if err := json.Unmarshal(c.Request.Body(), &req); err != nil {
			writeError(c, consts.StatusBadRequest, "invalid JSON body")
			return
		}
		updated, err := svc.Update(ctx, id, req)
		writeServiceResult(c, updated, err, consts.StatusOK)
	}
}

func deletePipeline(svc *pipelines.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		err := svc.Delete(ctx, id)
		writeServiceResult(c, map[string]bool{"deleted": true}, err, consts.StatusOK)
	}
}

func activatePipeline(svc *pipelines.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		activated, err := svc.Activate(ctx, id)
		if err != nil {
			var validationErr *pipeline.ValidationError
			if errors.As(err, &validationErr) {
				c.JSON(consts.StatusBadRequest, map[string]any{"valid": false, "error": err.Error(), "issues": validationErr.Issues})
				return
			}
			writeServiceResult(c, nil, err, consts.StatusOK)
			return
		}
		c.JSON(consts.StatusOK, activated)
	}
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
