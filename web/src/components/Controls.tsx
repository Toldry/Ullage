import type { ComponentChildren } from 'preact';

export type CheckState = 'on' | 'off' | 'partial';

export function Checkbox(props: {
  state: CheckState;
  label: string;
  disabled?: boolean;
  onToggle?: () => void;
}) {
  return (
    <button
      type="button"
      role="checkbox"
      aria-checked={props.state === 'on' ? 'true' : props.state === 'partial' ? 'mixed' : 'false'}
      aria-label={props.label}
      class={`checkbox ${props.state}`}
      disabled={props.disabled}
      onClick={(event) => {
        event.stopPropagation();
        props.onToggle?.();
      }}
    />
  );
}

export function Kbd(props: { children: ComponentChildren; inverse?: boolean }) {
  return <span class={props.inverse ? 'kbd kbd-inverse' : 'kbd'}>{props.children}</span>;
}

export function Logo() {
  return (
    <div class="logo" aria-hidden="true">
      <div class="logo-fill" />
    </div>
  );
}

export type Fill = 'unknown' | 'known' | 'system' | 'unaccounted' | 'free';

export function Swatch(props: { fill: Fill; small?: boolean }) {
  return <span class={`swatch fill-${props.fill}${props.small ? ' swatch-small' : ''}`} />;
}
