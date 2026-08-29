/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { AdminSecurityStatus } from './AdminSecurityStatus';
import type { BuildInfo } from './BuildInfo';
import type { FailedJob } from './FailedJob';
import type { HealthState } from './HealthState';
import type { MigrationStatus } from './MigrationStatus';
import type { StorageStatus } from './StorageStatus';
export type AdminStatus = {
    state: HealthState;
    build: BuildInfo;
    migration: MigrationStatus;
    databaseState: HealthState;
    failedJobs: Array<FailedJob>;
    storage: StorageStatus;
    security: AdminSecurityStatus;
};

