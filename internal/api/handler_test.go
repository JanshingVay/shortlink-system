package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRespondError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	respondError(c, http.StatusNotFound, "短链不存在")

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("状态码错误: got=%d", recorder.Code)
	}
	if recorder.Body.String() != `{"code":404,"error":"短链不存在"}` {
		t.Fatalf("错误响应格式错误: %s", recorder.Body.String())
	}
}

func TestCreateReqValidationShape(t *testing.T) {
	if (CreateReq{}).LongURL != "" {
		t.Fatal("CreateReq 默认值异常")
	}
}

func BenchmarkRespondError(b *testing.B) {
	gin.SetMode(gin.TestMode)
	for i := 0; i < b.N; i++ {
		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		respondError(c, http.StatusNotFound, "not found")
	}
}
