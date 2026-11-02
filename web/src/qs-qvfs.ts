import { api, type StatusData, type QvfsData } from './api.js';

function fmtBytes(n: number): string {
  if (!n) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return (i === 0 ? v : v.toFixed(1)) + ' ' + units[i];
}

function fmtPct(r: number): string {
  return (r * 100).toFixed(1) + '%';
}

export class QsQvfs extends HTMLElement {
  private timer: ReturnType<typeof setInterval> | null = null;

  connectedCallback(): void {
    void this.refresh();
    this.timer = setInterval(() => void this.refresh(), 2000);
  }

  disconnectedCallback(): void {
    if (this.timer) clearInterval(this.timer);
    this.timer = null;
  }

  async refresh(): Promise<void> {
    try {
      const j = await api<StatusData>('/api/status');
      const q: QvfsData | undefined = j.data!.qvfs;
      if (!q) {
        // 非 qvps 模式（旧隧道面板）：隐藏本组件
        this.innerHTML = '';
        return;
      }
      this.innerHTML =
        `<div class="qs-qvfs">` +
        `<h2>qvfs 缓存</h2>` +
        `<div>缓存块: <span data-blocks>${q.blocks ?? 0}</span></div>` +
        `<div>缓存用量: <span data-used>${fmtBytes(q.usedBytes ?? 0)}</span></div>` +
        `<div>脏块待回写: <span data-dirty>${q.dirty ?? 0}</span></div>` +
        `<div>命中率: <span data-hitrate>${fmtPct(q.hitRate ?? 0)}</span></div>` +
        `</div>`;
    } catch (e) {
      this.innerHTML = `<div class="qs-qvfs error">qvfs error: ${(e as Error).message}</div>`;
    }
  }
}

customElements.define('qs-qvfs', QsQvfs);
