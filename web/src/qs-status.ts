import { api, type StatusData } from './api.js';

export class QsStatus extends HTMLElement {
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
      const d = j.data!;
      const s = d.stats || { rxBytes: 0, txBytes: 0 };
      this.innerHTML =
        `<div class="qs-status">` +
        `<div>mode: <span data-mode>${d.mode}</span></div>` +
        `<div>sessions: <span data-sessions>${d.sessions}</span></div>` +
        `<div>rx: <span data-rx>${s.rxBytes}</span> bytes</div>` +
        `<div>tx: <span data-tx>${s.txBytes}</span> bytes</div>` +
        `<div>version: <span data-version>${d.version}</span></div>` +
        `</div>`;
    } catch (e) {
      this.innerHTML = `<div class="qs-status error">status error: ${(e as Error).message}</div>`;
    }
  }
}

customElements.define('qs-status', QsStatus);
