# 短链接系统 API 层核心依赖知识点总结
## 一、核心依赖库
| 库名称                | 类型       | 核心作用                                                                 | 面试高频考点                                                                 |
|-----------------------|------------|--------------------------------------------------------------------------|------------------------------------------------------------------------------|
| `net/http`            | Go 标准库  | HTTP 协议基础能力（状态码、请求/响应处理、Context 透传）                 | 1. HTTP 状态码选型（301/302 区别、400/404/500 适用场景）<br>2. Context 生命周期与传参<br>3. HTTP 重定向原理 |
| `context`             | Go 标准库  | 上下文管理（超时、取消、请求链路透传）                                   | 1. Context 核心设计理念（父子上下文、value 传参规范）<br>2. 业务层透传 Context 的必要性<br>3. 避免 Context 泄漏（如 goroutine 中未正确传递） |
| `github.com/gin-gonic/gin` | 第三方框架 | HTTP 路由、参数绑定、响应封装、中间件体系                               | 1. Gin 核心特性（高性能原因：基于 httprouter 实现的 radix 树路由）<br>2. 参数绑定方式（ShouldBindJSON/ShouldBindUri 区别）<br>3. Gin 中间件开发与执行顺序<br>4. Gin.H 底层实现（本质是 map[string]interface{}）<br>5. 自定义响应结构体替代 Gin.H 的最佳实践 |

## 二、高频考点拆解
### 1. net/http 标准库
#### （1）HTTP 状态码选型（面试必问）
| 状态码 | 名称       | 本系统场景                | 核心考点                                                                 |
|--------|------------|---------------------------|--------------------------------------------------------------------------|
| 301    | Moved Permanently | 不使用（短链重定向）| 永久重定向，浏览器会缓存跳转结果，导致短链点击量无法统计                 |
| 302/307 | Found/Temporary Redirect | 短链重定向（用 302/307） | 临时重定向，每次跳转都会请求服务器，可正常统计点击量；307 更严格遵循 HTTP 规范，不允许方法转换 |
| 400    | Bad Request | 参数校验失败              | 客户端请求参数错误（如 JSON 格式错误、URL 格式不合法）                   |
| 404    | Not Found   | 短码不存在/布隆过滤拦截   | 资源不存在，需区分“业务层面不存在”和“服务层面找不到”                     |
| 500    | Internal Server Error | 业务层执行失败            | 服务端内部错误，生产环境需隐藏具体错误信息，避免泄露系统细节             |

#### （2）Context 透传
- 核心作用：将 HTTP 请求的上下文（超时、取消信号、traceID 等）透传给业务层，保证链路可追踪、资源可释放；
- 正确用法：`c.Request.Context()`（基于 HTTP 请求的上下文）优于 `context.Background()`（空上下文，无超时/取消能力）；
- 面试陷阱：
  - 错误：在 Handler 中直接用 `context.Background()`，导致业务层无法感知 HTTP 请求取消；
  - 正确：重定向接口中使用 `c.Request.Context()`，生成接口建议替换为 `c.Request.Context()`（而非 `context.Background()`）。

### 2. gin-gonic/gin 框架
#### （1）参数绑定
- `ShouldBindJSON(&req)`：自动解析 HTTP 请求体的 JSON 数据并绑定到结构体，结合 `binding` tag 做参数校验；
- 核心 tag：`required`（必传）、`url`（URL 格式校验），底层依赖 `go-playground/validator` 做参数校验；
- 面试考点：`ShouldBindJSON` vs `MustBindJSON`：
  - `ShouldBindJSON`：校验失败返回 error，需手动处理；
  - `MustBindJSON`：校验失败直接返回 400 响应，灵活性低，生产环境推荐 `ShouldBindJSON`。

#### （2）路由设计
- 动态路由：`router.GET("/:code", h.Redirect)` 中 `:code` 是路径参数，通过 `c.Param("code")` 获取；
- 路由分组：本系统未体现，但面试高频——如 `/api/v1` 分组可添加鉴权中间件：
  ```go
  v1 := router.Group("/api/v1")
  v1.Use(middleware.Auth()) // 鉴权中间件
  v1.POST("/shorten", h.GenerateShortLink)
  ```
