package router

import (
	"bytes"
	"embed"
	"html"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/gin-contrib/gzip"
	"github.com/gin-gonic/gin"
)

// WebAssets holds the embedded dashboard frontend assets.
type WebAssets struct {
	BuildFS   embed.FS
	IndexPage []byte
}

// indexPageRender is the embedded index.html with the configured system name
// rendered into the title, cached for the name it was built from.
type indexPageRender struct {
	name string
	body []byte
}

var renderedIndexPage atomic.Value // stores indexPageRender

// serveIndexPage writes the embedded index.html with the build-time
// "New API" title replaced by the configured system name, so the browser tab
// shows the site branding immediately instead of flashing "New API" until the
// frontend status request completes. The render is redone only when the
// system name changes.
func serveIndexPage(c *gin.Context, raw []byte) {
	name := common.SystemName
	if cached, ok := renderedIndexPage.Load().(indexPageRender); ok && cached.name == name {
		c.Data(http.StatusOK, "text/html; charset=utf-8", cached.body)
		return
	}
	escaped := html.EscapeString(name)
	body := bytes.Replace(raw, []byte("<title>New API</title>"), []byte("<title>"+escaped+"</title>"), 1)
	body = bytes.Replace(body, []byte(`<meta name="title" content="New API" />`), []byte(`<meta name="title" content="`+escaped+`" />`), 1)
	renderedIndexPage.Store(indexPageRender{name: name, body: body})
	c.Data(http.StatusOK, "text/html; charset=utf-8", body)
}

func SetWebRouter(router *gin.Engine, assets WebAssets, pluginDispatcher gin.HandlerFunc) {
	frontendFS := common.EmbedFolder(assets.BuildFS, "web/dist")
	compress := gzip.Gzip(gzip.DefaultCompression)

	router.NoRoute(
		pluginDispatcher,
		middleware.RouteTag("web"),
		func(c *gin.Context) {
			// ServeFrontendFiles sends build files already compressed.
			if !frontendFS.Exists("/", c.Request.URL.Path) {
				compress(c)
			}
		},
		middleware.AccessTokenAudit(),
		middleware.GlobalWebRateLimit(frontendFS),
		middleware.Cache(),
		middleware.ServeFrontendFiles(frontendFS),
		func(c *gin.Context) {
			if strings.HasPrefix(c.Request.RequestURI, "/v1") || strings.HasPrefix(c.Request.RequestURI, "/api") || strings.HasPrefix(c.Request.RequestURI, "/assets") {
				controller.RelayNotFound(c)
				return
			}
			c.Header("Cache-Control", "no-cache")
			serveIndexPage(c, assets.IndexPage)
		},
	)
}
