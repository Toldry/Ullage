import { useEffect, useRef, useState } from 'preact/hooks';
import { Kbd } from '../components/Controls';
import { SessionExpiredError } from '../lib/api';
import { copyText } from '../lib/clipboard';
import { exportApi } from '../lib/exporting';
import { roundsOf } from '../lib/results';
import { formatBytes, formatCount, pluralize } from '../lib/format';
import { placeholderSegments } from '../lib/redaction';
import type { ExportView } from '../lib/types';

interface Props {
  guard: <T>(work: Promise<T>) => Promise<T | undefined>;
  batch: number;
  onBatchChange: (batch: number) => void;
  onGoPaste: () => void;
  onExportLoaded: (view: ExportView) => void;
}

export function ExportScreen({ guard, batch, onBatchChange, onGoPaste, onExportLoaded }: Props) {
  const [view, setView] = useState<ExportView | null>(null);
  const [copyNote, setCopyNote] = useState('');
  const [error, setError] = useState('');
  const textElement = useRef<HTMLDivElement>(null);

  useEffect(() => {
    (async () => {
      try {
        const loaded = await exportApi.get();
        setView(loaded);
        onExportLoaded(loaded);
      } catch (failure) {
        // A rejected session switches the whole app to the invalid-link state; any other
        // failure (for example unreadable redaction rules) is explained here.
        if (failure instanceof SessionExpiredError) await guard(Promise.reject(failure));
        else setError(failure instanceof Error ? failure.message : String(failure));
      }
    })();
  }, [guard, onExportLoaded]);

  const rounds = view ? roundsOf(view) : [];
  const current = rounds[Math.min(batch, rounds.length) - 1];
  const explaining = current?.kind === 'explain';

  const copy = async () => {
    if (!current) return;
    const outcome = await copyText(current.text, textElement.current);
    const time = new Date().toTimeString().slice(0, 8);
    setCopyNote(outcome === 'copied'
      ? `Copied at ${time}. Ullage never sends it anywhere.`
      : outcome === 'selected'
        ? 'The browser blocked the clipboard. The text is selected: press Ctrl+C.'
        : 'Copying failed. Select the text and press Ctrl+C.');
  };

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const selectionEmpty = (window.getSelection()?.toString() ?? '') === '';
      if ((event.ctrlKey || event.metaKey) && event.key === 'c' && selectionEmpty) {
        event.preventDefault();
        void copy();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  const rebuild = async () => {
    const rebuilt = await guard(exportApi.rebuild());
    if (rebuilt) {
      setView(rebuilt);
      onExportLoaded(rebuilt);
      onBatchChange(1);
    }
  };

  if (error && !view) {
    return (
      <div class="screen">
        <ExportHeading />
        <div class="banner banner-bad">
          <div class="banner-text">
            <div class="banner-title">No prompt can be built</div>
            <div class="muted">{error}</div>
          </div>
        </div>
      </div>
    );
  }
  if (!view || !current) return <div class="screen" />;

  const lines = current.text.replace(/\n$/, '').split('\n');
  const totalBatches = view.batches.length;

  return (
    <div class="screen screen-wide">
      <div class="screen-header">
        <ExportHeading />
        <BatchStepper count={rounds.length} current={current.number}
          label={explaining ? 'Explanations' : undefined}
          done={(number) => rounds[number - 1].imported} onPick={onBatchChange} />
      </div>

      {view.stale && (
        <div class="banner banner-warn">
          <div class="banner-text">
            <div class="banner-title">Redactions changed after a reply was imported</div>
            <div class="muted">
              This prompt still reflects the old redactions. Rebuild it to apply them; replies
              already imported are discarded and the new prompt gets a new session code.
            </div>
          </div>
          <button type="button" class="button" onClick={rebuild}>Rebuild prompt</button>
        </div>
      )}

      <div class="export-grid">
        <div class="panel">
          <div class="panel-header export-header">
            <span>Exact text to be copied</span>
            <span class="cell-mono faint normal-case">
              {explaining ? pluralize(current.items, 'category', 'categories')
                : `${formatCount(current.items)} items`} ·{' '}
              {formatCount(current.characters)} chars ·
              ≈{formatTokens(current.tokenEstimate)} tokens
            </span>
            <span class="spacer" />
            <span class="legend-item normal-case">
              <span class="placeholder-swatch" />your redactions
            </span>
            <span class="faint normal-case">read-only</span>
          </div>
          <div class="prompt-text" ref={textElement}>
            {lines.map((line, index) => (
              <div class="prompt-line" key={index}>
                <span class="line-number">{index + 1}</span>
                <span class="line-text">
                  {placeholderSegments(line || ' ').map((segment, part) => (
                    <span key={part} class={segment.placeholder ? 'placeholder-token' : ''}>
                      {segment.text}
                    </span>
                  ))}
                </span>
              </div>
            ))}
          </div>
        </div>

        <div class="side-column">
          <div class="panel panel-raised copy-card">
            <button type="button" class="button button-primary button-large copy-button"
              onClick={copy}>
              {explaining ? 'Copy explanation prompt'
                : current.number === 1 ? 'Copy prompt' : `Copy batch ${current.number}`}
              <Kbd inverse>Ctrl C</Kbd>
            </button>
            <div class="small faint">
              {copyNote || 'This text is the only thing that leaves your machine, and only ' +
                'through your clipboard.'}
            </div>
          </div>
          <div class="panel steps-card">
            <div class="section-title">Then</div>
            {explaining ? (
              <>
                <Step number={1}
                  text="Paste this into the same conversation as the batches." />
                <Step number={2} text="Copy the reply and paste it in step 5." />
                <div class="small muted">
                  It asks for short explanations of the categories you requested on the
                  Results screen. Only their numbers, the assistant's own names and IDs are
                  in it.
                </div>
              </>
            ) : current.number === 1 ? (
              <>
                <Step number={1}
                  text="Open a new conversation in any assistant: Claude, ChatGPT, or another." />
                <Step number={2} text="Paste and send. Copy the whole reply." />
                <Step number={3} text="Come back and paste it in step 5." />
                {totalBatches > 1 && (
                  <div class="small muted">
                    Paste batches 2 to {totalBatches} into the same conversation. They rely
                    on the instructions in batch 1.
                  </div>
                )}
              </>
            ) : (
              <>
                <Step number={1} text="Paste this into the same conversation as batch 1." />
                <Step number={2} text="Copy the reply and paste it in step 5." />
              </>
            )}
          </div>
          <div class="panel">
            {rounds.map((item) => (
              <button type="button" key={item.number}
                class={`batch-row${item.number === current.number ? ' active' : ''}`}
                onClick={() => onBatchChange(item.number)}>
                <span>
                  {item.kind === 'explain' ? `Round ${item.number}` : `Batch ${item.number}`}
                </span>
                <span class="cell-mono faint">
                  {item.kind === 'explain' ? `${formatCount(item.items)} to explain`
                    : `${formatCount(item.items)} items`}
                </span>
                <span class={`small ${item.imported ? 'ok-text' : 'faint'}`}>
                  {item.imported ? `imported · ${item.categoryCount} categories`
                    : item.number === current.number ? 'ready to copy' : 'not imported'}
                </span>
              </button>
            ))}
          </div>
          <div class="small muted">
            Session code <span class="mono strong">{view.code}</span> is in the prompt. Replies
            must echo it; replies to older prompts are rejected. Items under{' '}
            {formatBytes(view.minimumItemBytes)} are not listed:{' '}
            {formatBytes(view.unknownBytes - view.representedBytes)} of{' '}
            {formatBytes(view.unknownBytes)} Unknown.
          </div>
          <button type="button" class="button button-primary button-with-key"
            onClick={onGoPaste}>
            I have the reply: paste response
            <Kbd inverse>5</Kbd>
          </button>
        </div>
      </div>
    </div>
  );
}

function ExportHeading() {
  return (
    <div class="heading">
      <div class="step-label">Step 4 of 7</div>
      <h1>Export prompt</h1>
      <p class="lead">
        This text is the only thing that leaves your machine, and it leaves through your
        clipboard. Ullage never sends it anywhere.
      </p>
    </div>
  );
}

function Step(props: { number: number; text: string }) {
  return (
    <div class="step-row">
      <span class="nav-number">{props.number}</span>
      <span class="small">{props.text}</span>
    </div>
  );
}

export function BatchStepper(props: {
  count: number;
  current: number;
  label?: string;
  done: (number: number) => boolean;
  onPick: (number: number) => void;
}) {
  return (
    <div class="batch-stepper">
      <span class="cell-mono faint">
        {props.label ?? 'Batch'} {props.current} of {props.count}
      </span>
      <div class="stepper-dots">
        {Array.from({ length: props.count }, (_, index) => index + 1).map((number) => (
          <button type="button" key={number} aria-label={`Batch ${number}`}
            class={`stepper-dot${number === props.current ? ' current' : ''}` +
              `${props.done(number) ? ' done' : ''}`}
            onClick={() => props.onPick(number)} />
        ))}
      </div>
    </div>
  );
}

function formatTokens(tokens: number): string {
  return tokens >= 1000 ? `${(tokens / 1000).toFixed(1)}k` : String(tokens);
}
