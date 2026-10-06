import { Kbd, Swatch, type Fill } from '../components/Controls';
import {
  formatBytes,
  formatCount,
  formatDateTime,
  formatLongDuration,
  pluralize,
} from '../lib/format';
import { squarify } from '../lib/treemap';
import type { Overview, TierName } from '../lib/types';

interface Props {
  overview: Overview;
  canRelaunch: boolean;
  onRescan: () => void;
  onRelaunch: () => void;
  onReview: () => void;
}

const tierLabels: Record<TierName, string> = {
  unknown: 'Unknown',
  known: 'Known',
  system: 'System',
  unaccounted: 'Unaccounted',
};

export function OverviewScreen(
  { overview, canRelaunch, onRescan, onRelaunch, onReview }: Props) {
  const totals = overview.totals;
  const rootNames = overview.roots.map((root) => root.display).join(' ');
  const partialBanner = overview.partial && (totals.unaccounted > 0 || overview.deniedCount > 0);

  return (
    <div class="screen screen-wide">
      <div class="screen-header">
        <div class="heading">
          <div class="step-label">Step 2 of 7</div>
          <h1>Overview</h1>
          <p class="lead lead-mono">
            {rootNames} · scanned {formatDateTime(overview.scannedAt)} in{' '}
            {formatLongDuration(overview.durationSeconds)} · {formatCount(overview.files)} files
          </p>
        </div>
        <div class="actions">
          <button type="button" class="button" onClick={onRescan}>Rescan</button>
          <button
            type="button"
            class="button button-primary button-with-key"
            onClick={onReview}
          >
            Review unknown items
            <Kbd inverse>3</Kbd>
          </button>
        </div>
      </div>

      {partialBanner && (
        <div class="banner banner-warn">
          <div class="banner-text">
            <div class="banner-title">
              Partial scan: up to {formatBytes(totals.unaccounted)} couldn't be read
            </div>
            <div class="muted">
              Ullage ran without administrator rights and skipped{' '}
              {pluralize(overview.deniedCount, 'folder')}
              {overview.deniedSample?.length
                ? `, including ${overview.deniedSample.slice(0, 3).join(', ')}`
                : ''}
              . The volume's used space is still counted, as one Unaccounted line.
            </div>
          </div>
          {canRelaunch && (
            <button type="button" class="button" onClick={onRelaunch}>
              Relaunch as administrator
            </button>
          )}
        </div>
      )}

      <UsageBar overview={overview} />

      <div class="overview-grid">
        <Treemap overview={overview} />
        <div class="side-column">
          <Ledger overview={overview} />
          <WhatLeaves />
          <Notes overview={overview} />
        </div>
      </div>
    </div>
  );
}

function UsageBar({ overview }: { overview: Overview }) {
  const totals = overview.totals;
  const capacity = Math.max(totals.capacity, 1);
  const freeShare = totals.free / capacity;
  const segments: { fill: Fill; bytes: number }[] = [
    { fill: 'system', bytes: totals.system },
    { fill: 'known', bytes: totals.known },
    { fill: 'unknown', bytes: totals.unknown },
    { fill: 'unaccounted', bytes: totals.unaccounted },
    { fill: 'free', bytes: totals.free },
  ];
  return (
    <div class="usage">
      <div class="usage-headline">
        <span class="usage-total">{formatBytes(totals.used)} used</span>
        <span class="cell-mono faint">of {formatBytes(totals.capacity)}</span>
        <span class="spacer" />
        {totals.free > 0 && (
          <span class={`cell-mono ${freeShare < 0.1 ? 'warn-text' : 'faint'}`}>
            {formatBytes(totals.free)} headroom · {(freeShare * 100).toFixed(1)}% free
          </span>
        )}
      </div>
      <div class="tier-bar">
        {segments
          .filter((segment) => segment.bytes > 0)
          .map((segment) => (
            <div
              key={segment.fill}
              class={`tier-bar-segment fill-${segment.fill}`}
              style={{ width: `${(segment.bytes / capacity) * 100}%` }}
            >
              {segment.bytes / capacity > 0.04 ? formatBytes(segment.bytes) : ''}
            </div>
          ))}
      </div>
      <div class="legend">
        <span class="legend-item"><Swatch fill="system" small />System, not analyzed</span>
        <span class="legend-item"><Swatch fill="known" small />Known: built-in recipes, no AI</span>
        <span class="legend-item">
          <Swatch fill="unknown" small />Unknown: will be sent to the AI
        </span>
        {totals.unaccounted > 0 && (
          <span class="legend-item"><Swatch fill="unaccounted" small />Unaccounted</span>
        )}
        {totals.free > 0 && (
          <span class="legend-item"><Swatch fill="free" small />Headroom (free)</span>
        )}
      </div>
    </div>
  );
}

// Layout happens in a fixed coordinate space and is rendered as percentages, so the
// treemap scales with the panel width.
const mapWidth = 760;
const mapHeight = 436;

