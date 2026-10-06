import { useEffect, useRef, useState } from 'preact/hooks';
import { Kbd } from '../components/Controls';
import { copyText } from '../lib/clipboard';
import { checkLabels, exportApi } from '../lib/exporting';
import { formatCount } from '../lib/format';
import type { CheckName, ExportView, PasteResult } from '../lib/types';
import { BatchStepper } from './ExportScreen';

interface Props {
  guard: <T>(work: Promise<T>) => Promise<T | undefined>;
  batch: number;
  onBatchChange: (batch: number) => void;
  onGoExport: () => void;
}

const checkOrder: CheckName[] = ['session', 'complete', 'format', 'ids', 'duplicates'];

export function PasteScreen({ guard, batch, onBatchChange, onGoExport }: Props) {
  const [view, setView] = useState<ExportView | null>(null);
  const [text, setText] = useState('');
  const [outcome, setOutcome] = useState<PasteResult | null>(null);
  const [copyNote, setCopyNote] = useState('');
  const repairElement = useRef<HTMLPreElement>(null);

  useEffect(() => {
    void guard(exportApi.get()).then((loaded) => loaded && setView(loaded));
  }, [guard]);

  const parse = async () => {
    if (!text.trim()) return;
    const result = await guard(exportApi.paste(batch, text));
    if (!result) return;
    setOutcome(result);
    setView(result.export);
    setCopyNote('');
  };

  const clear = () => {
    setText('');
    setOutcome(null);
  };

  if (!view) {
    return (
      <div class="screen">
        <PasteHeading code="" />
        <div class="placeholder">Export a prompt first (step 4).</div>
      </div>
    );
  }
  const current = view.batches[Math.min(batch, view.batches.length) - 1];
  const nextBatch = view.batches.find((item) => !item.imported && item.number !== batch);

  return (
    <div class="screen screen-wide">
      <div class="screen-header">
        <PasteHeading code={view.code} />
        <BatchStepper count={view.batches.length} current={current.number}
          done={(number) => view.batches[number - 1].imported}
          onPick={(number) => { onBatchChange(number); setOutcome(null); }} />
      </div>
      <div class="export-grid">
        <div class="paste-column">
          <div class="panel">
            <div class="panel-header export-header">
              <span>Response</span>
              <span class="cell-mono faint normal-case">
                {formatCount(text.length)} characters
              </span>
              <span class="spacer" />
              <span class="faint normal-case">Prose and ``` fences around the block are fine</span>
            </div>
            <textarea class="paste-input" spellcheck={false} value={text}
              aria-label={`Reply for batch ${current.number}`}
              placeholder={`Paste the assistant's reply for batch ${current.number} here.`}
              onInput={(event) => setText((event.target as HTMLTextAreaElement).value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
                  event.preventDefault();
                  void parse();
                }
              }} />
          </div>
          <div class="start-row">
            <button type="button" class="button button-primary button-with-key"
              disabled={!text.trim()} onClick={parse}>
              Parse response
              <Kbd inverse>Ctrl Enter</Kbd>
            </button>
            <button type="button" class="button" onClick={clear}>Clear</button>
            <span class="spacer" />
            <span class="small faint">Pasted text never leaves this tab and this PC.</span>
          </div>
          {outcome && (
            <Outcome outcome={outcome} batchNumber={current.number}
              batchCount={view.batches.length} nextBatch={nextBatch?.number}
              copyNote={copyNote} repairElement={repairElement}
              onCopyRepair={async () => {
                const result = await copyText(outcome.repair ?? '', repairElement.current);
                setCopyNote(result === 'copied' ? 'Repair prompt copied.'
                  : 'The text is selected: press Ctrl+C.');
              }}
              onNextBatch={(number) => { onBatchChange(number); clear(); onGoExport(); }}
              onCopyPromptAgain={onGoExport} />
          )}
        </div>
        <div class="side-column">
          <div class="panel checks">
            <div class="section-title">Checks</div>
            {checkOrder.map((check) => {
              const state = !outcome ? 'skip'
                : outcome.result.problems.some((problem) => problem.check === check) ? 'fail'
                  : check !== 'session' && check !== 'complete' && !outcome.result.found
                    ? 'skip' : 'pass';
              return (
                <div class="check-row" key={check}>
                  <span class={`dot check-${state}`} />
                  <span class={`check-label check-${state}`}>
                    {state === 'skip' ? '–' : state}
                  </span>
                  <span>{checkLabels[check]}</span>
                </div>
              );
            })}
          </div>
          <div class="panel">
            {view.batches.map((item) => (
              <div class="batch-row static" key={item.number}>
                <span>Batch {item.number}</span>
                <span class={`small ${item.imported ? 'ok-text' : 'faint'}`}>
                  {item.imported
                    ? `${item.categoryCount} categories · ${formatCount(item.classifiedItems)} ` +
                      `of ${formatCount(item.items)} items`
                    : 'not imported'}
                </span>
              </div>
            ))}
          </div>
          <div class="small muted">
            Imported categories map back to real paths locally. The assistant only ever saw
            redacted names.
          </div>
        </div>
      </div>
    </div>
  );
}

