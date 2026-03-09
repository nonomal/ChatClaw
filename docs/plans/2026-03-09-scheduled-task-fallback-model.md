# Scheduled Task Fallback Model Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 让计划任务在所选 AI 助手未配置默认模型时，自动选择第一个可用的 LLM 模型创建会话。

**Architecture:** 在 `scheduledtasks` 服务端新增一个兜底选模逻辑，优先使用助手默认模型；若为空，则从已启用 provider 的可用 LLM 模型中按与前端一致的顺序选择首个模型。选择结果仅写入本次计划任务创建的 conversation，不回写助手配置。

**Tech Stack:** Go, Bun, SQLite, Wails

---

### Task 1: 先补失败测试

**Files:**
- Modify: `internal/services/scheduledtasks/service_test.go`

**Step 1: Write the failing test**

新增一个测试，覆盖“助手默认模型为空，但存在已启用 provider 和可用 LLM 模型”时，计划任务运行应成功并把兜底模型写入新会话。

**Step 2: Run test to verify it fails**

Run: `go test ./internal/services/scheduledtasks -run TestRunScheduledTaskNowFallsBackToFirstAvailableLLM`
Expected: FAIL，原因是当前逻辑不会兜底选模。

### Task 2: 实现服务端兜底选模

**Files:**
- Modify: `internal/services/scheduledtasks/service.go`
- Modify: `internal/services/scheduledtasks/model.go`
- Modify: `internal/services/scheduledtasks/conversation_runner.go`

**Step 1: Write minimal implementation**

新增查询逻辑，在助手默认模型为空时，从启用的 provider 和启用的 `llm` 模型中按排序取第一个。

**Step 2: Keep write scope minimal**

只将所选 provider/model 写入本次 `CreateConversation` 输入，不修改 `agents` 表中的默认模型字段。

**Step 3: Preserve existing behavior**

若助手已有默认模型，则继续沿用当前逻辑。

### Task 3: 验证与回归

**Files:**
- Modify: `internal/services/scheduledtasks/service_test.go`

**Step 1: Run focused tests**

Run: `go test ./internal/services/scheduledtasks`
Expected: PASS

**Step 2: Check for regressions**

确认已有“优先使用助手默认模型”的测试继续通过。
