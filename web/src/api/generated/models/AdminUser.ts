/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type AdminUser = {
    id: string;
    username: string;
    displayName: string;
    isAdmin: boolean;
    status: AdminUser.status;
    createdAt: string;
    updatedAt: string;
};
export namespace AdminUser {
    export enum status {
        ACTIVE = 'ACTIVE',
        DISABLED = 'DISABLED',
    }
}

