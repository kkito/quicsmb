import { describe, it, expect, vi, afterEach } from 'vitest';
import { QsStatus } from '../src/qs-status.js';

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

function stubStatus(data: unknown) {
  vi.stubGlobal(
    'fetch',
    vi.fn().mockResolvedValue({ json: async () => ({ ok: true, data }), status: 200 }),
  );
}

describe('qs-status', () => {
  it('renders mode and sessions', async () => {
    stubStatus({ mode: 'client', sessions: 2, stats: { rxBytes: 10, txBytes: 20 }, version: 'dev' });
    const el = new QsStatus() as unknown as HTMLElement;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 50));
    expect(el.innerHTML).toContain('client');
    expect(el.querySelector('[data-sessions]')?.textContent).toBe('2');
  });

  it('shows error on fetch failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ json: async () => ({ ok: false, error: { message: 'boom' } }), status: 500 }),
    );
    const el = new QsStatus() as unknown as HTMLElement;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 50));
    expect(el.innerHTML).toContain('boom');
  });
});