function Treemap({ overview }: { overview: Overview }) {
  const groups = overview.groups
    .filter((group) => group.bytes > 0)
    .map((group) => ({ ...group, value: group.bytes }));
  const percent = (value: number, whole: number) => `${(value / whole) * 100}%`;
  const placedGroups = squarify(groups, { x: 0, y: 0, width: mapWidth, height: mapHeight });

  return (
    <div class="treemap" aria-label="Disk usage treemap">
      <div class="treemap-inner">
        {placedGroups.map((group) => {
          const items = group.items
            .filter((item) => item.bytes > 0)
            .sort((first, second) => second.bytes - first.bytes)
            .map((item) => ({ ...item, value: item.bytes }));
          const placedItems = squarify(items, {
            x: group.x + 1,
            y: group.y + 19,
            width: group.width - 2,
            height: group.height - 20,
          });
          return [
            <div
              key={`${group.tier}-label`}
              class="treemap-tier-label"
              style={{
                left: percent(group.x, mapWidth),
                top: percent(group.y, mapHeight),
                width: percent(group.width, mapWidth),
              }}
            >
              <Swatch fill={group.tier} />
              {tierLabels[group.tier]} · {formatBytes(group.bytes)}
            </div>,
            ...placedItems.map((item, index) => (
              <div
                key={`${group.tier}-${index}`}
                class={`treemap-cell fill-${group.tier}`}
                title={`${item.label} · ${formatBytes(item.bytes)}`}
                style={{
                  left: percent(item.x + 1, mapWidth),
                  top: percent(item.y + 1, mapHeight),
                  width: `calc(${percent(item.width, mapWidth)} - 2px)`,
                  height: `calc(${percent(item.height, mapHeight)} - 2px)`,
                }}
              >
                {item.width > 72 && item.height > 38 && (
                  <>
                    <div class="treemap-name">{item.label}</div>
                    <div class="treemap-size">{formatBytes(item.bytes)}</div>
                  </>
                )}
              </div>
            )),
          ];
        })}
      </div>
    </div>
  );
}

function Ledger({ overview }: { overview: Overview }) {
  const { totals, known } = overview;
  const rows: { fill: Fill; label: string; bytes: number; sub: string }[] = [
    {
      fill: 'system',
      label: 'System, not analyzed',
      bytes: totals.system,
      sub: 'Windows, Program Files and boot files. Never sent anywhere, never offered ' +
        'for deletion.',
    },
    {
      fill: 'known',
      label: 'Known · built-in recipes',
      bytes: totals.known,
      sub:
        `${pluralize(known.recipesMatched, 'recipe')} matched. ` +
        `${formatBytes(known.cleanupCandidates)} are cleanup candidates` +
        (known.installedAppsCount
          ? `; ${formatBytes(known.installedAppsBytes)} of installed apps are kept.`
          : '.'),
    },
    {
      fill: 'unknown',
      label: 'Unknown · goes to the AI',
      bytes: totals.unknown,
      sub:
        `${pluralize(overview.unknownFolders, 'folder')} with files, ` +
        'sent only after your redaction review.',
    },
  ];
  if (totals.unaccounted > 0) {
    rows.push({
      fill: 'unaccounted',
      label: overview.partial ? 'Unaccounted · not elevated' : 'Unaccounted',
      bytes: totals.unaccounted,
      sub: overview.partial
        ? 'Used space the volume reports but Ullage could not open, plus file-system ' +
          'metadata. Relaunch as administrator to include more of it.'
        : 'Space the volume reports as used that no scanned folder holds: file-system ' +
          'metadata, excluded folders, other profiles, or data behind mount points.',
    });
  }
  return (
    <div class="panel">
      <div class="panel-header"><span>Tier</span><span>Size</span></div>
      {rows.map((row) => (
        <div class="ledger-row" key={row.fill}>
          <Swatch fill={row.fill} />
          <span class="ledger-label">{row.label}</span>
          <span class="mono align-right">{formatBytes(row.bytes)}</span>
          <span class="ledger-sub">{row.sub}</span>
        </div>
      ))}
      <div class="ledger-total">
        <span>Total used</span>
        <span class="mono">{formatBytes(totals.used)}</span>
      </div>
    </div>
  );
}

function WhatLeaves() {
  const sent = ['Paths, after redaction', 'Rounded sizes', 'File counts'];
  const never = ['File contents: never', 'Username: replaced by ~', 'Machine name: never'];
  return (
    <div class="panel leaves">
      <div class="banner-title">What leaves this machine</div>
      <div class="option-detail">
        Only Unknown items, only after your review, only through your clipboard.
      </div>
      <div class="leaves-grid">
        {sent.map((text, index) => [
          <span class="leaves-item" key={text}><span class="leaves-dot" />{text}</span>,
          <span class="leaves-item never" key={never[index]}>
            <span class="leaves-dot" />{never[index]}
          </span>,
        ])}
      </div>
    </div>
  );
}

function Notes({ overview }: { overview: Overview }) {
  const notes: string[] = [];
  if (overview.placeholders > 0) {
    notes.push(
      `${pluralize(overview.placeholders, 'cloud-only placeholder')}: ` +
        `${formatBytes(overview.notLocalBytes)} not stored locally, counted at their on-disk size.`,
    );
  }
  if (overview.links > 0) {
    notes.push(`${pluralize(overview.links, 'junction or link', 'junctions or links')} ` +
      'recorded but not followed.');
  }
  if (!notes.length) return null;
  return (
    <div class="notes">
      {notes.map((note) => <span key={note}>{note}</span>)}
    </div>
  );
}
