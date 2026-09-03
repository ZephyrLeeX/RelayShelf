/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type StorageRuntimeStatus = {
    healthy: boolean;
    reason: StorageRuntimeStatus.reason;
    lastCheckedAt: string | null;
    changedAt: string;
};
export namespace StorageRuntimeStatus {
    export enum reason {
        HEALTHY = 'HEALTHY',
        NAS_UNAVAILABLE = 'NAS_UNAVAILABLE',
        NAS_TIMEOUT = 'NAS_TIMEOUT',
        NAS_FULL = 'NAS_FULL',
    }
}

