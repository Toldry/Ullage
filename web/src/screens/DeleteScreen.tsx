import { useCallback, useEffect, useRef, useState } from 'preact/hooks';
import { Kbd } from '../components/Controls';
import { copyText } from '../lib/clipboard';
import {
  deletionApi, errorLog, retryableStatuses, splitPath, statusLabels, type Tone,
} from '../lib/deletion';
import { formatBytes, formatCount, formatDuration, pluralize } from '../lib/format';
import type { DeletionItem, DeletionPlan, DeletionRun, Tally } from '../lib/types';

interface Props {
  guard: <T>(work: Promise<T>) => Promise<T | undefined>;
  keys: string[];
  volumeName: string;
  onDone: (changed: boolean) => void;
}

const pollMilliseconds = 300;

type Phase = 'starting' | 'checking' | 'confirm' | 'deleting' | 'finished';

export function DeleteScreen({ guard, keys, volumeName, onDone }: Props) {
  const [plan, setPlan] = useState<DeletionPlan | null>(null);
  const [run, setRun] = useState<DeletionRun | null>(null);
  const [permanent, setPermanent] = useState(false);
  const [typed, setTyped] = useState('');
  const [note, setNote] = useState('');
  const [busy, setBusy] = useState(false);
  const [selectionKeys, setSelectionKeys] = useState<string[] | null>(null);
  const changed = useRef(false);
  const primaryButton = useRef<HTMLButtonElement>(null);

  // Coming back to this step while a deletion runs shows that run instead of starting over.
  useEffect(() => {
    void guard(deletionApi.progress()).then((current) => {
      if (current && !current.finished && current.total > 0) {
        changed.current = true;
        setRun(current);
      } else {
        setSelectionKeys(keys);
      }
    });
    // The selection is read once, when the step opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [guard]);

  useEffect(() => {
    if (!selectionKeys) return;
    setPlan(null);
    setPermanent(false);
    setTyped('');
    void guard(deletionApi.prepare(selectionKeys)).then((loaded) => loaded && setPlan(loaded));
  }, [guard, selectionKeys]);

  // The server checks items in the background; poll until every one has a verdict.
  useEffect(() => {
    if (!plan?.verifying) return;
    const timer = window.setTimeout(async () => {
      const next = await guard(deletionApi.plan());
      if (next) setPlan(next);
    }, pollMilliseconds);
    return () => window.clearTimeout(timer);
  }, [plan, guard]);

  useEffect(() => {
    if (!run || run.finished) return;
    const timer = window.setTimeout(async () => {
      const next = await guard(deletionApi.progress());
      if (next) setRun(next);
    }, pollMilliseconds);
    return () => window.clearTimeout(timer);
  }, [run, guard]);

  const phase: Phase = run ? (run.finished ? 'finished' : 'deleting')
    : !plan ? 'starting' : plan.verifying ? 'checking' : 'confirm';

  useEffect(() => {
    if (phase === 'confirm') primaryButton.current?.focus();
  }, [phase]);

  const confirmed = phase === 'confirm' && !!plan && plan.ready.count > 0 &&
    (!permanent || typed === plan.confirmation);

  const execute = useCallback(async () => {
    if (!plan || !confirmed || busy) return;
    setBusy(true);
    const result = await guard(deletionApi.execute(plan.id, permanent, typed));
    setBusy(false);
    if (result) {
      changed.current = true;
      setRun(result);
    }
  }, [plan, confirmed, busy, guard, permanent, typed]);

  const leave = useCallback(() => {
    if (phase !== 'deleting') onDone(changed.current);
  }, [phase, onDone]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      if (event.key === 'Escape') {
        event.preventDefault();
        leave();
      } else if (event.key === 'Enter' && phase === 'confirm' && !permanent &&
        target.tagName !== 'BUTTON' && target.tagName !== 'INPUT') {
        event.preventDefault();
        void execute();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [leave, execute, phase, permanent]);

  const items = run ? run.items.concat(plan?.items.filter((item) => item.status !== 'ready')
    ?? []) : plan?.items ?? [];
  const count = selectionKeys?.length ?? keys.length;

  return (
    <div class="screen screen-wide">
      <div class="screen-header">
        <div class="heading">
          <div class="step-label">Step 7 of 7</div>
          <h1>{heading(phase, plan, run, count)}</h1>
          <p class="lead">{lead(phase)}</p>
        </div>
        <div class="actions">
          {phase === 'deleting' && run ? (
            <button type="button" class="button" disabled={run.stopping}
              onClick={async () => {
                const next = await guard(deletionApi.stop());
                if (next) setRun(next);
              }}>
              {run.stopping ? 'Stopping after this item…' : 'Stop after current item'}
            </button>
          ) : (
            <button type="button" class="button button-with-key" onClick={leave}>
              Back to results
              <Kbd>Esc</Kbd>
            </button>
          )}
        </div>
      </div>

      {(phase === 'starting' || phase === 'checking') && (
        <CheckingCard plan={plan} count={count} />
      )}
      {phase === 'deleting' && run && <DeletingCard run={run} />}

      {phase === 'confirm' && plan && (
        <TallyRow cells={[
          { label: 'Ready', tone: 'ok', tally: plan.ready },
          { label: 'Blocked · changed or refused', tone: 'bad',
            tally: { count: plan.changed.count + plan.refused.count,
              bytes: plan.changed.bytes + plan.refused.bytes } },
          { label: 'Missing', tone: 'none', tally: plan.missing },
        ]} />
      )}
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

      <div class="panel delete-items">
        <div class="dialog-table-head">
          <span>{phase === 'deleting' || phase === 'finished' ? 'Status' : 'Re-verified'}</span>
          <span>Path</span>
          <span class="align-right">Size at scan</span>
        </div>
        <div class="dialog-rows">
          {items.length === 0 && <div class="dialog-row muted small">Preparing…</div>}
          {items.map((item) => (
            <ItemRow key={item.key} item={item}
              active={(phase === 'checking' && item.path === plan?.current) ||
                (phase === 'deleting' && item.path === run?.current)} />
          ))}
        </div>
      </div>

      {phase === 'confirm' && plan && plan.ready.count > 0 && (
        <div class="panel delete-options">
          {cloudCount(plan) > 0 && (
            <div class="cloud-note small">
              <span class="strong warn-text">
                {pluralize(cloudCount(plan), 'item')} {cloudCount(plan) === 1 ? 'is' : 'are'}{' '}
                cloud-synced.
              </span>{' '}
              <span class="muted">
                Deleting {cloudCount(plan) === 1 ? 'it' : 'them'} also deletes the cloud copy
                and the copies on your other devices; the cloud service may keep them in its
                own recycle bin for a while.
              </span>
            </div>
          )}
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

      <div class="start-row">
        {phase === 'confirm' && plan && (
          <>
            {permanent ? (
              <button type="button" class="button button-danger" disabled={!confirmed || busy}
                onClick={execute}>
                Permanently delete {pluralize(plan.ready.count, 'item')}
              </button>
            ) : (
              <button type="button" class="button button-primary button-with-key"
                disabled={!confirmed || busy} onClick={execute} ref={primaryButton}>
                Move {pluralize(plan.ready.count, 'item')} to Recycle Bin
                <Kbd inverse>Enter</Kbd>
              </button>
            )}
            <span class="small faint">
              {plan.ready.count < plan.items.length
                ? 'Blocked items are left out. Rescan to include changed items.'
                : 'Nothing outside the listed items is touched.'}
            </span>
          </>
        )}
        {phase === 'deleting' && (
          <span class="small faint">
            Failures don't stop the run; they are collected for review at the end.
          </span>
        )}
        {phase === 'finished' && run && (
          <>
            <button type="button" class="button button-primary" onClick={leave}>
              Done
            </button>
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
            {errorLog(items) && (
              <button type="button" class="ghost-button" onClick={async () => {
                const outcome = await copyText(errorLog(items));
                setNote(outcome === 'copied' ? 'Error log copied.' : 'Copying failed.');
              }}>Copy error log</button>
            )}
            <span class="small muted">
              {note || (run.permanent ? 'Space is freed now. Rescan to update the overview.'
                : `Free space on ${volumeName} changes when you empty the Recycle Bin ` +
                  `(${formatBytes(run.removed.bytes)} waiting).`)}
            </span>
          </>
        )}
      </div>
    </div>
  );
}

function heading(phase: Phase, plan: DeletionPlan | null, run: DeletionRun | null,
  count: number): string {
  switch (phase) {
    case 'starting':
    case 'checking':
      return `Checking ${pluralize(plan?.toCheck ?? count, 'item')}…`;
    case 'confirm':
      return `Delete ${pluralize(plan!.ready.count, 'item')}`;
    case 'deleting':
      return run!.permanent ? 'Deleting…' : 'Moving to the Recycle Bin…';
    default:
      return run!.failed.count ? 'Finished with failures' : 'Finished';
  }
}

function lead(phase: Phase): string {
  switch (phase) {
    case 'starting':
    case 'checking':
      return 'Each selected item is read again from disk and compared with the scan. ' +
        'Anything that changed since then is left alone.';
    case 'confirm':
      return 'Every item was checked against the disk just now and is checked again right ' +
        'before it is deleted.';
    case 'deleting':
      return 'Each item is verified once more, then removed. You can stop after the ' +
        'current item.';
    default:
      return 'This is what happened to each item, checked on disk afterwards.';
  }
}

function CheckingCard({ plan, count }: { plan: DeletionPlan | null; count: number }) {
  const total = plan?.toCheck ?? count;
  const checked = plan?.checked ?? 0;
  const elapsed = plan?.elapsedSeconds ?? 0;
  const share = plan && plan.toCheckBytes > 0 ? plan.checkedBytes / plan.toCheckBytes
    : total > 0 ? checked / total : 0;
  const rate = elapsed > 0 ? (plan?.filesRead ?? 0) / elapsed : 0;
  return (
    <ProgressCard share={share} determinate={!!plan}
      detail={`${formatCount(checked)} of ${pluralize(total, 'item')} checked` +
        (plan ? ` · ${formatBytes(plan.checkedBytes)} of ${formatBytes(plan.toCheckBytes)}`
          : '')}
      elapsed={elapsed}
      stats={[
        { label: 'Items checked', value: `${formatCount(checked)} / ${formatCount(total)}` },
        { label: 'Files re-read', value: formatCount(plan?.filesRead ?? 0) },
        { label: 'Folders re-read', value: formatCount(plan?.foldersRead ?? 0) },
        { label: 'Rate', value: `${formatCount(rate)} files/s` },
      ]}
      currentLabel="Checking" current={plan?.current ?? ''} />
  );
}

function DeletingCard({ run }: { run: DeletionRun }) {
  const total = run.removed.bytes + run.failed.bytes + run.skipped.bytes + run.pending.bytes;
  const done = total - run.pending.bytes;
  const verb = run.permanent ? 'deleted' : 'moved';
  return (
    <ProgressCard share={total > 0 ? done / total : 0} determinate
      detail={`${formatBytes(done)} of ${formatBytes(total)} · ` +
        `${run.done} of ${pluralize(run.total, 'item')} done`}
      elapsed={run.elapsedSeconds}
      stats={[
        { label: run.permanent ? 'Deleted' : 'Moved to bin',
          value: `${formatCount(run.removed.count)} · ${formatBytes(run.removed.bytes)}` },
        { label: 'Failed', value: formatCount(run.failed.count) },
        { label: 'Skipped', value: formatCount(run.skipped.count) },
        { label: 'Left', value: formatCount(run.total - run.done) },
      ]}
      currentLabel={run.permanent ? 'Deleting' : 'Moving'}
      current={run.current ?? (run.stopping ? `stopping, ${verb} so far` : '')} />
  );
}

function ProgressCard(props: {
  share: number;
  determinate: boolean;
  detail: string;
  elapsed: number;
  stats: { label: string; value: string }[];
  currentLabel: string;
  current: string;
}) {
  const percent = Math.floor(Math.min(1, props.share) * 100);
  return (
    <div class="panel panel-raised progress-card">
      <div class="progress-headline">
        <span class="progress-percent">{props.determinate ? `${percent}%` : '…'}</span>
        <span class="mono muted">{props.detail}</span>
        <span class="spacer" />
        <span class="cell-mono faint">{formatDuration(props.elapsed)} elapsed</span>
      </div>
      <div class="progress-bar" role="progressbar" aria-valuemin={0} aria-valuemax={100}
        aria-valuenow={props.determinate ? percent : undefined}>
        {props.determinate ? (
          <div class="progress-bar-fill" style={{ width: `${percent}%` }} />
        ) : (
          <div class="progress-bar-indeterminate" />
        )}
      </div>
      <div class="stats">
        {props.stats.map((stat) => (
          <div class="stat" key={stat.label}>
            <span class="stat-label">{stat.label}</span>
            <span class="stat-value">{stat.value}</span>
          </div>
        ))}
      </div>
      <div class="reading">
        <span class="faint">{props.currentLabel}</span>
        <span class="reading-path">{props.current || '…'}</span>
      </div>
    </div>
  );
}

function cloudCount(plan: DeletionPlan): number {
  return plan.items.filter((item) => item.status === 'ready' && item.cloudSync).length;
}

function sumBlocked(plan: DeletionPlan): number {
  return plan.items.filter((item) => item.status !== 'ready')
    .reduce((sum, item) => sum + item.bytes, 0);
}

function TallyRow(props: { cells: { label: string; tone: Tone; tally: Tally }[] }) {
  return (
    <div class="panel tally-row">
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

function ItemRow({ item, active }: { item: DeletionItem; active: boolean }) {
  const { label, tone } = statusLabels[item.status];
  const [directory, name] = splitPath(item.path || item.key);
  return (
    <div class={`dialog-row${tone === 'bad' ? ' row-bad' : ''}${active ? ' row-active' : ''}`}>
      <span class="row-status">
        <span class={`status-dot status-${active ? 'active' : tone}`} />
        <span class={`small tone-text-${tone}`}>{active ? 'Working…' : label}</span>
      </span>
      <span class="row-path">
        <span class="mono small ellipsis"><span class="faint">{directory}</span>{name}</span>
        {item.reason && <span class={`small tone-text-${tone} wrap-text`}>{item.reason}</span>}
        {!item.reason && item.categoryName && (
          <span class="small faint ellipsis">{item.categoryName}</span>
        )}
        {item.cloudSync && (
          <span class="small warn-text">
            Cloud-synced: also deleted from the cloud and your other devices.
          </span>
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
