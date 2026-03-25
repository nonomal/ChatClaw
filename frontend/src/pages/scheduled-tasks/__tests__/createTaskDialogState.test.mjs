import test from 'node:test'
import assert from 'node:assert/strict'

import { prepareCreateTaskDialogState, prepareEditTaskDialogState } from '../createTaskDialogState.ts'

test('prepareCreateTaskDialogState reloads base options before opening create dialog', async () => {
  const callOrder = []
  const expectedForm = { name: 'new-task' }

  const state = await prepareCreateTaskDialogState(
    async () => {
      callOrder.push('load-base-options')
    },
    () => {
      callOrder.push('build-empty-form')
      return /** @type {any} */ (expectedForm)
    }
  )

  assert.deepEqual(callOrder, ['load-base-options', 'build-empty-form'])
  assert.equal(state.editingTask, null)
  assert.equal(state.form, expectedForm)
  assert.equal(state.createDialogOpen, true)
})

test('prepareEditTaskDialogState reloads base options before opening edit dialog', async () => {
  const callOrder = []
  const task = /** @type {any} */ ({ id: 7, name: 'existing-task' })
  const expectedForm = { id: 7, name: 'edited-task' }

  const state = await prepareEditTaskDialogState(
    async () => {
      callOrder.push('load-base-options')
    },
    task,
    (currentTask) => {
      callOrder.push('build-task-form')
      assert.equal(currentTask, task)
      return /** @type {any} */ (expectedForm)
    }
  )

  assert.deepEqual(callOrder, ['load-base-options', 'build-task-form'])
  assert.equal(state.editingTask, task)
  assert.equal(state.form, expectedForm)
  assert.equal(state.createDialogOpen, true)
})
