export interface ApiEnvelope<T = unknown> {
  ok: boolean;
  data?: T;
  error?: { code?: string; message?: string };
}

export async function api<T = unknown>(path: string, opts?: RequestInit): Promise<ApiEnvelope<T>> {
  const r = await fetch(path, opts);
  const j = (await r.json()) as ApiEnvelope<T>;
  if (!j.ok) throw new Error((j.error && j.error.message) || 'HTTP ' + r.status);
  return j;
}

export interface QvfsData {
  blocks: number;
  usedBytes: number;
  dirty: number;
  hitRate: number;
}

export interface StatusData {
  mode: string;
  sessions: number;
  stats: { rxBytes: number; txBytes: number };
  version: string;
  qvfs?: QvfsData;
}

export interface SessionEntry {
  id: number;
  mode: string;
  peer: string;
  started: string;
  bytesUp: number;
  bytesDown: number;
  command: string;
  dialect: string;
}

export interface LogEntry {
  Time: string;
  Dir: string;
  Summary: string;
}
