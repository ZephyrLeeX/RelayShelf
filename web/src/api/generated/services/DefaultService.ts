/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { AuthBootstrap } from '../models/AuthBootstrap';
import type { CreateMessageRequest } from '../models/CreateMessageRequest';
import type { Device } from '../models/Device';
import type { DirectSendRequest } from '../models/DirectSendRequest';
import type { EditMessageRequest } from '../models/EditMessageRequest';
import type { EditSensitiveBodyRequest } from '../models/EditSensitiveBodyRequest';
import type { FavoriteRequest } from '../models/FavoriteRequest';
import type { ForwardRequest } from '../models/ForwardRequest';
import type { Lifecycle } from '../models/Lifecycle';
import type { LoginRequest } from '../models/LoginRequest';
import type { Message } from '../models/Message';
import type { MessageList } from '../models/MessageList';
import type { PasswordChangeRequest } from '../models/PasswordChangeRequest';
import type { RenameDeviceRequest } from '../models/RenameDeviceRequest';
import type { ReplaceMessageTagsRequest } from '../models/ReplaceMessageTagsRequest';
import type { SensitiveBody } from '../models/SensitiveBody';
import type { SensitiveRequest } from '../models/SensitiveRequest';
import type { Session } from '../models/Session';
import type { Tag } from '../models/Tag';
import type { TagRequest } from '../models/TagRequest';
import type { UpdateTagRequest } from '../models/UpdateTagRequest';
import type { VersionRequest } from '../models/VersionRequest';
import type { CancelablePromise } from '../core/CancelablePromise';
import { OpenAPI } from '../core/OpenAPI';
import { request as __request } from '../core/request';
export class DefaultService {
    /**
     * @param requestBody
     * @returns AuthBootstrap Authenticated session bootstrap
     * @throws ApiError
     */
    public static login(
        requestBody: LoginRequest,
    ): CancelablePromise<AuthBootstrap> {
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
     * @param idempotencyKey
     * @param requestBody
     * @returns Message Recipient message
     * @throws ApiError
     */
    public static directSendMessage(
        idempotencyKey: string,
        requestBody: DirectSendRequest,
    ): CancelablePromise<Message> {
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
     * @returns Message Recipient message
     * @throws ApiError
     */
    public static forwardMessage(
        messageId: string,
        idempotencyKey: string,
        requestBody: ForwardRequest,
    ): CancelablePromise<Message> {
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
}
