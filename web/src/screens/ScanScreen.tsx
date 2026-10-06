import { useState } from 'preact/hooks';
import { Checkbox, Kbd } from '../components/Controls';
import { estimateScanMinutes, formatBytes, formatDateTime } from '../lib/format';
import type { Config, Volume } from '../lib/types';

export interface ScanSelection {
  roots: string[];
  includeOtherProfiles: boolean;
  exclusions: string[];
}

interface Props {
  volumes: Volume[];
  config: Config;
  elevated: boolean;
  canRelaunch: boolean;
  firstRun: boolean;
  platform: string;
  lastScan?: { at: string; roots: string; usedBytes: number };
  error: string;
  selected: Set<string>;
  onToggleVolume: (root: string) => void;
  onStart: (selection: ScanSelection) => void;
  onRelaunch: () => void;
  onConfigChange: (config: Config) => void;
}

export function ScanScreen(props: Props) {
  const [newExclusion, setNewExclusion] = useState('');
  const chosen = props.volumes.filter((volume) => props.selected.has(volume.root));
  const minutes = chosen.reduce((sum, volume) => sum + estimateScanMinutes(volume.usedBytes), 0);
  const startMeta = chosen.length
    ? `${chosen.map((volume) => volume.name).join(' ')} · about ${minutes} min · metadata only`
    : 'Select at least one drive';
  const exclusions = props.config.exclusions;

  const start = () =>
    props.onStart({
      roots: chosen.map((volume) => volume.root),
      includeOtherProfiles: props.config.includeOtherProfiles,
      exclusions,
    });

  const addExclusion = () => {
    const pattern = newExclusion.trim();
    if (pattern && !exclusions.includes(pattern)) {
      props.onConfigChange({ ...props.config, exclusions: [...exclusions, pattern] });
    }
    setNewExclusion('');
  };

  return (
    <div class="screen">
      <div class="heading">
        <div class="step-label">Step 1 of 7</div>
        <h1>{props.firstRun ? 'Choose what to scan' : 'Scan'}</h1>
        <p class="lead">
          Ullage reads file metadata on this PC: names, sizes and timestamps. It never opens
          file contents, and nothing leaves the machine during a scan.
        </p>
      </div>

      {props.firstRun && (
        <div class="intro">
          <div class="intro-cell">
            <div class="step-label">1 · on this PC</div>
            <div class="title">Scan metadata</div>
            <div class="text">
              Built-in recipes recognize Docker, WSL, NuGet, npm and more without any AI.
            </div>
          </div>
          <div class="intro-cell">
            <div class="step-label">2 · you decide</div>
            <div class="title">Review what leaves</div>
            <div class="text">
              Unrecognized paths go into a prompt you copy yourself. Redact any name first.
            </div>
          </div>
          <div class="intro-cell">
            <div class="step-label">3 · reversible by default</div>
            <div class="title">Delete with re-checks</div>
            <div class="text">
              Every item is re-verified first and goes to the Recycle Bin unless you choose
              otherwise.
            </div>
          </div>
        </div>
      )}

      {props.elevated ? (
        <div class="banner banner-plain">
          <span class="dot dot-ok" />
          <span>
            <strong>Running as administrator.</strong> Full scan: protected folders and other
            users' profiles are readable.
          </span>
        </div>
      ) : (
        <div class="banner banner-warn">
          <div class="banner-text">
            <div class="banner-title">Not running as administrator: this scan will be partial</div>
            <div class="muted">
              {props.platform === 'windows' ? (
                <>
                  Ullage can't read{' '}
                  <span class="inline-path">C:/Program Files/WindowsApps</span>,{' '}
                  <span class="inline-path">C:/System Volume Information</span> or other users'
                  profiles. The space they use is shown as one Unaccounted line so totals still
                  add up. Relaunching shows a Windows UAC prompt and opens a new tab.
                </>
              ) : (
                <>Folders you cannot read are listed after the scan.</>
              )}
            </div>
          </div>
          {props.canRelaunch && (
            <button type="button" class="button" onClick={props.onRelaunch}>
              Relaunch as administrator
            </button>
          )}
        </div>
      )}

      <div class="panel">
        <div class="drive-grid drive-head">
          <span />
          <span>Drive</span>
          <span>Label</span>
          <span>FS</span>
          <span>Used</span>
          <span class="align-right">Free</span>
          <span class="align-right">Est. time</span>
        </div>
        {props.volumes.map((volume) => {
          const isSelected = props.selected.has(volume.root);
          const usedShare = volume.totalBytes ? volume.usedBytes / volume.totalBytes : 0;
          const lowSpace = usedShare > 0.9;
          return (
            <div
              key={volume.root}
              class="drive-grid drive-row"
              onClick={() => props.onToggleVolume(volume.root)}
            >
              <Checkbox
                state={isSelected ? 'on' : 'off'}
                label={`Scan ${volume.name}`}
                onToggle={() => props.onToggleVolume(volume.root)}
              />
              <span class="drive-letter">{volume.name}</span>
              <span class="drive-label">
                <span>{volume.label || 'Local Disk'}</span>
                <span class="drive-kind">{volume.kind}</span>
              </span>
              <span class="cell-mono muted">{volume.fileSystem}</span>
              <span class="drive-used">
                <span class="meter">
                  <MeterFill share={usedShare} warn={lowSpace} />
                </span>
                <span class="cell-mono">
                  {formatBytes(volume.usedBytes)} / {formatBytes(volume.totalBytes)}
                </span>
              </span>
              <span class={`cell-mono align-right ${lowSpace ? 'warn-text' : 'muted'}`}>
                {formatBytes(volume.freeBytes)}
              </span>
              <span class="cell-mono align-right faint">
                ~{estimateScanMinutes(volume.usedBytes)} min
              </span>
            </div>
          );
        })}
        {props.volumes.length === 0 && (
          <div class="panel-note">No local drives found. Network drives are never scanned.</div>
        )}
        <div class="panel-note">
          WSL distros and Docker Desktop live inside .vhdx files on C:. They are measured as
          files, never mounted or opened. Network drives are not listed: scanning them would
          send traffic over the network.
        </div>
      </div>

      <div class="options">
        <div class="option">
          <Checkbox state="on" label="Skip junctions and reparse points (always on)" disabled />
          <div class="option-text">
            <span class="option-title">Skip junctions and links</span>
            <span class="option-detail">
              Always on. Links are listed with their target but never followed, so nothing is
              counted twice.
            </span>
          </div>
        </div>
        <div class="option">
          <Checkbox
            state={props.config.includeOtherProfiles ? 'on' : 'off'}
            label="Include other users' profiles"
            disabled={!props.elevated}
            onToggle={() =>
              props.onConfigChange({
                ...props.config,
                includeOtherProfiles: !props.config.includeOtherProfiles,
              })
            }
          />
          <div class="option-text">
            <span class="option-title">Include other users' profiles</span>
            <span class="option-detail">
              {props.elevated
                ? 'Off: only your own profile is read.'
                : 'Needs administrator. Only your own profile is read.'}
            </span>
          </div>
        </div>
        <div class="option">
          <Checkbox state={exclusions.length ? 'on' : 'off'} label="Never scan these" disabled />
          <div class="option-text">
            <span class="option-title">Never scan these folders ({exclusions.length})</span>
            <div class="chips">
              {exclusions.map((pattern) => (
                <span class="chip" key={pattern}>
                  {pattern}
                  <button
                    type="button"
                    class="chip-remove"
                    aria-label={`Remove ${pattern}`}
                    onClick={() =>
                      props.onConfigChange({
                        ...props.config,
                        exclusions: exclusions.filter((existing) => existing !== pattern),
                      })
                    }
                  >
                    ×
                  </button>
                </span>
              ))}
              <input
                class="chip-input"
                placeholder="+ ~/vault/** or name"
                aria-label="Add a folder pattern to never scan"
                value={newExclusion}
                onInput={(event) => setNewExclusion((event.target as HTMLInputElement).value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter') {
                    event.preventDefault();
                    event.stopPropagation();
                    addExclusion();
                  }
                }}
                onBlur={addExclusion}
              />
            </div>
          </div>
        </div>
      </div>

      <div class="start-row">
        <button
          type="button"
          class="button button-primary button-large"
          disabled={chosen.length === 0}
          onClick={start}
        >
          Start scan
          <Kbd inverse>Enter</Kbd>
        </button>
        <span class="cell-mono muted">{startMeta}</span>
        {props.error && <span class="error-text">{props.error}</span>}
        <div class="spacer" />
        {props.lastScan ? (
          <span class="faint">
            Last scan: {formatDateTime(props.lastScan.at)} · {props.lastScan.roots} ·{' '}
            {formatBytes(props.lastScan.usedBytes)} used
          </span>
        ) : (
          props.firstRun && <span class="faint">No previous sessions on this PC</span>
        )}
      </div>
    </div>
  );
}

// Width is set through the CSSOM (allowed by the CSP), never through a style attribute.
export function MeterFill(props: { share: number; warn?: boolean }) {
  return (
    <span
      class={`meter-fill${props.warn ? ' full' : ''}`}
      style={{ width: `${Math.min(100, Math.max(0, props.share * 100))}%` }}
    />
  );
}
