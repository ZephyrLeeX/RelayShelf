/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
import type { HealthState } from './HealthState';
import type { StorageThresholdState } from './StorageThresholdState';
export type StorageStatus = {
    state: HealthState;
    logicalUsageBytes: number;
    maxStorageBytes: number | null;
    thresholdState: StorageThresholdState;
    nasAvailableBytes: number | null;
    nasTotalBytes: number | null;
    stagingUsageBytes: number;
    stagingAvailableBytes: number | null;
    stagingTotalBytes: number | null;
    degradedReasons: Array<'NAS_UNAVAILABLE' | 'NAS_TIMEOUT' | 'LOGICAL_THRESHOLD_WARNING' | 'LOGICAL_THRESHOLD_EXCEEDED' | 'STAGING_UNAVAILABLE' | 'DATABASE_UNAVAILABLE'>;
};

