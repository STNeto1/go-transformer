package httpapi

import (
	"errors"
	"strconv"
	"strings"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"go-transformer/internal/inputfiles"
)

func parseID(c *app.RequestContext) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(c, consts.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func writeServiceResult(c *app.RequestContext, payload any, err error, successStatus int) {
	if err != nil {
		status := consts.StatusInternalServerError
		if errors.Is(err, inputfiles.ErrNotFound) {
			status = consts.StatusNotFound
		} else if strings.Contains(err.Error(), "required") || strings.Contains(err.Error(), "must be") || strings.Contains(err.Error(), "invalid") {
			status = consts.StatusBadRequest
		} else if strings.Contains(err.Error(), "already exists") {
			status = consts.StatusConflict
		}
		writeError(c, status, err.Error())
		return
	}
	c.JSON(successStatus, payload)
}

func writeError(c *app.RequestContext, status int, message string) {
	c.JSON(status, map[string]string{"error": message})
}
