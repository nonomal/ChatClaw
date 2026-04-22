package providers

import (
	"context"
	"encoding/json"
	"time"

	"chatclaw/internal/sqlite"

	"github.com/uptrace/bun"
)

// Provider 渚涘簲鍟?DTO锛堟毚闇茬粰鍓嶇锛?
type Provider struct {
	ID          int64     `json:"id"`
	ProviderID  string    `json:"provider_id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Icon        string    `json:"icon"`
	IsBuiltin   bool      `json:"is_builtin"`
	IsFree      bool      `json:"is_free"`
	Enabled     bool      `json:"enabled"`
	SortOrder   int       `json:"sort_order"`
	APIEndpoint string    `json:"api_endpoint"`
	APIKey      string    `json:"api_key"`
	ExtraConfig string    `json:"extra_config"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Model 妯″瀷 DTO锛堟毚闇茬粰鍓嶇锛?
type Model struct {
	ID              int64     `json:"id"`
	ProviderID      string    `json:"provider_id"`
	ModelID         string    `json:"model_id"`
	Name            string    `json:"name"`
	ModelSupplier   string    `json:"model_supplier"`
	UniModelName    string    `json:"uni_model_name"`
	Type            string    `json:"type"`         // llm, embedding, rerank
	Capabilities    []string  `json:"capabilities"` // 鏀寔鐨勮緭鍏ョ被鍨? text, image, audio, video, file
	DefaultUseModel string    `json:"default_use_model"`
	IsBuiltin       bool      `json:"is_builtin"`
	Enabled         bool      `json:"enabled"`
	SortOrder       int       `json:"sort_order"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ModelGroup 妯″瀷鍒嗙粍锛堟寜绫诲瀷鍒嗙粍锛?
type ModelGroup struct {
	Type   string  `json:"type"`
	Models []Model `json:"models"`
}

// ProviderWithModels 渚涘簲鍟嗗強鍏舵ā鍨?
type ProviderWithModels struct {
	Provider    Provider     `json:"provider"`
	ModelGroups []ModelGroup `json:"model_groups"`
}

// UpdateProviderInput 鏇存柊渚涘簲鍟嗙殑杈撳叆鍙傛暟
type UpdateProviderInput struct {
	Enabled     *bool   `json:"enabled"`
	APIKey      *string `json:"api_key"`
	APIEndpoint *string `json:"api_endpoint"`
	ExtraConfig *string `json:"extra_config"`
}

// CreateModelInput 鍒涘缓妯″瀷鐨勮緭鍏ュ弬鏁?
type CreateModelInput struct {
	ModelID      string   `json:"model_id"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`         // llm, embedding, rerank
	Capabilities []string `json:"capabilities"` // 鏀寔鐨勮緭鍏ョ被鍨? text, image, audio, video, file
}

// UpdateModelInput 鏇存柊妯″瀷鐨勮緭鍏ュ弬鏁?
// 娉ㄦ剰锛歮odel_id 鍜?type 鍒涘缓鍚庝笉鍏佽淇敼
type UpdateModelInput struct {
	Name         *string  `json:"name"`
	Enabled      *bool    `json:"enabled"`
	Capabilities []string `json:"capabilities"` // 鏀寔鐨勮緭鍏ョ被鍨? text, image, audio, video, file
}

// providerModel 鏁版嵁搴撴ā鍨?
type providerModel struct {
	bun.BaseModel `bun:"table:providers,alias:p"`

	ID          int64     `bun:"id,pk,autoincrement"`
	ProviderID  string    `bun:"provider_id,notnull"`
	Name        string    `bun:"name,notnull"`
	Type        string    `bun:"type,notnull"`
	Icon        string    `bun:"icon,notnull"`
	IsBuiltin   bool      `bun:"is_builtin,notnull"`
	IsFree      bool      `bun:"is_free,notnull"`
	Enabled     bool      `bun:"enabled,notnull"`
	SortOrder   int       `bun:"sort_order,notnull"`
	APIEndpoint string    `bun:"api_endpoint,notnull"`
	APIKey      string    `bun:"api_key,notnull"`
	ExtraConfig string    `bun:"extra_config,notnull"`
	CreatedAt   time.Time `bun:"created_at,notnull"`
	UpdatedAt   time.Time `bun:"updated_at,notnull"`
}

// BeforeInsert 鍦?INSERT 鏃惰嚜鍔ㄨ缃?created_at 鍜?updated_at锛堝瓧绗︿覆鏍煎紡锛?
var _ bun.BeforeInsertHook = (*providerModel)(nil)

func (*providerModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := sqlite.NowUTC()
	query.Value("created_at", "?", now)
	query.Value("updated_at", "?", now)
	return nil
}

// BeforeUpdate 鍦?UPDATE 鏃惰嚜鍔ㄨ缃?updated_at锛堝瓧绗︿覆鏍煎紡锛?
var _ bun.BeforeUpdateHook = (*providerModel)(nil)

func (*providerModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	query.Set("updated_at = ?", sqlite.NowUTC())
	return nil
}

func (m *providerModel) toDTO() Provider {
	return Provider{
		ID:          m.ID,
		ProviderID:  m.ProviderID,
		Name:        m.Name,
		Type:        m.Type,
		Icon:        m.Icon,
		IsBuiltin:   m.IsBuiltin,
		IsFree:      m.IsFree,
		Enabled:     m.Enabled,
		SortOrder:   m.SortOrder,
		APIEndpoint: m.APIEndpoint,
		APIKey:      m.APIKey,
		ExtraConfig: m.ExtraConfig,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
	}
}

// modelModel 鏁版嵁搴撴ā鍨?
type modelModel struct {
	bun.BaseModel `bun:"table:models,alias:m"`

	ID              int64     `bun:"id,pk,autoincrement"`
	ProviderID      string    `bun:"provider_id,notnull"`
	ModelID         string    `bun:"model_id,notnull"`
	Name            string    `bun:"name,notnull"`
	Type            string    `bun:"type,notnull"`
	Capabilities    string    `bun:"capabilities,notnull"` // JSON 鏁扮粍鏍煎紡瀛樺偍
	DefaultUseModel string    `bun:"default_use_model,notnull"`
	IsBuiltin       bool      `bun:"is_builtin,notnull"`
	Enabled         bool      `bun:"enabled,notnull"`
	SortOrder       int       `bun:"sort_order,notnull"`
	CreatedAt       time.Time `bun:"created_at,notnull"`
	UpdatedAt       time.Time `bun:"updated_at,notnull"`
}

// BeforeInsert 鍦?INSERT 鏃惰嚜鍔ㄨ缃?created_at 鍜?updated_at锛堝瓧绗︿覆鏍煎紡锛?
var _ bun.BeforeInsertHook = (*modelModel)(nil)

func (*modelModel) BeforeInsert(ctx context.Context, query *bun.InsertQuery) error {
	now := sqlite.NowUTC()
	query.Value("created_at", "?", now)
	query.Value("updated_at", "?", now)
	return nil
}

// BeforeUpdate 鍦?UPDATE 鏃惰嚜鍔ㄨ缃?updated_at锛堝瓧绗︿覆鏍煎紡锛?
var _ bun.BeforeUpdateHook = (*modelModel)(nil)

func (*modelModel) BeforeUpdate(ctx context.Context, query *bun.UpdateQuery) error {
	query.Set("updated_at = ?", sqlite.NowUTC())
	return nil
}

func (m *modelModel) toDTO() Model {
	var capabilities []string
	_ = json.Unmarshal([]byte(m.Capabilities), &capabilities)
	return Model{
		ID:              m.ID,
		ProviderID:      m.ProviderID,
		ModelID:         m.ModelID,
		Name:            m.Name,
		ModelSupplier:   "",
		UniModelName:    "",
		Type:            m.Type,
		Capabilities:    capabilities,
		DefaultUseModel: m.DefaultUseModel,
		IsBuiltin:       m.IsBuiltin,
		Enabled:         m.Enabled,
		SortOrder:       m.SortOrder,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}
