package viewkit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The viewkit is served from every song mount (including atp's), so its public
// names are a compatibility surface: the JavaScript must keep exporting them.
func TestJSCoversTheSharedSurface(t *testing.T) {
	js := JS()
	for _, name := range []string{
		"escapeHTML", "domEventName", "domSafeId", "actionId", "formatCount",
		"timeAgo", "niceDate", "routeOf", "hrefFor", "itemHref", "buildQuery",
		"quoteValue", "aggregate", "topEntries", "seriesOfDay", "project",
		"geoPoints", "cluster", "severityRank", "triage", "indexByID",
		"referenceEnds", "annotations", "annotationSections", "timelineGroups",
	} {
		if !strings.Contains(js, name+": "+name) {
			t.Errorf("viewkit.js does not export %q", name)
		}
	}
	if !strings.Contains(js, "root.SongView = api") {
		t.Error("viewkit.js must publish window.SongView")
	}
}

func TestHandlerServesTheLibrary(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler()(rec, httptest.NewRequest(http.MethodGet, AssetPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != ContentType {
		t.Errorf("content type = %q, want %q", got, ContentType)
	}
	if !strings.Contains(rec.Body.String(), "SongView") {
		t.Error("body does not look like the viewkit")
	}
}

func TestHandlerRejectsWrites(t *testing.T) {
	rec := httptest.NewRecorder()
	Handler()(rec, httptest.NewRequest(http.MethodPost, AssetPath, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
