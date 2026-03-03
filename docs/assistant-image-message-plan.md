# ChatClaw AI 助手「发送图片」功能开发计划（基于 Eino）

> 本文档目标：在 **无外部 AI token 或无联网的情况下**，也能完全按照本文档说明实现 AI 助手的「发送图片」能力。  
> 当前代码版本：以 `internal/services/chat` 与 `frontend/src/pages/assistant` 目录中的现有实现为基础。

---

## 1. 目标与约束

- **主要目标**
  - 在 AI 助手对话中，允许用户 **附带多张图片** 一起发送给模型。
  - 后端基于 **Eino ChatModel + ADK Runner**，将文本 + 图片一起传给支持多模态的模型。
  - 前端支持 **选择图片预览、删除、限制大小与数量**，并在消息列表中展示已发送的图片。

- **通讯约束（你给出的约定）**
  1. **通讯层：图片统一使用 base64 数据传输**。  
     - 前端发送：`data:image/png;base64,xxx` 或单独的 `base64 + mime_type` 字段。
     - 后端与 Eino 之间仍然使用结构化字段构造 `schema.Message`。
  2. **存储层：单独添加一个 JSON 字段存储图片信息**。  
     - 消息主表继续存储 `content` 文本；新增 JSON 字段记录该条消息关联的多张图片的元信息。

- **现状简要**
  - 表结构：
    - `messages` 表已存在（见 `internal/sqlite/migrations/202602051000_create_chat_messages_table.go`）。  
    - 已有一张 `message_attachments` 表（图片、文件等多模态输入），目前用于基于文件路径的附件管理。
  - 服务层：
    - Chat 入口：`internal/services/chat/service.go`（`SendMessage` / `EditAndResend` 等）。
    - 消息及 DTO：`internal/services/chat/model.go`（`Message` / `messageModel` / 事件结构体）。
    - 与 Eino 的集成：`internal/services/chat/generation.go`（`runGeneration` / `runGenerationCore` / `loadMessagesForContext` 等），通过 `schema.Message` 与 Eino ADK 交互。
  - 前端：
    - 输入区：`frontend/src/pages/assistant/components/ChatInputArea.vue`。
    - 消息列表与消息项：`ChatMessageList.vue` / `ChatMessageItem.vue`。
    - Store：`frontend/src/stores/chat.ts` 与 `useConversations.ts` 等。

---

## 2. 数据与接口设计

### 2.1 数据结构：消息图片 JSON 字段

#### 2.1.1 新增字段

- 在 `messages` 表上新增字段（示例命名，推荐二选一，根据最终实现选择）：
  - 方案 A：`images_json TEXT NOT NULL DEFAULT '[]'`
  - 方案 B：`attachments_json TEXT NOT NULL DEFAULT '[]'`（更通用，可扩展为文件/音频等）

> 下文用 **`images_json`** 作为具体示例，你也可以在真实开发中改名为更通用的 `attachments_json`。

#### 2.1.2 JSON 结构约定

```json
[
  {
    "id": "local-uuid-or-hash",          // 客户端临时 ID，可选
    "kind": "image",                     // 目前固定为 image，预留扩展
    "source": "inline_base64",          // inline_base64 / file_path / remote_url
    "mime_type": "image/png",
    "base64": "iVBORw0KGgoAAA...",       // 当 source=inline_base64 时必填（不含 data: 前缀）
    "data_url": "data:image/png;base64,xxx", // 可选，前端直接展示用
    "width": 0,                          // 可选元数据
    "height": 0,
    "file_name": "screenshot.png",       // 原始文件名
    "size": 123456                       // 字节数
  }
]
```

- **存储层建议**
  - DB 中直接保存 **压缩后的 base64（不带 `data:` 前缀）**，避免冗余。
  - 前端/服务层在需要时动态拼接 `data:<mime>;base64,<base64>`。
  - 如后续希望与 `message_attachments` 表打通，可把 `source` 设计为：
    - `inline_base64`：当前规划（无需访问文件，只靠 JSON）。
    - `db_attachment`：引用 `message_attachments.id`，实现持久化文件管理。

### 2.2 Go 层 DTO / Model 设计

#### 2.2.1 Go 结构体

