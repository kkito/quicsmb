import { describe, it, expect, vi, afterEach } from 'vitest';
import { QsQvfs } from '../src/qs-qvfs.js';

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

describe('qs-qvfs', () => {
  it('renders blocks/used/dirty/hitRate', async () => {
    stubStatus({
      mode: 'qvps',
      sessions: 0,
      stats: { rxBytes: 0, txBytes: 0 },
      version: 'qvfs',
      qvfs: { blocks: 2, usedBytes: 1048576, dirty: 2, hitRate: 0.928571 },
    });
    const el = new QsQvfs() as unknown as HTMLElement;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 50));
    expect(el.querySelector('[data-blocks]')?.textContent).toBe('2');
    expect(el.querySelector('[data-dirty]')?.textContent).toBe('2');
    expect(el.querySelector('[data-used]')?.textContent).toBe('1.0 MB');
    expect(el.querySelector('[data-hitrate]')?.textContent).toBe('92.9%');
  });

  it('hides itself when mode has no qvfs', async () => {
    stubStatus({ mode: 'client', sessions: 1, stats: { rxBytes: 1, txBytes: 2 }, version: 'x' });
    const el = new QsQvfs() as unknown as HTMLElement;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 50));
    expect(el.innerHTML).toBe('');
  });

  it('shows error on fetch failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ json: async () => ({ ok: false, error: { message: 'boom' } }), status: 500 }),
    );
    const el = new QsQvfs() as unknown as HTMLElement;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 50));
    expect(el.innerHTML).toContain('boom');
  });
});
