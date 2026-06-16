/* tslint:disable */
/* eslint-disable */

/**
 * Returned by add_member: serialized commit + welcome.
 */
export class WasmAddResult {
    private constructor();
    free(): void;
    [Symbol.dispose](): void;
    readonly commit: Uint8Array;
    readonly welcome: Uint8Array;
}

export class WasmEngine {
    free(): void;
    [Symbol.dispose](): void;
    add_member(group_id: string, key_package: Uint8Array): WasmAddResult;
    create_group(group_id: string): void;
    create_group_with_compliance(group_id: string, compliance_kp: Uint8Array): Uint8Array;
    /**
     * Returns plaintext for application messages; empty Vec for commits/proposals.
     */
    decrypt(group_id: string, message: Uint8Array): Uint8Array;
    encrypt(group_id: string, plaintext: Uint8Array): Uint8Array;
    join_from_welcome(welcome: Uint8Array): void;
    key_package_bytes(): Uint8Array;
    constructor(name: string);
}

export type InitInput = RequestInfo | URL | Response | BufferSource | WebAssembly.Module;

export interface InitOutput {
    readonly memory: WebAssembly.Memory;
    readonly __wbg_wasmaddresult_free: (a: number, b: number) => void;
    readonly __wbg_wasmengine_free: (a: number, b: number) => void;
    readonly wasmaddresult_commit: (a: number) => [number, number];
    readonly wasmaddresult_welcome: (a: number) => [number, number];
    readonly wasmengine_add_member: (a: number, b: number, c: number, d: number, e: number) => [number, number, number];
    readonly wasmengine_create_group: (a: number, b: number, c: number) => [number, number];
    readonly wasmengine_create_group_with_compliance: (a: number, b: number, c: number, d: number, e: number) => [number, number, number, number];
    readonly wasmengine_decrypt: (a: number, b: number, c: number, d: number, e: number) => [number, number, number, number];
    readonly wasmengine_encrypt: (a: number, b: number, c: number, d: number, e: number) => [number, number, number, number];
    readonly wasmengine_join_from_welcome: (a: number, b: number, c: number) => [number, number];
    readonly wasmengine_key_package_bytes: (a: number) => [number, number, number, number];
    readonly wasmengine_new: (a: number, b: number) => number;
    readonly __wbindgen_exn_store: (a: number) => void;
    readonly __externref_table_alloc: () => number;
    readonly __wbindgen_externrefs: WebAssembly.Table;
    readonly __wbindgen_free: (a: number, b: number, c: number) => void;
    readonly __wbindgen_malloc: (a: number, b: number) => number;
    readonly __wbindgen_realloc: (a: number, b: number, c: number, d: number) => number;
    readonly __externref_table_dealloc: (a: number) => void;
    readonly __wbindgen_start: () => void;
}

export type SyncInitInput = BufferSource | WebAssembly.Module;

/**
 * Instantiates the given `module`, which can either be bytes or
 * a precompiled `WebAssembly.Module`.
 *
 * @param {{ module: SyncInitInput }} module - Passing `SyncInitInput` directly is deprecated.
 *
 * @returns {InitOutput}
 */
export function initSync(module: { module: SyncInitInput } | SyncInitInput): InitOutput;

/**
 * If `module_or_path` is {RequestInfo} or {URL}, makes a request and
 * for everything else, calls `WebAssembly.instantiate` directly.
 *
 * @param {{ module_or_path: InitInput | Promise<InitInput> }} module_or_path - Passing `InitInput` directly is deprecated.
 *
 * @returns {Promise<InitOutput>}
 */
export default function __wbg_init (module_or_path?: { module_or_path: InitInput | Promise<InitInput> } | InitInput | Promise<InitInput>): Promise<InitOutput>;
