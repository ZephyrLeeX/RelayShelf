/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { AddAttachmentsRequest } from '../models/AddAttachmentsRequest';
import type { AdminStatus } from '../models/AdminStatus';
import type { AdminUser } from '../models/AdminUser';
import type { AdminUserList } from '../models/AdminUserList';
import type { AuthBootstrap } from '../models/AuthBootstrap';
import type { CreateAdminUserRequest } from '../models/CreateAdminUserRequest';
import type { CreateMessageRequest } from '../models/CreateMessageRequest';
import type { CreateUploadRequest } from '../models/CreateUploadRequest';
import type { Device } from '../models/Device';
import type { DirectSendRequest } from '../models/DirectSendRequest';
import type { EditMessageRequest } from '../models/EditMessageRequest';
import type { EditSensitiveBodyRequest } from '../models/EditSensitiveBodyRequest';
import type { FavoriteRequest } from '../models/FavoriteRequest';
import type { ForwardRequest } from '../models/ForwardRequest';
import type { Lifecycle } from '../models/Lifecycle';
import type { LoginRequest } from '../models/LoginRequest';
import type { Message } from '../models/Message';
import type { MessageDeliveryReceipt } from '../models/MessageDeliveryReceipt';
import type { MessageList } from '../models/MessageList';
import type { PasswordChangeRequest } from '../models/PasswordChangeRequest';
import type { RenameDeviceRequest } from '../models/RenameDeviceRequest';
import type { ReplaceMessageTagsRequest } from '../models/ReplaceMessageTagsRequest';
import type { ResetAdminUserPasswordRequest } from '../models/ResetAdminUserPasswordRequest';
import type { RuntimeSettings } from '../models/RuntimeSettings';
import type { SensitiveBody } from '../models/SensitiveBody';
import type { SensitiveRequest } from '../models/SensitiveRequest';
import type { Session } from '../models/Session';
import type { StorageStatus } from '../models/StorageStatus';
import type { Tag } from '../models/Tag';
import type { TagRequest } from '../models/TagRequest';
import type { TOTPChallengeRequest } from '../models/TOTPChallengeRequest';
import type { TOTPCodeRequest } from '../models/TOTPCodeRequest';
import type { TOTPEnrollmentPending } from '../models/TOTPEnrollmentPending';
import type { TOTPEnrollmentRequest } from '../models/TOTPEnrollmentRequest';
import type { TOTPLoginChallenge } from '../models/TOTPLoginChallenge';
import type { TOTPStatus } from '../models/TOTPStatus';
import type { UpdateRuntimeSettingsRequest } from '../models/UpdateRuntimeSettingsRequest';
import type { UpdateTagRequest } from '../models/UpdateTagRequest';
import type { UploadSession } from '../models/UploadSession';
import type { VersionRequest } from '../models/VersionRequest';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class DefaultService {
    /**
     * Stream metadata-only RealtimeEvent SSE frames. Last-Event-ID is ignored; clients refetch truth after reconnect.
     * @returns string Realtime event stream
     * @throws ApiError
     */
    public static getEvents(): CancelablePromise<string> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/events',
            errors: {
                401: `API error`,
            },
        });
    }
    /**
     * @param requestBody
     * @returns AuthBootstrap Authenticated session bootstrap
     * @returns TOTPLoginChallenge Password accepted but a TOTP second factor is required; no session cookie is issued until the challenge completes
     * @throws ApiError
     */
    public static login(
        requestBody: LoginRequest,
    ): CancelablePromise<AuthBootstrap | TOTPLoginChallenge> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/auth/login',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                429: `API error`,
            },
        });
    }
    /**
     * @param requestBody
     * @returns AuthBootstrap Second factor accepted; authenticated session bootstrap
     * @throws ApiError
     */
    public static completeLoginTotp(
        requestBody: TOTPChallengeRequest,
    ): CancelablePromise<AuthBootstrap> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/auth/login/totp',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                410: `API error`,
                429: `API error`,
            },
        });
    }
    /**
     * @returns TOTPStatus Current user TOTP enrollment state without any secret material
     * @throws ApiError
     */
    public static getTotpStatus(): CancelablePromise<TOTPStatus> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/auth/totp',
            errors: {
                401: `API error`,
            },
        });
    }
    /**
     * @param requestBody
     * @returns TOTPEnrollmentPending Pending enrollment material; TOTP stays disabled until the confirmation code validates
     * @throws ApiError
     */
    public static startTotpEnrollment(
        requestBody: TOTPEnrollmentRequest,
    ): CancelablePromise<TOTPEnrollmentPending> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/auth/totp/enroll',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                403: `API error`,
                409: `API error`,
                422: `API error`,
                429: `API error`,
            },
        });
    }
    /**
     * @param requestBody
     * @returns TOTPStatus TOTP enabled
     * @throws ApiError
     */
    public static confirmTotpEnrollment(
        requestBody: TOTPCodeRequest,
    ): CancelablePromise<TOTPStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/auth/totp/confirm',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                403: `API error`,
                404: `API error`,
                429: `API error`,
            },
        });
    }
    /**
     * @param requestBody
     * @returns TOTPStatus TOTP disabled
     * @throws ApiError
     */
    public static disableTotp(
        requestBody: TOTPCodeRequest,
    ): CancelablePromise<TOTPStatus> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/auth/totp/disable',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                403: `API error`,
                404: `API error`,
                429: `API error`,
            },
        });
    }
    /**
     * @returns void
     * @throws ApiError
     */
    public static logout(): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/auth/logout',
            errors: {
                401: `API error`,
                403: `API error`,
            },
        });
    }
    /**
     * @returns AuthBootstrap Current session bootstrap
     * @throws ApiError
     */
    public static getAuthSession(): CancelablePromise<AuthBootstrap> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/auth/session',
            errors: {
                401: `API error`,
            },
        });
    }
    /**
     * @param requestBody
     * @returns void
     * @throws ApiError
     */
    public static changePassword(
        requestBody: PasswordChangeRequest,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/auth/password/change',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                403: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @returns Session Current user's sessions
     * @throws ApiError
     */
    public static listSessions(): CancelablePromise<Array<Session>> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/sessions',
            errors: {
                401: `API error`,
            },
        });
    }
    /**
     * @param sessionId
     * @returns void
     * @throws ApiError
     */
    public static revokeSession(
        sessionId: string,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/sessions/{sessionId}',
            path: {
                'sessionId': sessionId,
            },
            errors: {
                401: `API error`,
                403: `API error`,
                404: `API error`,
            },
        });
    }
    /**
     * @returns Device Current user's devices
     * @throws ApiError
     */
    public static listDevices(): CancelablePromise<Array<Device>> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/devices',
            errors: {
                401: `API error`,
            },
        });
    }
    /**
     * @param deviceId
     * @param requestBody
     * @returns Device Renamed device
     * @throws ApiError
     */
    public static renameDevice(
        deviceId: string,
        requestBody: RenameDeviceRequest,
    ): CancelablePromise<Device> {
        return __request(OpenAPI, {
            method: 'PATCH',
            url: '/devices/{deviceId}',
            path: {
                'deviceId': deviceId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                403: `API error`,
                404: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param requestBody
     * @returns UploadSession Created upload session
     * @throws ApiError
     */
    public static createUpload(
        requestBody: CreateUploadRequest,
    ): CancelablePromise<UploadSession> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/uploads',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                403: `API error`,
                422: `API error`,
                503: `API error`,
            },
        });
    }
    /**
     * @param uploadId
     * @returns UploadSession Current upload status
     * @throws ApiError
     */
    public static getUpload(
        uploadId: string,
    ): CancelablePromise<UploadSession> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/uploads/{uploadId}',
            path: {
                'uploadId': uploadId,
            },
            errors: {
                401: `API error`,
                404: `API error`,
            },
        });
    }
    /**
     * @param uploadId
     * @param partNumber
     * @param requestBody
     * @returns void
     * @throws ApiError
     */
    public static putUploadPart(
        uploadId: string,
        partNumber: number,
        requestBody: Blob,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/uploads/{uploadId}/parts/{partNumber}',
            path: {
                'uploadId': uploadId,
                'partNumber': partNumber,
            },
            body: requestBody,
            mediaType: 'application/octet-stream',
            errors: {
                401: `API error`,
                404: `API error`,
                409: `API error`,
                410: `API error`,
                415: `API error`,
                422: `API error`,
                503: `API error`,
            },
        });
    }
    /**
     * @param uploadId
     * @returns UploadSession Upload finalized or already completed
     * @throws ApiError
     */
    public static completeUpload(
        uploadId: string,
    ): CancelablePromise<UploadSession> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/uploads/{uploadId}/complete',
            path: {
                'uploadId': uploadId,
            },
            errors: {
                401: `API error`,
                403: `API error`,
                404: `API error`,
                409: `API error`,
                410: `API error`,
                422: `API error`,
                503: `API error`,
                507: `API error`,
            },
        });
    }
    /**
     * @param idempotencyKey
     * @param requestBody
     * @returns Message Created message
     * @throws ApiError
     */
    public static createMessage(
        idempotencyKey: string,
        requestBody: CreateMessageRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/messages',
            headers: {
                'Idempotency-Key': idempotencyKey,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param lifecycle
     * @param favorite
     * @param tagId
     * @param cursor
     * @param limit
     * @returns MessageList Active messages
     * @throws ApiError
     */
    public static listMessages(
        lifecycle?: Lifecycle,
        favorite?: boolean,
        tagId?: Array<string>,
        cursor?: string,
        limit: number = 30,
    ): CancelablePromise<MessageList> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/messages',
            query: {
                'lifecycle': lifecycle,
                'favorite': favorite,
                'tagId': tagId,
                'cursor': cursor,
                'limit': limit,
            },
            errors: {
                422: `API error`,
            },
        });
    }
    /**
     * @param q
     * @param lifecycle
     * @param favorite
     * @param tagId
     * @param type
     * @param createdAfter
     * @param createdBefore
     * @param cursor
     * @param limit
     * @returns MessageList Current user's matching active messages, newest first
     * @throws ApiError
     */
    public static searchMessages(
        q?: string,
        lifecycle?: Lifecycle,
        favorite?: boolean,
        tagId?: Array<string>,
        type?: string,
        createdAfter?: string,
        createdBefore?: string,
        cursor?: string,
        limit: number = 30,
    ): CancelablePromise<MessageList> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/search',
            query: {
                'q': q,
                'lifecycle': lifecycle,
                'favorite': favorite,
                'tagId': tagId,
                'type': type,
                'createdAfter': createdAfter,
                'createdBefore': createdBefore,
                'cursor': cursor,
                'limit': limit,
            },
            errors: {
                401: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param idempotencyKey
     * @param requestBody
     * @returns MessageDeliveryReceipt Immutable delivery receipt
     * @throws ApiError
     */
    public static directSendMessage(
        idempotencyKey: string,
        requestBody: DirectSendRequest,
    ): CancelablePromise<MessageDeliveryReceipt> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/messages/direct-send',
            headers: {
                'Idempotency-Key': idempotencyKey,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @returns Message Message detail
     * @throws ApiError
     */
    public static getMessage(
        messageId: string,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/messages/{messageId}',
            path: {
                'messageId': messageId,
            },
            errors: {
                404: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param requestBody
     * @returns Message Updated message
     * @throws ApiError
     */
    public static editMessage(
        messageId: string,
        requestBody: EditMessageRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'PATCH',
            url: '/messages/{messageId}',
            path: {
                'messageId': messageId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param requestBody
     * @returns Message Updated message
     * @throws ApiError
     */
    public static replaceMessageTags(
        messageId: string,
        requestBody: ReplaceMessageTagsRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/messages/{messageId}/tags',
            path: {
                'messageId': messageId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param requestBody
     * @returns Message Updated message
     * @throws ApiError
     */
    public static addMessageAttachments(
        messageId: string,
        requestBody: AddAttachmentsRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/messages/{messageId}/attachments',
            path: {
                'messageId': messageId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param attachmentId
     * @param requestBody
     * @returns Message Updated message
     * @throws ApiError
     */
    public static removeMessageAttachment(
        messageId: string,
        attachmentId: string,
        requestBody: VersionRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/messages/{messageId}/attachments/{attachmentId}',
            path: {
                'messageId': messageId,
                'attachmentId': attachmentId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param attachmentId
     * @returns binary Full attachment
     * @throws ApiError
     */
    public static downloadAttachment(
        attachmentId: string,
    ): CancelablePromise<Blob> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/attachments/{attachmentId}/download',
            path: {
                'attachmentId': attachmentId,
            },
            errors: {
                304: `Not modified`,
                404: `API error`,
                416: `API error`,
                503: `API error`,
            },
        });
    }
    /**
     * @param attachmentId
     * @returns binary Full allowlisted inline attachment
     * @throws ApiError
     */
    public static previewAttachment(
        attachmentId: string,
    ): CancelablePromise<Blob> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/attachments/{attachmentId}/preview',
            path: {
                'attachmentId': attachmentId,
            },
            errors: {
                304: `Not modified`,
                404: `API error`,
                416: `API error`,
                503: `API error`,
            },
        });
    }
    /**
     * @param attachmentId
     * @returns binary Server-generated safe raster thumbnail
     * @throws ApiError
     */
    public static getAttachmentThumbnail(
        attachmentId: string,
    ): CancelablePromise<Blob> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/attachments/{attachmentId}/thumbnail',
            path: {
                'attachmentId': attachmentId,
            },
            errors: {
                404: `API error`,
                503: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param requestBody
     * @returns Message Permanent message
     * @throws ApiError
     */
    public static makeMessagePermanent(
        messageId: string,
        requestBody: VersionRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/messages/{messageId}/make-permanent',
            path: {
                'messageId': messageId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param requestBody
     * @returns Message Updated message
     * @throws ApiError
     */
    public static setMessageFavorite(
        messageId: string,
        requestBody: FavoriteRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/messages/{messageId}/favorite',
            path: {
                'messageId': messageId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param requestBody
     * @returns Message Trashed message
     * @throws ApiError
     */
    public static trashMessage(
        messageId: string,
        requestBody: VersionRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/messages/{messageId}/trash',
            path: {
                'messageId': messageId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param requestBody
     * @returns Message Updated message metadata
     * @throws ApiError
     */
    public static setMessageSensitive(
        messageId: string,
        requestBody: SensitiveRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/messages/{messageId}/sensitive',
            path: {
                'messageId': messageId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @returns SensitiveBody Sensitive plaintext
     * @throws ApiError
     */
    public static revealSensitiveBody(
        messageId: string,
    ): CancelablePromise<SensitiveBody> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/messages/{messageId}/sensitive-body',
            path: {
                'messageId': messageId,
            },
            errors: {
                404: `API error`,
                409: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param requestBody
     * @returns Message Updated safe metadata
     * @throws ApiError
     */
    public static editSensitiveBody(
        messageId: string,
        requestBody: EditSensitiveBodyRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/messages/{messageId}/sensitive-body',
            path: {
                'messageId': messageId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param idempotencyKey
     * @param requestBody
     * @returns MessageDeliveryReceipt Immutable delivery receipt
     * @throws ApiError
     */
    public static forwardMessage(
        messageId: string,
        idempotencyKey: string,
        requestBody: ForwardRequest,
    ): CancelablePromise<MessageDeliveryReceipt> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/messages/{messageId}/forward',
            path: {
                'messageId': messageId,
            },
            headers: {
                'Idempotency-Key': idempotencyKey,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param cursor
     * @param limit
     * @returns MessageList Trashed messages
     * @throws ApiError
     */
    public static listTrash(
        cursor?: string,
        limit: number = 30,
    ): CancelablePromise<MessageList> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/trash',
            query: {
                'cursor': cursor,
                'limit': limit,
            },
            errors: {
                422: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @param requestBody
     * @returns Message Restored message
     * @throws ApiError
     */
    public static restoreMessage(
        messageId: string,
        requestBody: VersionRequest,
    ): CancelablePromise<Message> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/trash/{messageId}/restore',
            path: {
                'messageId': messageId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
            },
        });
    }
    /**
     * @param messageId
     * @returns void
     * @throws ApiError
     */
    public static permanentlyDeleteMessage(
        messageId: string,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/trash/{messageId}',
            path: {
                'messageId': messageId,
            },
            errors: {
                404: `API error`,
            },
        });
    }
    /**
     * @returns Tag User tags
     * @throws ApiError
     */
    public static listTags(): CancelablePromise<Array<Tag>> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/tags',
        });
    }
    /**
     * @param requestBody
     * @returns Tag Created tag
     * @throws ApiError
     */
    public static createTag(
        requestBody: TagRequest,
    ): CancelablePromise<Tag> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/tags',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param tagId
     * @param requestBody
     * @returns Tag Updated tag
     * @throws ApiError
     */
    public static updateTag(
        tagId: string,
        requestBody: UpdateTagRequest,
    ): CancelablePromise<Tag> {
        return __request(OpenAPI, {
            method: 'PATCH',
            url: '/tags/{tagId}',
            path: {
                'tagId': tagId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                404: `API error`,
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param tagId
     * @returns void
     * @throws ApiError
     */
    public static deleteTag(
        tagId: string,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/tags/{tagId}',
            path: {
                'tagId': tagId,
            },
            errors: {
                404: `API error`,
            },
        });
    }
    /**
     * @returns RuntimeSettings Typed singleton runtime settings
     * @throws ApiError
     */
    public static getRuntimeSettings(): CancelablePromise<RuntimeSettings> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/admin/settings',
            errors: {
                401: `API error`,
                403: `API error`,
            },
        });
    }
    /**
     * @param requestBody
     * @returns RuntimeSettings Atomically updated runtime settings
     * @throws ApiError
     */
    public static updateRuntimeSettings(
        requestBody: UpdateRuntimeSettingsRequest,
    ): CancelablePromise<RuntimeSettings> {
        return __request(OpenAPI, {
            method: 'PUT',
            url: '/admin/settings',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                403: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @returns StorageStatus Bounded storage health projection without internal paths
     * @throws ApiError
     */
    public static getStorageStatus(): CancelablePromise<StorageStatus> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/admin/storage',
            errors: {
                401: `API error`,
                403: `API error`,
            },
        });
    }
    /**
     * @returns AdminStatus Operational status without private content or raw job payloads
     * @throws ApiError
     */
    public static getAdminStatus(): CancelablePromise<AdminStatus> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/admin/status',
            errors: {
                401: `API error`,
                403: `API error`,
            },
        });
    }
    /**
     * @param cursor
     * @param limit
     * @returns AdminUserList Bounded operational user metadata page; every stored user is reachable via cursor pagination
     * @throws ApiError
     */
    public static listAdminUsers(
        cursor?: string,
        limit: number = 30,
    ): CancelablePromise<AdminUserList> {
        return __request(OpenAPI, {
            method: 'GET',
            url: '/admin/users',
            query: {
                'cursor': cursor,
                'limit': limit,
            },
            errors: {
                401: `API error`,
                403: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param requestBody
     * @returns AdminUser User created
     * @throws ApiError
     */
    public static createAdminUser(
        requestBody: CreateAdminUserRequest,
    ): CancelablePromise<AdminUser> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/admin/users',
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                403: `API error`,
                409: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param userId
     * @returns void
     * @throws ApiError
     */
    public static disableAdminUser(
        userId: string,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/admin/users/{userId}/disable',
            path: {
                'userId': userId,
            },
            errors: {
                401: `API error`,
                403: `API error`,
                404: `API error`,
            },
        });
    }
    /**
     * @param userId
     * @param requestBody
     * @returns void
     * @throws ApiError
     */
    public static resetAdminUserPassword(
        userId: string,
        requestBody: ResetAdminUserPasswordRequest,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'POST',
            url: '/admin/users/{userId}/password/reset',
            path: {
                'userId': userId,
            },
            body: requestBody,
            mediaType: 'application/json',
            errors: {
                401: `API error`,
                403: `API error`,
                404: `API error`,
                422: `API error`,
            },
        });
    }
    /**
     * @param userId
     * @returns void
     * @throws ApiError
     */
    public static deleteAdminUser(
        userId: string,
    ): CancelablePromise<void> {
        return __request(OpenAPI, {
            method: 'DELETE',
            url: '/admin/users/{userId}',
            path: {
                'userId': userId,
            },
            errors: {
                401: `API error`,
                403: `API error`,
                404: `API error`,
            },
        });
    }
}
