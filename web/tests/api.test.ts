import { describe, it, expect, vi, afterEach } from 'vitest';
import { api } from '../src/api.js';

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('api client', () => {
  it('returns data on ok=true', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        json: async () => ({ ok: true, data: { mode: 'client' } }),
        status: 200,
      }),
    );
    const j = await api<{ mode: string }>('/api/status');
    expect(j.data!.mode).toBe('client');
  });

  it('throws message on ok=false', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({
        json: async () => ({ ok: false, error: { code: 'BAD_LIMIT', message: 'invalid limit' } }),
        status: 400,
      }),
    );
    await expect(api('/api/logs')).rejects.toThrow('invalid limit');
  });

  it('throws HTTP status when body has no message', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ json: async () => ({ ok: false }), status: 500 }),
    );
    await expect(api('/api/status')).rejects.toThrow('500');
  });
});
