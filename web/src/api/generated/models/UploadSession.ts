/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { UploadStatus } from './UploadStatus';
export type UploadSession = {
    id: string;
    originalFilename: string;
    expectedSize: number;
    clientMime?: string | null;
    chunkSize: number;
    partCount: number;
    status: UploadStatus;
    expiresAt: string;
    completedParts: Array<number>;
    createdAt: string;
    updatedAt: string;
};

