package httpapi

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"go-transformer/internal/inputfiles"
)

func RegisterInputFileRoutes(h *server.Hertz, svc *inputfiles.Service) {
	h.POST("/input-files/upload", uploadInputFile(svc))
	h.POST("/input-files/register", registerInputFile(svc))
	h.GET("/input-files", listInputFiles(svc))
	h.GET("/input-files/:id", getInputFile(svc))
	h.GET("/input-files/:id/download", downloadInputFile(svc))
	h.PATCH("/input-files/:id", updateInputFile(svc))
	h.DELETE("/input-files/:id", deleteInputFile(svc))
}

func uploadInputFile(svc *inputfiles.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		fh, err := c.FormFile("file")
		if err != nil {
			writeError(c, consts.StatusBadRequest, "file is required")
			return
		}
		file, err := fh.Open()
		if err != nil {
			writeError(c, consts.StatusBadRequest, err.Error())
			return
		}
		defer file.Close()
		metadata, err := inputfiles.DecodeMetadata(c.PostForm("metadata"))
		if err != nil {
			writeError(c, consts.StatusBadRequest, err.Error())
			return
		}
		name := c.PostForm("name")
		if strings.TrimSpace(name) == "" {
			name = fh.Filename
		}
		created, err := svc.Upload(ctx, name, c.PostForm("object_key"), c.PostForm("format"), metadata, file, fh.Size, fh.Header.Get("Content-Type"))
		writeServiceResult(c, created, err, consts.StatusCreated)
	}
}

func registerInputFile(svc *inputfiles.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var req inputfiles.RegisterRequest
		if err := json.Unmarshal(c.Request.Body(), &req); err != nil {
			writeError(c, consts.StatusBadRequest, "invalid JSON body")
			return
		}
		created, err := svc.Register(ctx, req)
		writeServiceResult(c, created, err, consts.StatusCreated)
	}
}

func listInputFiles(svc *inputfiles.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		limit, _ := strconv.Atoi(c.Query("limit"))
		offset, _ := strconv.Atoi(c.Query("offset"))
		files, err := svc.List(ctx, inputfiles.ListOptions{Limit: limit, Offset: offset, Bucket: c.Query("bucket"), Format: c.Query("format"), Name: c.Query("name")})
		writeServiceResult(c, map[string]any{"files": files}, err, consts.StatusOK)
	}
}

func getInputFile(svc *inputfiles.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		file, err := svc.Get(ctx, id)
		writeServiceResult(c, file, err, consts.StatusOK)
	}
}

func downloadInputFile(svc *inputfiles.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		url, err := svc.PresignedDownload(ctx, id, 15*time.Minute)
		writeServiceResult(c, map[string]string{"url": url}, err, consts.StatusOK)
	}
}

func updateInputFile(svc *inputfiles.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		var req inputfiles.UpdateRequest
		if err := json.Unmarshal(c.Request.Body(), &req); err != nil {
			writeError(c, consts.StatusBadRequest, "invalid JSON body")
			return
		}
		updated, err := svc.Update(ctx, id, req)
		writeServiceResult(c, updated, err, consts.StatusOK)
	}
}

func deleteInputFile(svc *inputfiles.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		err := svc.Delete(ctx, id)
		writeServiceResult(c, map[string]bool{"deleted": true}, err, consts.StatusOK)
	}
}
