package opencode

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestCatalogOnlyConfiguredModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("directory") != "/tmp/project" {
			t.Error("catalog missing directory scope")
		}
		switch r.URL.Path {
		case "/config/providers":
			fmt.Fprint(w, `{"providers":[{"id":"configured","models":{"selected":{}}}]}`)
		case "/agent":
			fmt.Fprint(w, `[{"name":"build"}]`)
		case "/provider":
			t.Error("must not read the global model catalog")
			fmt.Fprint(w, `{"all":[{"id":"unconfigured","models":{"unavailable":{}}}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	models, _, err := New(server.URL, true).Catalog(t.Context(), "/tmp/project")
	if err != nil || !reflect.DeepEqual(models, []string{"configured/selected"}) {
		t.Fatalf("models=%v error=%v", models, err)
	}
}
