package api

import (
	"context"
	"net/http"
	"shortlink-system/internal/service"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	svc *service.ShortLinkService
}

func NewHandler(svc *service.ShortLinkService) *Handler {
	return &Handler{svc: svc}
}

// CreateReq 接收长链的 JSON 请求体
type CreateReq struct {
	LongURL string `json:"long_url" binding:"required,url"` // Gin 自带 URL 格式校验
}

// GenerateShortLink 处理长转短 API
func (h *Handler) GenerateShortLink(c *gin.Context) {
	var req CreateReq
	// 校验 JSON 格式
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求参数或 URL 格式错误"})
		return
	}

	// 调用业务逻辑
	shortURL, err := h.svc.Create(context.Background(), req.LongURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "系统内部错误"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":      200,
		"short_url": shortURL,
	})
}

// Redirect 短链重定向 API
func (h *Handler) Redirect(c *gin.Context) {
	// 获取 URL 路径中的短码，例如 GET /aZ3kP9 中的 "aZ3kP9"
	shortCode := c.Param("code")

	longURL, err := h.svc.Redirect(c.Request.Context(), shortCode)
	if err != nil {
		// 如果被布隆过滤器拦截，返回 404
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	// HTTP 302 临时重定向 (面试考点：为什么不用 301？因为 301 会被浏览器永久缓存，我们就统计不到点击量了)
	c.Redirect(http.StatusFound, longURL)
}

// RegisterRoutes 注册所有的 API 路由
func (h *Handler) RegisterRoutes(router *gin.Engine) {
	router.POST("/api/v1/shorten", h.GenerateShortLink) // 生成接口
	router.GET("/:code", h.Redirect)                    // 重定向接口
}
