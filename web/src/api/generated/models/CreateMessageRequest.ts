/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { BodyFormat } from './BodyFormat';
import type { Lifecycle } from './Lifecycle';
export type CreateMessageRequest = {
    title?: string | null;
    body?: string | null;
    bodyFormat?: BodyFormat;
    lifecycle?: Lifecycle;
    sensitive?: boolean;
    tagIds?: Array<string>;
    uploadIds?: Array<string>;
};

