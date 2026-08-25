/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { AttachmentSummary } from './AttachmentSummary';
import type { BodyFormat } from './BodyFormat';
import type { Lifecycle } from './Lifecycle';
import type { Tag } from './Tag';
export type Message = {
    id: string;
    body: string | null;
    bodyFormat: BodyFormat;
    detectedType?: string | null;
    detectedLanguage?: string | null;
    sensitive: boolean;
    lifecycle: Lifecycle;
    favorite: boolean;
    expiresAt?: string | null;
    trashedAt?: string | null;
    purgeAt?: string | null;
    sourceUserId?: string | null;
    sourceMessageId?: string | null;
    version: number;
    createdAt: string;
    updatedAt: string;
    tags: Array<Tag>;
    attachments: Array<AttachmentSummary>;
};

