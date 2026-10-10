package router

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testIndexPage = `<html><head><title>New API</title><meta name="title" content="New API" /></head><body></body></html>`

func serveTestIndexPage(t *testing.T, name string) string {
	t.Helper()
	oldName := common.SystemName
	common.SystemName = name
	t.Cleanup(func() {
		common.SystemName = oldName
		renderedIndexPage = atomic.Value{}
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	serveIndexPage(c, []byte(testIndexPage))

	require.Equal(t, http.StatusOK, w.Code)
	return w.Body.String()
}

func TestServeIndexPageRendersConfiguredSystemName(t *testing.T) {
	body := serveTestIndexPage(t, "ShadeSheep")
	assert.Contains(t, body, "<title>ShadeSheep</title>")
	assert.Contains(t, body, `<meta name="title" content="ShadeSheep" />`)
	assert.NotContains(t, body, "New API")
}

func TestServeIndexPageEscapesSystemName(t *testing.T) {
	body := serveTestIndexPage(t, `A<B>"C"`)
	assert.Contains(t, body, "<title>A&lt;B&gt;&#34;C&#34;</title>")
	assert.Contains(t, body, `<meta name="title" content="A&lt;B&gt;&#34;C&#34;" />`)
}

func TestServeIndexPageRerendersWhenSystemNameChanges(t *testing.T) {
	serveTestIndexPage(t, "First")
	body := serveTestIndexPage(t, "Second")
	assert.Contains(t, body, "<title>Second</title>")
}
