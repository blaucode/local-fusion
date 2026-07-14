package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"auth"
)

func BenchmarkGreetingHandler(b *testing.B) {
	user := &auth.User{ID: "bench", Name: "BenchmarkUser"}
	req := httptest.NewRequest(http.MethodGet, "/greeting", nil).WithContext(auth.NewContextWithUser(nil, user))

	rr := httptest.NewRecorder()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		GreetingHandler(rr, req)
		rr.Body.Reset()
	}
}