在 `internal/services/chat/model.go` 添加图片结构与字段（示意）：

```go
// ImagePayload describes a single image attached to a message.
type ImagePayload struct {
    ID       string `json:"id,omitempty"`
    Kind     string `json:"kind"`                 // "image"
    Source   string `json:"source"`               // "inline_base64"
    MimeType string `json:"mime_type"`
    Base64   string `json:"base64"`               // without "data:" prefix
    DataURL  string `json:"data_url,omitempty"`   // optional convenience field for frontend
    Width    int    `json:"width,omitempty"`
    Height   int    `json:"height,omitempty"`
    FileName string `json:"file_name,omitempty"`
    Size     int64  `json:"size,omitempty"`
}
```

- DTO 层（返回给前端的 `Message`）新增字段：

```go
type Message struct {
    // ... existing fields ...
    ImagesJSON string `json:"images_json,omitempty"` // raw JSON string of []ImagePayload
}
```

- DB 模型 `messageModel` 新增字段：

```go
type messageModel struct {
    // ... existing fields ...
    ImagesJSON string `bun:"images_json,notnull" json:"images_json"`
}
```

- `toDTO()` 中补充赋值。

> 注意：实现时需要在迁移之后，确保已有数据的默认值为 `'[]'`，避免 `NOT NULL` 冲突。

#### 2.2.2 发送消息输入结构

- 现有 `SendMessageInput`：

```go
type SendMessageInput struct {
    ConversationID int64  `json:"conversation_id"`
    Content        string `json:"content"`
    TabID          string `json:"tab_id"`
}
```

- 扩展为支持图片：

```go
type SendMessageInput struct {
    ConversationID int64          `json:"conversation_id"`
    Content        string         `json:"content"`
    TabID          string         `json:"tab_id"`
    Images         []ImagePayload `json:"images,omitempty"` // from frontend (base64)
}
```

- `EditAndResendInput` 如需支持修改图片，可类似扩展 `Images` 字段；首期可以**仅支持修改文本**，在文档中注明限制。

### 2.3 前后端通讯 JSON 协议

- **前端 → 后端（发送消息）**

```json
{
  "conversation_id": 1,
  "tab_id": "assistant-main",
  "content": "请帮我分析这张图片",
  "images": [
    {
      "kind": "image",
      "source": "inline_base64",
      "mime_type": "image/png",
      "base64": "iVBORw0KGgoAAA...",
      "file_name": "screenshot.png",
      "size": 123456
    }
  ]
}
```

- **后端 → 前端（事件流 + 历史消息）**
  - 历史消息 `Message` 中带上 `images_json` 字段（字符串），前端自行 `JSON.parse` 渲染。
  - Chat 事件（如 `chat:start` / `chat:chunk` / `chat:complete`）**无需修改结构**，只要在最终保存消息时写入 `images_json`，前端通过刷新/增量更新拿到图片数据即可。

---

## 3. 后端改造步骤（Go / Eino）

### 3.1 数据库迁移

1. 在 `internal/sqlite/migrations/` 中创建新的迁移文件，例如：
   - `202603031200_add_message_images_json.go`
2. 迁移内容（示意）：
   - `ALTER TABLE messages ADD COLUMN images_json TEXT NOT NULL DEFAULT '[]';`
3. 回滚逻辑：
   - `ALTER TABLE` 不支持直接删列，可在回滚中 **保留该列** 或通过重建表（成本较高，一般不建议）；因此建议回滚函数中仅保留注释或不做实际操作。

### 3.2 模型与 DTO 更新

在 `internal/services/chat/model.go` 中：

1. 定义 `ImagePayload` 结构体。
2. 给 `messageModel` 添加 `ImagesJSON` 字段（`bun` tag 与上游迁移保持一致）。
3. DTO `Message` 增加 `ImagesJSON` 字段。
4. 在 `toDTO()` 中补充字段复制。

> 注意：所有新字段应遵循项目规范，使用 `sqlite.NowUTC()` 钩子依旧只更新时间字段，不对 `images_json` 做特殊处理。

### 3.3 SendMessage 入口处理图片

位置：`internal/services/chat/service.go` 中 `SendMessage`。