- 高性能原因：Gin 基于 radix 树实现路由匹配，时间复杂度 O(1)，优于传统的正则匹配路由。

#### （3）响应处理
- JSON 响应：`c.JSON(statusCode, gin.H{})`，`gin.H` 是 `map[string]interface{}` 的别名，简洁但不规范；
- 生产优化（面试加分）：自定义响应结构体替代 `gin.H`，保证响应格式统一：
  ```go
  type Response struct {
      Code int         `json:"code"`
      Msg  string      `json:"msg"`
      Data interface{} `json:"data,omitempty"`
  }
  c.JSON(http.StatusOK, Response{Code:200, Msg:"success", Data: shortURL})
  ```
- 重定向：`c.Redirect(http.StatusFound, longURL)`，302 对应 `http.StatusFound`（Go 1.7+ 推荐），而非直接写 302。

#### （4）中间件
- 核心场景（面试高频）：
  - 限流：短链生成接口添加限流中间件，防止恶意请求；
  - 日志：记录请求 URL、耗时、状态码；
  - 跨域：处理前端跨域请求（`Access-Control-Allow-Origin`）；
- 执行顺序：Gin 中间件遵循“先加先执行”（洋葱模型），例如：
  ```go
  router.Use(middleware.Log(), middleware.Recovery()) // 先执行 Log，再执行 Recovery
  ```

### 3. 整体架构设计考点
#### （1）分层设计
- API 层职责：仅处理 HTTP 交互（参数校验、路由、响应），不包含业务逻辑；
- 依赖注入：`Handler` 结构体注入 `service.ShortLinkService`，实现层与层之间解耦；
- 面试考点：为什么 API 层不直接写业务逻辑？
  - 复用性：service 层可被 CLI、GRPC 等多端复用；
  - 可测试性：service 层可单独做单元测试，无需启动 HTTP 服务；
  - 可维护性：需求变更时，修改范围可控（如改参数校验只动 API 层，改短链生成规则只动 service 层）。

#### （2）错误处理
- 本系统优化点（面试加分）：
  1. 区分业务错误和系统错误：布隆过滤器拦截返回 404（业务错误），service 层未知错误返回 500（系统错误）；
  2. 生产环境不暴露原始错误：500 错误返回“系统内部错误”，而非 `err.Error()`，避免泄露系统细节；
  3. 错误日志：补充日志打印，方便排查问题（如 `log.Printf("生成短链失败：%v", err)`）。

## 三、面试常问扩展问题
1. **Gin 对比其他框架（如 Echo、Beego）的优势？**
   - 性能：Gin 是高性能 HTTP 框架，基于 radix 树路由，QPS 高于 Beego；
   - 轻量：核心代码简洁，无过多封装，学习成本低；
   - 生态：中间件丰富，社区活跃。
2. **短链重定向为什么不用 301？**
   - 301 是永久重定向，浏览器会缓存跳转结果，后续请求直接由浏览器跳转，服务器无法统计点击量；
   - 302/307 是临时重定向，每次跳转都需请求服务器，可正常统计点击量、做权限校验等。
3. **Context 在本系统中的作用？**
   - 传递请求生命周期：当 HTTP 请求被取消（如客户端关闭连接），Context 会触发取消，业务层可及时停止耗时操作（如数据库查询）；
   - 链路追踪：可在 Context 中传递 traceID，串联 API 层和 service 层的日志；
   - 超时控制：可通过 `context.WithTimeout` 给业务层设置超时（如生成短链最多耗时 1s）。
4. **参数校验除了 Gin 的 binding tag，还有哪些方式？**
   - 手动校验：针对复杂规则（如 URL 域名白名单）；
   - 自定义 validator 校验规则：扩展 `binding` tag，如自定义 `domain` tag 校验 URL 域名；
   - 第三方校验库：如 `go-playground/validator` 直接使用，支持更丰富的校验规则。