function PasteHeading({ code }: { code: string }) {
  return (
    <div class="heading">
      <div class="step-label">Step 5 of 7</div>
      <h1>Paste response</h1>
      <p class="lead">
        Paste the assistant's full reply. Ullage parses it in this tab and checks it
        {code ? <> against session <span class="mono strong">{code}</span></> : ''} before
        importing anything.
      </p>
    </div>
  );
}

function Outcome(props: {
  outcome: PasteResult;
  batchNumber: number;
  batchCount: number;
  nextBatch?: number;
  copyNote: string;
  repairElement: { current: HTMLPreElement | null };
  onCopyRepair: () => void;
  onNextBatch: (number: number) => void;
  onCopyPromptAgain: () => void;
}) {
  const { outcome } = props;
  const problems = outcome.result.problems;
  const sessionMismatch = problems.some((problem) => problem.check === 'session');
  const truncated = outcome.result.truncated && problems.length === 1;
  if (outcome.valid) {
    const empty = outcome.result.categories.length === 0;
    return (
      <div class="result-card tone-ok">
        <div class="result-title">
          {empty ? 'Imported: the assistant recognized nothing in this batch'
            : `Imported ${outcome.result.categories.length} categories`}
        </div>
        <div class="muted">
          {formatCount(outcome.result.classified)} items classified. Items the assistant
          left out stay Unknown.
        </div>
        <div class="actions">
          {props.nextBatch ? (
            <button type="button" class="button button-primary"
              onClick={() => props.onNextBatch(props.nextBatch!)}>
              Copy batch {props.nextBatch} prompt
            </button>
          ) : (
            <span class="small faint">All batches imported. Results arrive in the next phase.</span>
          )}
        </div>
      </div>
    );
  }
  const tone = truncated ? 'tone-warn' : 'tone-bad';
  const title = sessionMismatch ? 'Session code mismatch'
    : truncated ? 'Response is truncated'
      : `${problems.length} ${problems.length === 1 ? 'problem' : 'problems'} in the response`;
  const body = sessionMismatch
    ? 'This reply belongs to another prompt, probably from an older conversation. Paste ' +
      `the reply to the current batch ${props.batchNumber} prompt, or copy the prompt again.`
    : 'Nothing was imported. The repair prompt lists each problem and asks for the full ' +
      'corrected reply; paste it into the same conversation.';
  return (
    <div class={`result-card ${tone}`}>
      <div class="result-title">{title}</div>
      <div class="muted">{body}</div>
      <div class="problem-list">
        {problems.map((problem, index) => (
          <div class="problem-row" key={index}>
            <span class="cell-mono faint">{problem.line ? `line ${problem.line}` : ''}</span>
            <span class="mono small">{problem.subject}</span>
            <span class="small">{problem.message}</span>
          </div>
        ))}
      </div>
      {outcome.repair && !sessionMismatch && (
        <div class="repair">
          <div class="between">
            <span class="section-title">Repair prompt</span>
            <span class="small faint">paste into the same conversation</span>
          </div>
          <pre class="repair-text" ref={props.repairElement}>{outcome.repair}</pre>
        </div>
      )}
      <div class="actions">
        {sessionMismatch ? (
          <button type="button" class="button button-primary" onClick={props.onCopyPromptAgain}>
            Copy batch {props.batchNumber} prompt again
          </button>
        ) : (
          <button type="button" class="button button-primary" onClick={props.onCopyRepair}>
            Copy repair prompt
          </button>
        )}
        <span class="small faint">{props.copyNote}</span>
      </div>
    </div>
  );
}
