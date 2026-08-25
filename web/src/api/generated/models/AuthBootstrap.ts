/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { Device } from './Device';
import type { Session } from './Session';
import type { User } from './User';
export type AuthBootstrap = {
    user: User;
    device: Device;
    session: Session;
    csrfToken: string;
};

