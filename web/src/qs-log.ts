import { api, type LogEntry } from './api.js';

function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export class QsLog extends HTMLElement {
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
      const j = await api<{ logs: LogEntry[] }>('/api/logs?limit=50');
      const logs = (j.data && j.data.logs) || [];
      this.innerHTML =
        `<ul class="qs-log">` +
        logs.map((l) => `<li><span>${esc(l.Time)}</span> <span>${esc(l.Dir)}</span> <span>${esc(l.Summary)}</span></li>`).join('') +
        `</ul>`;
    } catch (e) {
      this.innerHTML = `<div class="qs-log error">logs error: ${(e as Error).message}</div>`;
    }
  }
}

customElements.define('qs-log', QsLog);
