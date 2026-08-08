package api

import (
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

// respondError 统一错误响应结构，保证成功/失败都带有 code 字段，便于客户端统一处理。
func respondError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"code": status, "error": msg})
}

// GenerateShortLink 处理长转短 API
func (h *Handler) GenerateShortLink(c *gin.Context) {
	var req CreateReq
	// 校验 JSON 格式
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "无效的请求参数或 URL 格式错误")
		return
	}

	// 使用请求上下文，使客户端断开 / 超时能正确传播到下游 DB、Redis 调用
	shortURL, err := h.svc.Create(c.Request.Context(), req.LongURL)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "系统内部错误")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":      200,
		"short_url": shortURL,
	})
}

func (h *Handler) Decode(c *gin.Context) {
	shortCode := c.Param("code")

	longURL, err := h.svc.Redirect(c.Request.Context(), shortCode)
	if err != nil {
		respondError(c, http.StatusNotFound, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":       200,
		"short_code": shortCode,
		"long_url":   longURL,
	})
}

// Redirect 短链重定向 API
func (h *Handler) Redirect(c *gin.Context) {
	// 获取 URL 路径中的短码，例如 GET /aZ3kP9 中的 "aZ3kP9"
	shortCode := c.Param("code")

	longURL, err := h.svc.Redirect(c.Request.Context(), shortCode)
	if err != nil {
		// 如果被布隆过滤器拦截，返回 404
		respondError(c, http.StatusNotFound, err.Error())
		return
	}

	// HTTP 302 临时重定向 (面试考点：为什么不用 301？因为 301 会被浏览器永久缓存，我们就统计不到点击量了)
	c.Redirect(http.StatusFound, longURL)
}

// RegisterRoutes 注册所有的 API 路由
func (h *Handler) RegisterRoutes(router *gin.Engine) {
	router.POST("/api/v1/encode", h.GenerateShortLink)
	router.GET("/api/v1/decode/:code", h.Decode)
	router.GET("/:code", h.Redirect)
}
