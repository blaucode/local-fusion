package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"example.com/project/auth"
)

func BenchmarkGreetingHandler_HappyPath(b *testing.B) {
	user := &auth.User{ID: "bench", Name: "BenchmarkUser"}
	ctx := auth.NewContext(context.Background(), user)

	req := httptest.NewRequest(http.MethodGet, "/greeting", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr.Body.Reset()
		GreetingHandler(rr, req)
	}
}