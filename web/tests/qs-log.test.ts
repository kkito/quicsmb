import { describe, it, expect, vi, afterEach } from 'vitest';
import { QsLog } from '../src/qs-log.js';

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

describe('qs-log', () => {
  it('renders log rows', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        json: async () => ({ ok: true, data: { logs: [{ Time: 't', Dir: 'connect', Summary: 'hello' }] } }),
        status: 200,
      }),
    );
    const el = new QsLog() as unknown as HTMLElement;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 50));
    expect(el.innerHTML).toContain('hello');
  });

  it('escapes html in summary', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        json: async () => ({ ok: true, data: { logs: [{ Time: 't', Dir: 'x', Summary: '<b>hi</b>' }] } }),
        status: 200,
      }),
    );
    const el = new QsLog() as unknown as HTMLElement;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 50));
    expect(el.innerHTML).toContain('&lt;b&gt;');
    expect(el.innerHTML).not.toContain('<b>hi</b>');
  });
});
