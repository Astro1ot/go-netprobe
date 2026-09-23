package api

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNullIsNotAnAllTargetsRequest(t *testing.T) {
	handler := HTTP(testEngine(t))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("POST", "/checks", strings.NewReader("null")))
	if response.Code != 400 {
		t.Fatalf("null must be rejected, got %d", response.Code)
	}
}