1. 校验输入：
   - 在原有 `content` 校验前/后，增加对 `input.Images` 的合法性检查：
     - 最多图片数量（例如 4 张）。
     - 单张/总大小限制（例如 `<= 2MB/8MB`）。
     - 必须为 `image/*`。
   - 如果文本为空但有图片，应允许发送（不要被现有 `content == ""` 限制拦截），需要修改校验逻辑：
     - 新规则：**文本与图片至少有一个非空**。

2. 将 `input.Images` 序列化为 JSON 字符串：

```go
imagesJSON := "[]"
if len(input.Images) > 0 {
    b, err := json.Marshal(input.Images)
    if err != nil {
        return nil, errs.Wrap("error.chat_images_serialize_failed", err)
    }
    imagesJSON = string(b)
}
```

3. 在 `runGeneration` 中写入用户消息时，将 `ImagesJSON` 一并保存：

```go
userMsg := &messageModel{
    ConversationID: conversationID,
    Role:           RoleUser,
    Content:        userContent,
    Status:         StatusSuccess,
    ToolCalls:      "[]",
    ImagesJSON:     imagesJSON,
}
```

4. 助手消息初始插入时（`assistantMsg`）可暂时 `ImagesJSON: "[]"`，后续如需要支持模型返回图片再扩展。

### 3.4 将图片传递给 Eino ChatModel

核心改造位置：`internal/services/chat/generation.go` 中的 `loadMessagesForContext`。

#### 3.4.1 现状

- 当前仅使用 `m.Content` 填充 `schema.Message.Content`，未使用多模态能力。

#### 3.4.2 目标行为

- 对于带图片的 **user 消息**：
  - `schema.Message` 采用 **多模态内容** 形式：
    - 文本部分仍通过 `Content` 或 `UserInputMultiContent` 中的 text part。
    - 图片部分使用 `MessageInputPart`，类型为 `image_url`，`URL` 形如 `data:<mime>;base64,<base64>`。
  - 参考 Eino 文档（ChatModel 使用指南 & OpenAI/Gemini/Claude 多模态示例）。

#### 3.4.3 具体改造步骤

1. 在 `loadMessagesForContext` 中，读取每条 `messageModel` 的 `ImagesJSON` 字段：
   - 仅对 `RoleUser` 消息解析。
2. 将 `ImagesJSON` 解析为 `[]ImagePayload`：

```go
var images []ImagePayload
if m.ImagesJSON != "" && m.ImagesJSON != "[]" {
    if err := json.Unmarshal([]byte(m.ImagesJSON), &images); err != nil {
        // 解析失败时，打印 warn 日志，继续仅用文本
        s.app.Logger.Warn("[chat] failed to parse images_json", "msg_id", m.ID, "error", err)
    }
}
```

3. 构造 `schema.Message`：

- 对于不含图片的消息，保留现有逻辑。
- 对于含图片的 user 消息：

```go
msg := &schema.Message{
    Role: role, // schema.User
}

hasText := strings.TrimSpace(m.Content) != ""
if !hasText && len(images) == 0 {
    // 防御性判断，跳过空消息
    continue
}

// Prefer multi-content form
var parts []schema.MessageInputPart
if hasText {
    parts = append(parts, schema.MessageInputPart{
        Type: schema.MessageInputTypeText,
        Text: m.Content,
    })
} else {
    // 兼容原有使用 Content 的逻辑
    msg.Content = m.Content
}

for _, img := range images {
    if img.Source != "inline_base64" || img.Base64 == "" || img.MimeType == "" {
        continue
    }
    dataURL := fmt.Sprintf("data:%s;base64,%s", img.MimeType, img.Base64)
    parts = append(parts, schema.MessageInputPart{
        Type: schema.MessageInputTypeImageURL,
        ImageURL: &schema.ChatMessageImageURL{
            URL: dataURL,
        },
    })
}

if len(parts) > 0 {
    msg.UserInputMultiContent = parts
}
```

> 说明：具体字段名以 Eino 当前版本 `schema.Message` 为准，上述代码需根据实际 API 略作调整（参考官方文档 `chat_model_guide` 与 `chat_model_openai`/`chat_model_gemini` 等示例）。

