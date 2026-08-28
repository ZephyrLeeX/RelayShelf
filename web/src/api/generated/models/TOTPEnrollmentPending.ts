/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type TOTPEnrollmentPending = {
    /**
     * Base32 shared secret returned exactly once during enrollment
     */
    secret: string;
    otpauthUrl: string;
    digits: TOTPEnrollmentPending.digits;
    periodSeconds: TOTPEnrollmentPending.periodSeconds;
    algorithm: TOTPEnrollmentPending.algorithm;
};
export namespace TOTPEnrollmentPending {
    export enum digits {
        '_6' = 6,
        '_8' = 8,
    }
    export enum periodSeconds {
        '_30' = 30,
    }
    export enum algorithm {
        SHA1 = 'SHA1',
    }
}

