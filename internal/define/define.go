package define

import "strings"

// AppID 用于文件系统/配置目录等“标识用途”
const AppID = "willchat"

// SingleInstanceUniqueID 单实例唯一标识符（反向域名格式）
const SingleInstanceUniqueID = "com.sesame.willchat"

// AppDisplayName 用于 UI 展示
const AppDisplayName = "WillChat"

// DefaultSQLiteFileName 默认 SQLite 数据库文件名
const DefaultSQLiteFileName = "data.sqlite"

// Env / ServerURL 的默认值由编译 tag 决定（见 env_dev.go / env_prod.go）

// IsDev 是否为开发环境
func IsDev() bool {
	return Env == "development"
}

// IsProd 是否为生产环境
func IsProd() bool {
	return Env == "production"
}

// ChatWikiOpenAPIEndpoint returns the ChatWiki OpenAPI base URL (ServerURL as configured; no suffix appended).
// Initial write to providers table uses this (see migrations: create_models_table, add_chatwiki_provider).
// Env defaults (env_dev.go / env_prod.go) should include /openapi so the stored api_endpoint is correct.
func ChatWikiOpenAPIEndpoint() string {
	return strings.TrimSuffix(strings.TrimSpace(ServerURL), "/")
}
