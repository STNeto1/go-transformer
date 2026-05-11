package httpapi

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"go-transformer/internal/inputfiles"
	"go-transformer/internal/pipelines"
)

type Config struct {
	Addr string
}

type Deps struct {
	InputFiles *inputfiles.Service
	Pipelines  *pipelines.Service
}

func NewServer(cfg Config, deps Deps) *server.Hertz {
	h := server.Default(server.WithHostPorts(cfg.Addr))
	h.Use(Middlewares()...)

	RegisterHealthRoutes(h)
	RegisterPipelineRoutes(h, deps.Pipelines)
	RegisterInputFileRoutes(h, deps.InputFiles)
	RegisterOpenAPIRoute(h, cfg.Addr)

	return h
}

func RegisterHealthRoutes(h *server.Hertz) {
	h.GET("/healthz", func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, HealthResponse{Status: "ok"})
	})
}
