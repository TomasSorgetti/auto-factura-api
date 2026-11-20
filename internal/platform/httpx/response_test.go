package httpx

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestJSONAndList(t *testing.T) {
	t.Parallel()

	t.Run("json", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		if err := JSON(rec, 201, map[string]string{"a": "b"}); err != nil {
			t.Fatal(err)
		}
		if rec.Code != 201 || strings.TrimSpace(rec.Body.String()) != `{"data":{"a":"b"}}` {
			t.Fatalf("got %d %q", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
			t.Fatalf("content type %q", ct)
		}
	})
	t.Run("list nil", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		if err := List[string](rec, nil, ""); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != `{"data":[],"nextCursor":null}` {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("list cursor", func(t *testing.T) {
		t.Parallel()
		rec := httptest.NewRecorder()
		if err := List(rec, []int{1, 2}, "abc"); err != nil {
			t.Fatal(err)
		}
		if got := strings.TrimSpace(rec.Body.String()); got != `{"data":[1,2],"nextCursor":"abc"}` {
			t.Fatalf("got %q", got)
		}
	})
}
