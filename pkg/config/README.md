# Configuration Management

遵循 **12-Factor App** 理念构建的配置管理中心，基于 `spf13/viper` 实现。

## ✨ 核心特性

* **配置与代码解耦**：所有数据库 DSN、Redis 寻址、服务器端口等易变参数全部从硬编码中剥离，集中由 `config.yaml` 管理。
* **环境变量覆写 (ENV Override)**：默认开启 `viper.AutomaticEnv()`。在本地开发时读取 YAML 文件，而在远端 Kubernetes 部署时，可以通过配置 ConfigMap 注入同名环境变量，实现无缝切换，无需重新编译二进制文件。
* **强类型映射**：利用 `mapstructure` 标签，将零散的配置项反序列化为规整的 Go 结构体，为业务逻辑提供强类型的配置支持。