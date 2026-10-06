// Copies with the Clipboard API (127.0.0.1 is a secure context). When the browser
// refuses, the text is selected on screen so the user can press Ctrl+C themselves.
export async function copyText(text: string, fallbackElement?: HTMLElement | null):
  Promise<'copied' | 'selected' | 'failed'> {
  try {
    await navigator.clipboard.writeText(text);
    return 'copied';
  } catch {
    if (!fallbackElement) return 'failed';
    const range = document.createRange();
    range.selectNodeContents(fallbackElement);
    const selection = window.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
    return 'selected';
  }
}
