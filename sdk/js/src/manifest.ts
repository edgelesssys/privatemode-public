/** Policy entry for a workload in the manifest. */
export interface ManifestPolicy {
  /** Subject Alternative Names for the workload's certificate. */
  SANs: string[];
  /** Identifier for the workload's secret. */
  WorkloadSecretID: string;
  /** Optional role assigned to the workload. */
  Role?: string;
}

/** Minimum TCB (Trusted Computing Base) version requirements. */
export interface MinimumTCB {
  BootloaderVersion: number;
  TEEVersion: number;
  SNPVersion: number;
  MicrocodeVersion: number;
}

/** Guest policy settings for SNP. */
export interface GuestPolicy {
  SMT: boolean;
  MigrateMA: boolean;
  Debug: boolean;
  CXLAllowed: boolean;
  PageSwapDisable: boolean;
}

/** Platform information for SNP. */
export interface PlatformInfo {
  SMTEnabled: boolean;
  ECCEnabled: boolean;
  AliasCheckComplete: boolean;
}

/** SNP reference value entry. */
export interface SNPReferenceValue {
  ProductName: string;
  TrustedMeasurement: string;
  MinimumTCB: MinimumTCB;
  GuestPolicy: GuestPolicy;
  PlatformInfo: PlatformInfo;
  AllowedChipIDs: string[];
}

/** Reference values for attestation verification. */
export interface ReferenceValues {
  snp?: SNPReferenceValue[];
}

/** Manifest describing the Privatemode deployment. */
export interface Manifest {
  /** Policies keyed by their hash. */
  Policies?: Record<string, ManifestPolicy>;
  /** Reference values for attestation verification. */
  ReferenceValues?: ReferenceValues;
  /** Public keys of seedshare owners. */
  SeedshareOwnerPubKeys?: string[];
}