4. 仅在配置的模型 **支持多模态** 时启用该逻辑：
   - 可以通过 `agentConfig.ModelID` / `providerConfig.Type` 识别多模态模型（如 `gpt-4.1-mini` 带 vision、`claude-3.7-sonnet` 带 image 支持等），否则 **降级为文本-only**：
     - 降级策略：只传 `content`，图片仍保存在 DB 和前端展示，但不会传给模型。
   - 可在文档中记录一份「推荐多模态模型列表」供配置参考。

### 3.5 其他后端注意点

- 事件结构（`ChatChunkEvent`、`ChatStartEvent` 等）暂时无需为图片扩展，首版本只支持 **用户上传图片 + 模型文本回答**。
- 如后续希望支持模型返回图片，可在：
  - `processStreamingOutput` / `processNonStreamingOutput` 中检测 `schema.Message` 的图片输出，类似构建 `ImagesJSON` 后保存至 assistant 消息。

---

## 4. 前端改造步骤（Vue 3 / shadcn-vue）

### 4.1 输入区 UI：选择图片按钮与预览

位置：`frontend/src/pages/assistant/components/ChatInputArea.vue`

#### 4.1.1 新增状态（通过父组件或 Pinia）

- 在上层容器（例如 `AssistantPage.vue` 或 `chat` store）中维护状态：

```ts
interface PendingImage {
  id: string       // uuid
  file: File
  mimeType: string
  base64: string   // without data prefix, or keep whole data URL
  dataUrl: string  // for <img> preview
  fileName: string
  size: number
}
```

- 在 `ChatInputArea` 中通过 `props` / `emit` 来管理：
  - `props.pendingImages: PendingImage[]`
  - emits:
    - `addImages(files: FileList | File[])`
    - `removeImage(id: string)`
    - `clearImages()`

> 为保持组件职责单一，推荐：`ChatInputArea` 只负责 UI 与事件，具体 File→base64 的读取由父组件负责。

#### 4.1.2 选择图片按钮

1. 在底部工具区（`Thinking`、`知识库选择` 按钮一排）中，按你截图中标红位置新增一个图片按钮：
   - 使用 `Button` + 图标（SVG 图标要遵守项目规则：`stroke="currentColor"` / `fill="currentColor"`）。
   - 点击时触发隐藏的 `<input type="file" accept="image/*" multiple>`。

2. 简要逻辑：

- 在父组件中：

```ts
const fileInputRef = ref<HTMLInputElement | null>(null)

const handleSelectImagesClick = () => {
  fileInputRef.value?.click()
}

const handleFilesSelected = async (files: FileList | null) => {
  if (!files || !files.length) return
  // 读取为 base64，生成 PendingImage 列表，更新状态
}
```

- 读取 base64 时注意：
  - 限制单张大小（例如 `<= 2MB`）及总量。
  - 使用 `FileReader.readAsDataURL`，然后拆分出 `data:<mime>;base64,xxx` 中的纯 base64。

#### 4.1.3 图片预览区域

- 在 `textarea` 下方 / 发送按钮上方增加一个简单的预览区域：
  - 使用 `flex` + `gap-2`，每张图片为一个小卡片：
    - 缩略图：`<img :src="img.dataUrl">`
    - 删除按钮 `X`。
  - 示例 Tailwind：
    - 容器：`class="mt-2 flex flex-wrap gap-2"`
    - 卡片：`class="relative h-16 w-16 overflow-hidden rounded-md border border-border bg-muted/40"`

### 4.2 发送消息时组装请求

位置：`frontend/src/stores/chat.ts` 或 `useConversations.ts` 中发起 Wails 绑定调用的地方。

1. 将 `PendingImage[]` 映射为 `ImagePayload` JSON：

```ts
const images = pendingImages.value.map((img) => ({
  kind: 'image',
  source: 'inline_base64',
  mime_type: img.mimeType,
  base64: img.base64,
  file_name: img.fileName,
  size: img.size,
}))
```

2. 调用绑定方法：

```ts
await ChatService.SendMessage({
  conversation_id: activeConversationId.value,
  tab_id: currentTabId,
  content: chatInput.value,
  images,
})
```

3. 发送成功后：
   - 清空 `chatInput`。
   - 清空 `pendingImages`。

