/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { AuthBootstrap } from '../models/AuthBootstrap';
import type { Device } from '../models/Device';
import type { LoginRequest } from '../models/LoginRequest';
import type { PasswordChangeRequest } from '../models/PasswordChangeRequest';
import type { RenameDeviceRequest } from '../models/RenameDeviceRequest';
import type { Session } from '../models/Session';
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
}
