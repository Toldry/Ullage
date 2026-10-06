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

// Redaction (internal/session/redaction.go)
export type RedactionDecision = 'visible' | 'home' | 'hidden' | 'subtree' | 'keep-extension';
export type RedactionSource = '' | 'rule' | 'session' | 'always' | 'same-name' | 'ancestor';

export interface RedactionSummary {
  available: boolean;
  error?: string;
  storePath: string;
  sessionCode: string;
  rules: number;
  unknownItems: number;
  redacted: number;
  byRules: number;
  bySession: number;
  alwaysRedacted: number;
}

export interface RedactionItem {
  id: number;
  name: string;
  path: string;
  kind: 'folder' | 'file' | 'link';
  bytes: number;
  files: number;
  hasChildren: boolean;
  decision: RedactionDecision;
  source: RedactionSource;
  ruleId?: string;
  rulePattern?: string;
  rendered: string;
  keepWellKnown: boolean;
}

export interface RedactionChildren {
  sessionCode: string;
  items: RedactionItem[];
  moreCount: number;
  moreBytes: number;
  filesBytes: number;
  filesCount: number;
}

export interface RedactionDetail {
  item: RedactionItem;
  preview: RedactionItem[];
}

export type RuleType = 'exact' | 'subtree' | 'glob' | 'name';
export type RuleTreatment = 'subtree' | 'keep-extension';

export interface RuleView {
  id: string;
  type: RuleType;
  pattern: string;
  treatment: RuleTreatment;
  keepWellKnown: boolean;
  created: string;
  lastMatched?: string;
  matches: number;
  matchBytes: number;
  hits: string[];
  inScan: boolean;
}

export interface RuleDraft {
  id?: string;
  type: RuleType;
  pattern?: string;
  treatment: RuleTreatment;
  keepWellKnown: boolean;
  sessionCode?: string;
  fromNode?: number;
}
