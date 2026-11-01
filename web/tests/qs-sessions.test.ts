import { describe, it, expect, vi, afterEach } from 'vitest';
import { QsSessions } from '../src/qs-sessions.js';

afterEach(() => {
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

describe('qs-sessions', () => {
  it('renders session rows', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        json: async () => ({
          ok: true,
          data: { sessions: [{ id: 7, peer: 'p', bytesUp: 1, bytesDown: 2, command: 'READ', dialect: 'SMB 3.1.1' }] },
        }),
        status: 200,
      }),
    );
    const el = new QsSessions() as unknown as HTMLElement;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 50));
    expect(el.innerHTML).toContain('READ');
    expect(el.innerHTML).toContain('7');
  });

  it('shows error on fetch failure', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('net down')));
    const el = new QsSessions() as unknown as HTMLElement;
    document.body.appendChild(el);
    await new Promise((r) => setTimeout(r, 50));
    expect(el.innerHTML).toContain('net down');
  });
});
