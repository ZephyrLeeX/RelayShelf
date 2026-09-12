import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { DefaultService, type BodyFormat, type EditMessageRequest, type Message } from '@/api/generated'
import { apiCodes, toApiError } from '@/shared/api/errors'
import { queryKeys } from '@/shared/api/queryKeys'

export type MessageCommand =
  | { type: 'permanent'; message: Message }
  | { type: 'extend'; message: Message; days: 1 | 3 | 7 }
  | { type: 'favorite'; message: Message; favorite: boolean }
  | { type: 'trash'; message: Message }
  | { type: 'restore'; message: Message }
  | { type: 'delete'; message: Message }
  | { type: 'sensitive'; message: Message; sensitive: boolean }
  | { type: 'edit'; message: Message; title: string; body?: string | null; bodyFormat?: BodyFormat }
  | { type: 'editSensitive'; message: Message; title: string; body: string }
  | { type: 'tags'; message: Message; tagIds: string[] }
  | { type: 'forward'; message: Message; recipientUserId: string }

async function execute(command: MessageCommand) {
  const { message } = command
  switch (command.type) {
    case 'permanent': return DefaultService.makeMessagePermanent(message.id, { expectedVersion: message.version })
    case 'extend': return DefaultService.extendMessageExpiry(message.id, { expectedVersion: message.version, days: command.days })
    case 'favorite': return DefaultService.setMessageFavorite(message.id, { expectedVersion: message.version, favorite: command.favorite })
    case 'trash': return DefaultService.trashMessage(message.id, { expectedVersion: message.version })
    case 'restore': return DefaultService.restoreMessage(message.id, { expectedVersion: message.version })
    case 'delete': await DefaultService.permanentlyDeleteMessage(message.id); return undefined
    case 'sensitive': return DefaultService.setMessageSensitive(message.id, { expectedVersion: message.version, sensitive: command.sensitive })
    case 'edit': {
      const payload: EditMessageRequest = { expectedVersion: message.version, title: command.title.trim() }
      if (command.body !== undefined) payload.body = command.body
      if (command.bodyFormat !== undefined) payload.bodyFormat = command.bodyFormat
      return DefaultService.editMessage(message.id, payload)
    }
    case 'editSensitive': return DefaultService.editSensitiveBody(message.id, { expectedVersion: message.version, title: command.title.trim(), body: command.body })
    case 'tags': return DefaultService.replaceMessageTags(message.id, { expectedVersion: message.version, tagIds: command.tagIds })
    case 'forward': return DefaultService.forwardMessage(message.id, crypto.randomUUID(), { expectedVersion: message.version, recipientUserId: command.recipientUserId })
  }
}

export function invalidateMessageTruth(id: string, client = useQueryClient()) {
  void client.invalidateQueries({ queryKey: queryKeys.messages.lists() })
  void client.invalidateQueries({ queryKey: queryKeys.messages.detail(id) })
  void client.invalidateQueries({ queryKey: queryKeys.search.root() })
  void client.invalidateQueries({ queryKey: queryKeys.trash.list() })
}

export function useMessageMutation() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: execute,
    onSuccess: (_, command) => invalidateMessageTruth(command.message.id, client),
    onError: (error, command) => {
      if (toApiError(error).code === apiCodes.versionConflict) invalidateMessageTruth(command.message.id, client)
    },
  })
}

export function mutationErrorMessage(error: unknown) {
  const adapted = toApiError(error)
  if (adapted.code === apiCodes.versionConflict) return '内容已在其他设备修改，请查看最新版本后重试。'
  if (adapted.code === apiCodes.favoriteRequiresPermanent) return '只有长期内容可以收藏。'
  return adapted.traceId ? `${adapted.message}（错误编号：${adapted.traceId}）` : adapted.message
}
