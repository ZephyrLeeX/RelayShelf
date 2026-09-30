/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { SavedSearchRequest } from './SavedSearchRequest';
export type SavedSearch = (SavedSearchRequest & {
    id: string;
    updatedAt: string;
    /**
     * Nonempty when conditions are no longer valid; do not broaden search
     */
    invalidReason: string;
});

