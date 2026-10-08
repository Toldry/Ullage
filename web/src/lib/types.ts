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
export type RedactionSource = '' | 'rule' | 'session' | 'always' | 'ancestor';

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

// Export and paste (internal/session/export.go, internal/response)
export interface BatchView {
  number: number;
  firstId: number;
  lastId: number;
  lines: number;
  items: number;
  text: string;
  characters: number;
  tokenEstimate: number;
  imported: boolean;
  categoryCount: number;
  classifiedItems: number;
  reply?: string;
  importedAt?: string;
}

export interface ExportView {
  code: string;
  batches: BatchView[];
  items: number;
  representedBytes: number;
  unknownBytes: number;
  minimumItemBytes: number;
  stale: boolean;
  explanationRounds: ExplanationView[];
  explanation?: ExplanationView;
}

// Round B prompt (internal/session/results.go)
export interface ExplanationView {
  round: number;
  text: string;
  categories: number;
  characters: number;
  tokenEstimate: number;
  imported: boolean;
  reply?: string;
  importedAt?: string;
  explained: number;
  stillQueued: number;
}

export interface ParsedExplanation {
  number: number;
  what: string;
  lost: string;
  steps: string[];
  line: number;
}

export type CheckName = 'session' | 'complete' | 'format' | 'ids' | 'duplicates';

export interface Problem {
  line: number;
  subject: string;
  message: string;
  check: CheckName;
}

export interface ParsedCategory {
  name: string;
  confidence: 'high' | 'med' | 'low';
  regenerable: 'yes' | 'partially' | 'no';
  deletable: boolean;
  ids: number[];
  line: number;
}

export interface PasteResult {
  result: {
    found: boolean;
    foundCode: string;
    truncated: boolean;
    categories: ParsedCategory[];
    problems: Problem[];
    classified: number;
  };
  explanations?: {
    found: boolean;
    foundCode: string;
    truncated: boolean;
    explanations: ParsedExplanation[];
    missing: number[];
    problems: Problem[];
  };
  valid: boolean;
  repair?: string;
  imported: boolean;
  export: ExportView;
}

// Results (internal/results, internal/session/results.go)
export interface ResultStep {
  text: string;
  command?: string;
}

export interface ResultExplanation {
  what: string;
  lost: string;
  steps: ResultStep[];
  note?: string;
  round?: number;
  receivedAt?: string;
}

export interface ResultItem {
  key: string;
  kind: 'file' | 'folder' | 'loose';
  path: string;
  exportedAs?: string;
  bytes: number;
  files: number;
  modified: number;
  cloudSync?: boolean;
  notLocalBytes?: number;
}

export interface ResultCategory {
  key: string;
  number?: number;
  name: string;
  restoredFrom?: string;
  source: 'recipe' | 'ai';
  recipeId?: string;
  recipeVersion?: number;
  batch?: number;
  importedAt?: string;
  confidence: 'recipe' | 'high' | 'med' | 'low';
  regenerable: 'yes' | 'partially' | 'no';
  bytes: number;
  files: number;
  itemCount: number;
  root: string;
  items: ResultItem[];
  moreItems: number;
  moreBytes: number;
  explanation?: ResultExplanation;
  explanationState: 'recipe' | 'none' | 'queued' | 'received';
  bruteForce: { allowed: boolean; reason?: string };
  needsExpand: boolean;
  needsExplanation: boolean;
  cloudSyncItems: number;
  notLocalBytes: number;
  selectable: boolean;
}

export interface ResultsView {
  categories: ResultCategory[];
  kept: ResultCategory[];
  summary: {
    systemBytes: number;
    knownKeptBytes: number;
    keptByAiBytes: number;
    unclassifiedBytes: number;
    reclaimableBytes: number;
  };
  code: string;
  batchCount: number;
  batchesImported: number;
  queuedCount: number;
  explanationRound: number;
  minimumItemBytes: number;
}

// Deletion (internal/session/deletion.go, internal/deletion)
export type DeletionStatus =
  | 'pending' | 'ready' | 'changed' | 'missing' | 'protected' | 'rejected'
  | 'recycled' | 'deleted' | 'failed' | 'partial' | 'skipped';

export interface DeletionItem {
  key: string;
  category: string;
  categoryName: string;
  path: string;
  kind: string;
  bytes: number;
  status: DeletionStatus;
  reason?: string;
  nowBytes?: number;
  cloudSync?: boolean;
}

export interface Tally {
  count: number;
  bytes: number;
}

export interface DeletionPlan {
  id: string;
  items: DeletionItem[];
  ready: Tally;
  changed: Tally;
  missing: Tally;
  refused: Tally;
  confirmation: string;
  verifying: boolean;
  toCheck: number;
  checked: number;
  toCheckBytes: number;
  checkedBytes: number;
  filesRead: number;
  foldersRead: number;
  current?: string;
  elapsedSeconds: number;
}

export interface DeletionRun {
  permanent: boolean;
  total: number;
  done: number;
  stopping: boolean;
  finished: boolean;
  items: DeletionItem[];
  removed: Tally;
  failed: Tally;
  skipped: Tally;
  pending: Tally;
  current?: string;
  elapsedSeconds: number;
}

// History (internal/history, internal/session/history.go)
export interface HistoryEntry {
  code: string;
  started: string;
  finished: string;
  drives: string[];
  usedBefore: number;
  usedAfter: number;
  batches: number;
  items: number;
  recycled: number;
  deleted: number;
  failed: number;
  reclaimed: number;
}

export interface HistoryView {
  entries: HistoryEntry[];
  current: string;
  since?: string;
  reclaimed: number;
  recycled: number;
}
