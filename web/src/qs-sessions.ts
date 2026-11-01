import { api, type SessionEntry } from './api.js';

function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

export class QsSessions extends HTMLElement {
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
      const j = await api<{ sessions: SessionEntry[] }>('/api/sessions');
      const rows = (j.data && j.data.sessions) || [];
      this.innerHTML =
        `<div class="qs-sessions">` +
        `<table><thead><tr><th>id</th><th>peer</th><th>up</th><th>down</th><th>smb</th></tr></thead><tbody>` +
        rows.map((e) => `<tr><td>${e.id}</td><td>${esc(e.peer || '')}</td><td>${e.bytesUp}</td><td>${e.bytesDown}</td><td>${esc([e.dialect, e.command].filter(Boolean).join(' '))}</td></tr>`).join('') +
        `</tbody></table></div>`;
    } catch (e) {
      this.innerHTML = `<div class="qs-sessions error">sessions error: ${(e as Error).message}</div>`;
    }
  }
}

customElements.define('qs-sessions', QsSessions);
