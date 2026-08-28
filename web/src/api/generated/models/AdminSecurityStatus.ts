/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type AdminSecurityStatus = {
    activeAdmins: number;
    activeAdminsWithoutTOTP: number;
    /**
     * True when at least one active admin exists and every active admin has confirmed TOTP
     */
    adminTotpSatisfied: boolean;
};

