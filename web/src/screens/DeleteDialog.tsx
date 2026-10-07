import { useCallback, useEffect, useRef, useState } from 'preact/hooks';
import { Kbd } from '../components/Controls';
import { copyText } from '../lib/clipboard';
import {
  deletionApi, errorLog, retryableStatuses, splitPath, statusLabels, type Tone,
} from '../lib/deletion';
import { formatBytes, pluralize } from '../lib/format';
import type { DeletionItem, DeletionPlan, DeletionRun, Tally } from '../lib/types';

interface Props {
  guard: <T>(work: Promise<T>) => Promise<T | undefined>;
  keys: string[];
  volumeName: string;
  onClose: (changed: boolean) => void;
}

const pollMilliseconds = 400;

export function DeleteDialog({ guard, keys, volumeName, onClose }: Props) {
  const [plan, setPlan] = useState<DeletionPlan | null>(null);
  const [run, setRun] = useState<DeletionRun | null>(null);
  const [permanent, setPermanent] = useState(false);
  const [typed, setTyped] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [selectionKeys, setSelectionKeys] = useState(keys);
  const started = useRef(false);
  const primaryButton = useRef<HTMLButtonElement>(null);
  const dialogElement = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setPlan(null);
    setPermanent(false);
    setTyped('');
    void guard(deletionApi.prepare(selectionKeys)).then((loaded) => loaded && setPlan(loaded));
  }, [guard, selectionKeys]);

  // While a run is going, poll its progress; the run happens on the server regardless.
  useEffect(() => {
    if (!run || run.finished) return;
    const timer = window.setTimeout(async () => {
      const next = await guard(deletionApi.progress());
      if (next) setRun(next);
    }, pollMilliseconds);
    return () => window.clearTimeout(timer);
  }, [run, guard]);

  // Focus moves into the dialog, so Enter confirms and the page behind no longer reacts.
  useEffect(() => {
    if (plan) (primaryButton.current ?? dialogElement.current)?.focus();
  }, [plan]);

  const confirmed = !!plan && plan.ready.count > 0 &&
    (!permanent || typed === plan.confirmation);

  const execute = useCallback(async () => {
    if (!plan || !confirmed || busy) return;
    setBusy(true);
    const result = await guard(deletionApi.execute(plan.id, permanent, typed));
    setBusy(false);
    if (result) {
      started.current = true;
      setRun(result);
    }
  }, [plan, confirmed, busy, guard, permanent, typed]);

  const close = useCallback(() => {
    if (run && !run.finished) return;
    onClose(started.current);
  }, [run, onClose]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      if (event.key === 'Escape') {
        event.preventDefault();
        close();
      } else if (event.key === 'Enter' && !run && !permanent && target.tagName !== 'BUTTON' &&
        target.tagName !== 'INPUT') {
        event.preventDefault();
        void execute();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [close, execute, run, permanent]);

  const phase = run ? (run.finished ? 'finished' : 'progress') : 'confirm';
  const items = run ? run.items.concat(plan?.items.filter((item) => item.status !== 'ready')
    ?? []) : plan?.items ?? [];
  const title = phase === 'confirm'
    ? plan ? `Delete ${pluralize(plan.ready.count, 'item')}` : 'Checking the selection'
    : phase === 'progress'
      ? (run!.permanent ? 'Deleting' : 'Moving to the Recycle Bin')
      : run!.failed.count ? 'Finished with failures' : 'Finished';
  const subtitle = phase === 'confirm'
    ? plan ? 'Every item was checked against the disk just now and is checked again right ' +
      'before it is deleted. Anything that changed since the scan is left alone.'
      : `Re-reading ${pluralize(keys.length, 'selected item')} from disk…`
    : phase === 'progress'
      ? 'Each item is verified once more, then removed. You can stop after the current item.'
      : 'This is what happened to each item, checked on disk afterwards.';

  return (
    <div class="dialog-scrim" role="presentation">
      <div class="dialog" role="dialog" aria-modal="true" aria-label={title} tabIndex={-1}
        ref={dialogElement}>
        <div class="dialog-header">
          <div class="dialog-heading">
            <div class="step-label">Step 7 of 7</div>
            <div class="dialog-title">{title}</div>
            <div class="muted">{subtitle}</div>
          </div>
          <button type="button" class="kbd kbd-button" disabled={phase === 'progress'}
            onClick={close} aria-label="Close">Esc</button>
        </div>

        {phase === 'confirm' && plan && (
          <TallyRow cells={[
            { label: 'Ready', tone: 'ok', tally: plan.ready },
            { label: 'Blocked · changed or refused', tone: 'bad',
              tally: { count: plan.changed.count + plan.refused.count,
                bytes: plan.changed.bytes + plan.refused.bytes } },
            { label: 'Missing', tone: 'none', tally: plan.missing },
          ]} />
        )}
        {phase === 'progress' && run && <ProgressBar run={run} />}
        {phase === 'finished' && run && (
          <TallyRow cells={[
            { label: run.permanent ? 'Deleted' : 'Moved to Recycle Bin', tone: 'ok',
              tally: run.removed },
            { label: 'Failed', tone: 'bad', tally: run.failed },
            { label: 'Skipped', tone: 'none', tally: {
              count: run.skipped.count + (plan ? plan.items.length - plan.ready.count : 0),
              bytes: run.skipped.bytes + (plan ? sumBlocked(plan) : 0) } },
          ]} />
        )}

        <div class="dialog-table-head">
          <span>{phase === 'confirm' ? 'Re-verified' : 'Status'}</span>
          <span>Path</span>
          <span class="align-right">Size at scan</span>
        </div>
        <div class="dialog-rows">
          {!plan && <div class="dialog-row muted small">Checking…</div>}
          {items.map((item) => <ItemRow key={item.key} item={item} />)}
        </div>

        {phase === 'confirm' && plan && plan.ready.count > 0 && (
          <div class="dialog-options">
            <div class="option-pair">
              <OptionCard selected={!permanent} onSelect={() => setPermanent(false)}
                title="Move to Recycle Bin" hint="default"
                text={`Restorable until you empty the bin. Space on ${volumeName} is freed ` +
                  'only when you do.'} />
              <OptionCard selected={permanent} danger onSelect={() => setPermanent(true)}
                title="Delete permanently"
                text="Skips the Recycle Bin and frees space now. Cannot be undone." />
            </div>
            {permanent && (
              <label class="confirm-permanent">
                <span>
                  To permanently delete {pluralize(plan.ready.count, 'item')} (
                  {formatBytes(plan.ready.bytes)}), type{' '}
                  <span class="mono strong">{plan.confirmation}</span>
                </span>
                <input class="confirm-input" value={typed} spellcheck={false}
                  placeholder={plan.confirmation} aria-label="Confirmation"
                  onInput={(event) => setTyped((event.target as HTMLInputElement).value)}
                  onKeyDown={(event) => {
                    if (event.key === 'Enter') void execute();
                  }} />
              </label>
            )}
          </div>
        )}

        <div class="dialog-footer">
          {phase === 'confirm' && (
            <>
              <span class="small faint grow">
                {plan && plan.ready.count < plan.items.length
                  ? 'Blocked items are left out. Rescan to include changed items.'
                  : 'Nothing outside the listed items is touched.'}
              </span>
              <button type="button" class="button" onClick={close}>Cancel</button>
              {permanent ? (
                <button type="button" class="button button-danger" disabled={!confirmed || busy}
                  onClick={execute}>
                  Permanently delete {plan ? pluralize(plan.ready.count, 'item') : ''}
                </button>
              ) : (
                <button type="button" class="button button-primary button-with-key"
                  disabled={!confirmed || busy} onClick={execute} ref={primaryButton}>
                  Move {plan ? pluralize(plan.ready.count, 'item') : ''} to Recycle Bin
                  <Kbd inverse>Enter</Kbd>
                </button>
              )}
            </>
          )}
          {phase === 'progress' && run && (
            <>
              <span class="small faint grow">
                Failures don't stop the run; they are collected for review at the end.
              </span>
              <button type="button" class="button" disabled={run.stopping}
                onClick={async () => {
                  const next = await guard(deletionApi.stop());
                  if (next) setRun(next);
                }}>
                {run.stopping ? 'Stopping after this item…' : 'Stop after current item'}
              </button>
            </>
          )}
          {phase === 'finished' && run && (
            <>
              <span class="small muted grow">
                {note || (run.permanent ? 'Space is freed now. Rescan to update the overview.'
                  : `Free space on ${volumeName} changes when you empty the Recycle Bin ` +
                    `(${formatBytes(run.removed.bytes)} waiting).`)}
              </span>
              {errorLog(items) && (
                <button type="button" class="ghost-button" onClick={async () => {
                  const outcome = await copyText(errorLog(items));
                  setNote(outcome === 'copied' ? 'Error log copied.' : 'Copying failed.');
                }}>Copy error log</button>
              )}
              {run.items.some((item) => retryableStatuses.includes(item.status)) && (
                <button type="button" class="button" onClick={() => {
                  setRun(null);
                  setNote('');
                  setSelectionKeys(run.items.filter((item) =>
                    retryableStatuses.includes(item.status)).map((item) => item.key));
                }}>
                  Retry {run.failed.count} failed
                </button>
              )}
              <button type="button" class="button button-primary" onClick={close}>Done</button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function sumBlocked(plan: DeletionPlan): number {
  return plan.items.filter((item) => item.status !== 'ready')
    .reduce((sum, item) => sum + item.bytes, 0);
}

function TallyRow(props: { cells: { label: string; tone: Tone; tally: Tally }[] }) {
  return (
    <div class="tally-row">
      {props.cells.map((cell) => (
        <div class="tally-cell" key={cell.label}>
          <span class={`tally-label tone-text-${cell.tone}`}>
            <span class={`status-dot status-${cell.tone}`} />{cell.label}
          </span>
          <span class="tally-value">
            {cell.tally.count ? `${pluralize(cell.tally.count, 'item')} · ` +
              formatBytes(cell.tally.bytes) : '0'}
          </span>
        </div>
      ))}
    </div>
  );
}

function ProgressBar({ run }: { run: DeletionRun }) {
  const total = run.removed.bytes + run.failed.bytes + run.skipped.bytes + run.pending.bytes;
  const done = total - run.pending.bytes;
  const share = total > 0 ? done / total : 0;
  return (
    <div class="run-progress">
      <div class="between cell-mono">
        <span>{formatBytes(done)} of {formatBytes(total)}</span>
        <span class="faint">
          {run.removed.count} {run.permanent ? 'deleted' : 'moved'} ·{' '}
          {run.failed.count} failed · {run.total - run.done} left
        </span>
      </div>
      <div class="progress-bar">
        <div class="progress-bar-fill" style={{ width: `${Math.round(share * 100)}%` }} />
      </div>
    </div>
  );
}

function ItemRow({ item }: { item: DeletionItem }) {
  const { label, tone } = statusLabels[item.status];
  const [directory, name] = splitPath(item.path || item.key);
  return (
    <div class={`dialog-row${tone === 'bad' ? ' row-bad' : ''}`}>
      <span class="row-status">
        <span class={`status-dot status-${tone}`} />
        <span class={`small tone-text-${tone}`}>{label}</span>
      </span>
      <span class="row-path">
        <span class="mono small ellipsis"><span class="faint">{directory}</span>{name}</span>
        {item.reason && <span class={`small tone-text-${tone} wrap-text`}>{item.reason}</span>}
        {!item.reason && item.categoryName && (
          <span class="small faint ellipsis">{item.categoryName}</span>
        )}
      </span>
      <span class="row-size cell-mono">
        <span>{formatBytes(item.bytes)}</span>
        {item.nowBytes && formatBytes(item.nowBytes) !== formatBytes(item.bytes)
          ? <span class="bad-text small">now {formatBytes(item.nowBytes)}</span> : null}
      </span>
    </div>
  );
}

function OptionCard(props: {
  selected: boolean;
  danger?: boolean;
  title: string;
  hint?: string;
  text: string;
  onSelect: () => void;
}) {
  return (
    <button type="button" role="radio" aria-checked={props.selected}
      class={`option-card${props.selected ? ' selected' : ''}` +
        `${props.danger && props.selected ? ' danger' : ''}`}
      onClick={props.onSelect}>
      <span class={`radio-dot${props.selected ? ' on' : ''}`} />
      <span class="option-card-text">
        <span class="strong">
          {props.title} {props.hint && <span class="small faint normal-weight">{props.hint}</span>}
        </span>
        <span class="small muted">{props.text}</span>
      </span>
    </button>
  );
}