### 4.3 消息列表中展示图片

位置：`frontend/src/pages/assistant/components/ChatMessageItem.vue`

1. 在消息 props 中增加 `images_json?: string`，与后端 DTO 对齐。
2. 在组件内部：

```ts
const images = computed<PendingImageLike[]>(() => {
  if (!props.message.images_json) return []
  try {
    return JSON.parse(props.message.images_json)
  } catch {
    return []
  }
})
```

3. 在消息气泡中（仅限 `role === 'user'`，后续可扩展 assistant）添加预览：
   - 与输入区预览类似，使用小缩略图。
   - 若 DB 中仅保存纯 base64，需要在展示前拼接 `data:` 前缀。

4. UI 规范：
   - 图片使用 Tailwind 语义色（`bg-muted`, `border-border` 等），不要加重阴影。
   - 遵循项目的深色模式友好规范。

---

## 5. 与 Eino / 模型的对接注意事项

### 5.1 模型支持情况

- **OpenAI 兼容模型**（`gpt-4.1` / `gpt-4.1-mini` 等）：
  - 支持 `image_url` 类型，URL 可以是 HTTP 链接或 `data:` URL（base64）。
- **Claude 模型**（通过 `anthropic` provider）：
  - 支持图片，但具体 API 形式以 Eino 的 `claude.ChatModel` 文档为准。
- **Gemini**：
  - 支持多模态，需要确认 `einogemini.NewChatModel` 的期望输入格式。

> 在实现前，建议阅读你给出的两类文档：
> - ChatModel 核心使用指南：`chat_model_guide`
> - 不同模型对接示例：`ecosystem_integration/chat_model/*`

### 5.2 降级策略

- 若当前会话选用的模型 **不支持图片**：
  - 仍保存并展示图片，但 **不将图片内容传入 Eino**，仅用文本 `content`。
  - 在前端可提示用户「当前模型不支持图像理解」。

### 5.3 Token 与体积控制

- base64 相比原图体积约 **增加 33%**，建议：
  - 限制单张分辨率与大小（必要时客户端压缩或后端拒绝过大图片）。
  - 控制单消息图片数量。

---

## 6. 分阶段实施建议

### Phase 1：仅用户图片输入 + 文本回答

1. 完成 DB 迁移与 `images_json` 字段接入。
2. 扩展 `SendMessageInput` / `messageModel` / `Message` DTO。
3. `SendMessage` 支持保存 `images_json`，`loadMessagesForContext` 将图片传入 Eino。
4. 前端实现选择图片、预览、发送与展示。

### Phase 2：模型输出图片（可选）

1. 在 `processStreamingOutput` / `processNonStreamingOutput` 中解析模型返回的图片内容（不同模型格式不同）。
2. 为 assistant 消息填充 `images_json` 字段。
3. 前端为 assistant 气泡增加图片展示。

### Phase 3：与 `message_attachments` 表打通（可选）

1. 将 `source` 字段扩展为 `db_attachment`。
2. 图片上传时物理落盘 + 在 `message_attachments` 插入记录；`images_json` 仅存储引用 ID 与少量元数据。
3. 统一文件访问权限与清理策略。

---

## 7. 验收清单

- **功能**
  - [ ] 聊天输入区支持选择多张图片，显示缩略图，可删除。
  - [ ] 无文本但有图片时可以发送消息。
  - [ ] 历史记录中能看到用户发送过的图片。
  - [ ] 使用支持多模态的模型时，模型回答明显受图片影响（可通过测试 prompt 验证）。

- **存储**
  - [ ] `messages.images_json` 字段按约定结构存储。
  - [ ] 旧数据不受影响，无 `NULL` 或迁移错误。

- **性能与安全**
  - [ ] 单条消息图片数量与体积有合理限制，超出时有用户友好提示。
  - [ ] base64 解析失败时有日志且不会影响其他消息。

---

通过以上步骤，即使在 **无法继续调用 AI 接口或没有 token 的情况下**，你也可以完全依照本文档，逐步为 ChatClaw AI 助手实现「发送图片」能力，包括前端 UI、后端数据结构以及与 Eino ChatModel 的集成改造。 

