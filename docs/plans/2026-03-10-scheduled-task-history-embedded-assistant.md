# 计划任务历史弹窗嵌入助手页 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 在计划任务历史记录弹窗右侧显示一个可继续交互的完整助手页实例，并用当前运行记录关联的会话初始化。

**Architecture:** 通过给 `AssistantPage` 增加嵌入模式和初始化会话参数，在同一个 Vue 应用中复用现有助手聊天能力。历史记录弹窗右侧不再渲染简化预览，而是挂载一个嵌入容器组件，并传入当前运行记录的会话 ID。

**Tech Stack:** Vue 3, Pinia, TypeScript, Wails bindings, existing assistant page components

---

### Task 1: 为助手页补充嵌入模式入口

**Files:**
- Modify: `frontend/src/pages/assistant/AssistantPage.vue`
- Create: `frontend/src/pages/assistant/components/EmbeddedAssistantPage.vue`

**Step 1: Write the failing test**

当前仓库没有对应页面级测试，改为先做静态实现并通过类型检查与构建验证。

**Step 2: Run test to verify it fails**

跳过单测，后续用 `npm run build` 验证。

**Step 3: Write minimal implementation**

- 为 `AssistantPage` 增加 `mode: 'main' | 'snap' | 'embedded'`。
- 增加 `initialConversationId`、`initialAgentId` 可选参数。
- 嵌入模式隐藏左侧侧栏，并跳过 tab 标题/图标更新、pending chat 初始化等主页面行为。
- 新建 `EmbeddedAssistantPage.vue`，只负责传入嵌入模式参数。

**Step 4: Run test to verify it passes**

Run: `npm run build`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/pages/assistant/AssistantPage.vue frontend/src/pages/assistant/components/EmbeddedAssistantPage.vue
git commit -m "feat: add embedded assistant mode"
```

### Task 2: 用嵌入助手替换历史弹窗右侧预览

**Files:**
- Modify: `frontend/src/pages/scheduled-tasks/components/TaskRunHistoryDialog.vue`
- Modify: `frontend/src/pages/scheduled-tasks/components/TaskRunConversationPreview.vue`

**Step 1: Write the failing test**

当前仓库没有该弹窗组件测试，改为实现后通过构建与手动验证确认行为。

**Step 2: Run test to verify it fails**

跳过单测，后续统一构建验证。

**Step 3: Write minimal implementation**

- 在历史弹窗右侧改为渲染嵌入式助手组件。
- 仅在没有关联会话时保留空态。
- 删除或停止使用旧的简化会话预览实现。

**Step 4: Run test to verify it passes**

Run: `npm run build`
Expected: PASS

**Step 5: Commit**

```bash
git add frontend/src/pages/scheduled-tasks/components/TaskRunHistoryDialog.vue frontend/src/pages/scheduled-tasks/components/TaskRunConversationPreview.vue
git commit -m "feat: embed assistant into task history dialog"
```

### Task 3: 验证嵌入模式交互

**Files:**
- No file changes required

**Step 1: Write the failing test**

无自动化测试，改为执行构建并记录手动验证点。

**Step 2: Run test to verify it fails**

跳过。

**Step 3: Write minimal implementation**

- 验证历史弹窗切换不同运行记录时，会话能正确切换。
- 验证嵌入模式下可继续发送消息、停止生成、打开工作区。
- 验证左侧助手/会话侧栏不显示。

**Step 4: Run test to verify it passes**

Run: `npm run build`
Expected: PASS

**Step 5: Commit**

```bash
git add .
git commit -m "test: verify embedded assistant history flow"
```
