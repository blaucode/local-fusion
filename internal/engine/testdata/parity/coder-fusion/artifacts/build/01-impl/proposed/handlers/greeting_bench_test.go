package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/project/auth"
)

func BenchmarkGreetingHandler(b *testing.B) {
	user := &auth.User{ID: "bench", Name: "BenchmarkUser"}
	req := httptest.NewRequest(http.MethodGet, "/greeting", nil)
	ctx := auth.NewContextWithUser(req.Context(), user)
	req = req.WithContext(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		GreetingHandler(rr, req)
		if rr.Code != http.StatusOK {
			b.Fatalf("unexpected status %d", rr.Code)
		}
	}
}