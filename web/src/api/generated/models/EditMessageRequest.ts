/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { BodyFormat } from './BodyFormat';
export type EditMessageRequest = {
    expectedVersion: number;
    /**
     * Omit to keep the current title; blank or whitespace-only clears it.
     */
    title?: string;
    body?: string | null;
    bodyFormat?: BodyFormat;
    detectedType?: string | null;
    detectedLanguage?: string | null;
};

