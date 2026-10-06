// Shapes of the JSON the Go server returns (internal/server, internal/session,
// internal/overview). Keep in sync with the json tags there.
export type ScanState = 'idle' | 'running' | 'finished' | 'cancelled' | 'failed';

export interface Config {
  theme: '' | 'dark' | 'light';
  includeOtherProfiles: boolean;
  exclusions: string[];
}

export interface Status {
  version: string;
  platform: string;
  elevated: boolean;
  port: number;
  state: ScanState;
  sessionCode: string;
  hasOverview: boolean;
  config: Config;
  canRelaunch: boolean;
  firstRun: boolean;
}

export interface Volume {
  root: string;
  name: string;
  label: string;
  fileSystem: string;
  kind: string;
  totalBytes: number;
  freeBytes: number;
  usedBytes: number;
}

export interface LogLine {
  sequence: number;
  time: string;
  kind: string;
  text: string;
  path?: string;
  bytes: number;
}

export interface Progress {
  state: ScanState;
  failure?: string;
  sessionCode: string;
  roots: string[];
  files: number;
  directories: number;
  allocatedBytes: number;
  expectedBytes: number;
  systemBytes: number;
  knownBytes: number;
  unknownBytes: number;
  deniedCount: number;
  linkCount: number;
  placeholders: number;
  currentPath: string;
  elapsedSeconds: number;
  paused: boolean;
  logSequence: number;
  log: LogLine[];
}

export interface OverviewItem {
  label: string;
  bytes: number;
  path?: string;
}

export type TierName = 'unknown' | 'known' | 'system' | 'unaccounted';

export interface TierGroup {
  tier: TierName;
  bytes: number;
  items: OverviewItem[];
}

export interface OverviewRoot {
  path: string;
  display: string;
  isVolumeRoot: boolean;
  fileSystem: string;
  totalBytes: number;
  freeBytes: number;
  usedBytes: number;
  scannedBytes: number;
}

export interface Overview {
  sessionCode: string;
  scannedAt: string;
  durationSeconds: number;
  files: number;
  directories: number;
  roots: OverviewRoot[];
  elevated: boolean;
  partial: boolean;
  deniedCount: number;
  deniedSample: string[] | null;
  totals: {
    system: number;
    known: number;
    unknown: number;
    unaccounted: number;
    free: number;
    capacity: number;
    used: number;
  };
  groups: TierGroup[];
  known: {
    recipesMatched: number;
    cleanupCandidates: number;
    installedAppsBytes: number;
    installedAppsCount: number;
  };
  unknownFolders: number;
  placeholders: number;
  notLocalBytes: number;
  links: number;
}
