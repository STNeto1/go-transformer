package httpapi

import (
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/hertz-contrib/cors"
	"github.com/hertz-contrib/requestid"
	"github.com/hertz-contrib/secure"
)

func Middlewares() []app.HandlerFunc {
	return []app.HandlerFunc{
		requestid.New(),
		cors.New(cors.Config{
			AllowOrigins:  []string{"http://localhost:5173", "http://localhost:3000"},
			AllowMethods:  []string{consts.MethodGet, consts.MethodPost, consts.MethodPatch, consts.MethodDelete, consts.MethodOptions},
			AllowHeaders:  []string{"Origin", "Content-Type", "Authorization", "X-Request-ID"},
			ExposeHeaders: []string{"X-Request-ID"},
			MaxAge:        10 * time.Minute,
		}),
		secure.New(
			secure.WithSSLRedirect(false),
			secure.WithContentTypeNosniff(true),
			secure.WithFrameDeny(true),
			secure.WithReferrerPolicy("no-referrer"),
			secure.WithIENoOpen(true),
		),
	}
}
