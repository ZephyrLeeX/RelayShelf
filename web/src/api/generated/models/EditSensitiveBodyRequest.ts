/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type EditSensitiveBodyRequest = {
    expectedVersion: number;
    /**
     * Omit to keep the current title; blank or whitespace-only clears it.
     */
    title?: string;
    body: string;
};

