import { useEffect, useState } from 'preact/hooks';
import { api } from '../lib/api';
import { formatBytes, formatDateTime, pluralize } from '../lib/format';
import type { HistoryEntry, HistoryView } from '../lib/types';

interface Props {
  guard: <T>(work: Promise<T>) => Promise<T | undefined>;
}

export function HistoryScreen({ guard }: Props) {
  const [view, setView] = useState<HistoryView | null>(null);
  const [confirmingClear, setConfirmingClear] = useState(false);

  useEffect(() => {
    void guard(api.get<HistoryView>('/api/history')).then((loaded) => loaded && setView(loaded));
  }, [guard]);

  const clear = async () => {
    if (!confirmingClear) {
      setConfirmingClear(true);
      return;
    }
    const cleared = await guard(api.post<HistoryView>('/api/history/clear', { confirm: true }));
    if (cleared) setView(cleared);
    setConfirmingClear(false);
  };

  if (!view) return <div class="screen" />;
  const past = view.entries.filter((entry) => entry.code !== view.current).length;

  return (
    <div class="screen screen-wide">
      <div class="screen-header">
        <div class="heading">
          <div class="step-label">Library</div>
          <h1>History</h1>
          <p class="lead">
            Sessions on this PC: dates, drives and sizes only. History holds no file or folder
            names, so it never needs redacting.
          </p>
        </div>
        {view.entries.length > 0 && (
          <div class="actions">
            {confirmingClear && <span class="small warn-text">Click again to clear.</span>}
            <button type="button" class="button" onClick={clear}
              onBlur={() => setConfirmingClear(false)}>
              {confirmingClear ? 'Clear all history' : 'Clear history'}
            </button>
          </div>
        )}
      </div>

      {view.entries.length === 0 ? (
        <div class="placeholder">No sessions yet. Finished scans and deletions appear here.</div>
      ) : (
        <>
          <div class="history-stats">
            <Stat label={view.since ? `Reclaimed since ${formatDay(view.since)}` : 'Reclaimed'}
              value={formatBytes(view.reclaimed)} />
            <Stat label="Past sessions" value={String(past)} />
            <Stat label="Sent to the Recycle Bin" value={formatBytes(view.recycled)}
              note="until the bin is emptied" />
          </div>
          <div class="history-table" role="table" aria-label="Sessions">
            <div class="history-row history-head" role="row">
              <span>Date</span><span>Drives</span><span>Used before → after</span>
              <span class="align-right">Reclaimed</span>
              <span class="align-right">To Recycle Bin</span>
              <span class="align-right">Batches</span><span>Outcome</span>
            </div>
            {view.entries.map((entry) => (
              <div class="history-row" role="row" key={entry.code}>
                <span class="cell-mono">{formatDateTime(entry.started)}</span>
                <span class="cell-mono">{entry.drives.join(' ') || '–'}</span>
                <span class="cell-mono muted">{usage(entry)}</span>
                <span class="cell-mono align-right">
                  {entry.reclaimed ? formatBytes(entry.reclaimed) : '0 B'}
                </span>
                <span class="cell-mono align-right muted">
                  {entry.recycled ? formatBytes(entry.recycled) : '–'}
                </span>
                <span class="cell-mono align-right">{entry.batches || '–'}</span>
                <span class="small history-outcome">
                  {entry.code === view.current && <span class="pill current-pill">current</span>}
                  {outcome(entry, entry.code === view.current)}
                </span>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

function Stat(props: { label: string; value: string; note?: string }) {
  return (
    <div class="history-stat">
      <span class="small muted">{props.label}</span>
      <span class="history-stat-value">{props.value}</span>
      {props.note && <span class="small faint">{props.note}</span>}
    </div>
  );
}

function usage(entry: HistoryEntry): string {
  if (!entry.usedBefore) return '–';
  if (!entry.usedAfter) return formatBytes(entry.usedBefore);
  return `${formatBytes(entry.usedBefore)} → ${formatBytes(entry.usedAfter)}`;
}

function outcome(entry: HistoryEntry, current: boolean): string {
  const failed = entry.failed ? ` · ${entry.failed} failed` : '';
  if (entry.items) return `${pluralize(entry.items, 'item')} deleted${failed}`;
  if (current) return entry.batches ? 'In progress' : 'Scanned';
  if (entry.batches) return 'Ended after import';
  return 'Ended after scan';
}

function formatDay(isoTime: string): string {
  return new Date(isoTime).toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
}
