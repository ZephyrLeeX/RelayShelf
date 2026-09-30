/* generated using openapi-typescript-codegen -- do not edit */
/* istanbul ignore file */
/* tslint:disable */
/* eslint-disable */
export type SearchConditions = {
    'q': string;
    lifecycle: SearchConditions.lifecycle;
    favorite: boolean;
    tagIds: Array<string>;
    type: SearchConditions.type;
    time: SearchConditions.time;
    /**
     * Local YYYY-MM-DDTHH:mm, inclusive start in timezone
     */
    from: string;
    /**
     * Local YYYY-MM-DDTHH:mm, exclusive end in timezone
     */
    to: string;
    timezone: SearchConditions.timezone;
};
export namespace SearchConditions {
    export enum lifecycle {
        ALL = '',
        TEMPORARY = 'TEMPORARY',
        PERMANENT = 'PERMANENT',
    }
    export enum type {
        ALL = '',
        TEXT = 'TEXT',
        MARKDOWN = 'MARKDOWN',
        CODE = 'CODE',
    }
    export enum time {
        ALL = 'all',
        _24H = '24h',
        _7D = '7d',
        _30D = '30d',
        CUSTOM = 'custom',
    }
    export enum timezone {
        ASIA_SHANGHAI = 'Asia/Shanghai',
        UTC = 'UTC',
    }
}

