/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { BodyFormat } from './BodyFormat';
export type DirectSendRequest = {
    recipientUserId: string;
    body?: string | null;
    bodyFormat?: BodyFormat;
    sensitive?: boolean;
    uploadIds?: Array<string>;
};

