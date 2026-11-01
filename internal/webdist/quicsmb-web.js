var QuicsmbWeb = (() => {
  // web/src/api.ts
  async function api(path, opts) {
    const r = await fetch(path, opts);
    const j = await r.json();
    if (!j.ok) throw new Error(j.error && j.error.message || "HTTP " + r.status);
    return j;
  }

  // web/src/qs-status.ts
  var QsStatus = class extends HTMLElement {
    timer = null;
    connectedCallback() {
      void this.refresh();
      this.timer = setInterval(() => void this.refresh(), 2e3);
    }
    disconnectedCallback() {
      if (this.timer) clearInterval(this.timer);
      this.timer = null;
    }
    async refresh() {
      try {
        const j = await api("/api/status");
        const d = j.data;
        const s = d.stats || { rxBytes: 0, txBytes: 0 };
        this.innerHTML = `<div class="qs-status"><div>mode: <span data-mode>${d.mode}</span></div><div>sessions: <span data-sessions>${d.sessions}</span></div><div>rx: <span data-rx>${s.rxBytes}</span> bytes</div><div>tx: <span data-tx>${s.txBytes}</span> bytes</div><div>version: <span data-version>${d.version}</span></div></div>`;
      } catch (e) {
        this.innerHTML = `<div class="qs-status error">status error: ${e.message}</div>`;
      }
    }
  };
  customElements.define("qs-status", QsStatus);

  // web/src/qs-sessions.ts
  function esc(s) {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }
  var QsSessions = class extends HTMLElement {
    timer = null;
    connectedCallback() {
      void this.refresh();
      this.timer = setInterval(() => void this.refresh(), 2e3);
    }
    disconnectedCallback() {
      if (this.timer) clearInterval(this.timer);
      this.timer = null;
    }
    async refresh() {
      try {
        const j = await api("/api/sessions");
        const rows = j.data && j.data.sessions || [];
        this.innerHTML = `<div class="qs-sessions"><table><thead><tr><th>id</th><th>peer</th><th>up</th><th>down</th><th>smb</th></tr></thead><tbody>` + rows.map((e) => `<tr><td>${e.id}</td><td>${esc(e.peer || "")}</td><td>${e.bytesUp}</td><td>${e.bytesDown}</td><td>${esc([e.dialect, e.command].filter(Boolean).join(" "))}</td></tr>`).join("") + `</tbody></table></div>`;
      } catch (e) {
        this.innerHTML = `<div class="qs-sessions error">sessions error: ${e.message}</div>`;
      }
    }
  };
  customElements.define("qs-sessions", QsSessions);

  // web/src/qs-log.ts
  function esc2(s) {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }
  var QsLog = class extends HTMLElement {
    timer = null;
    connectedCallback() {
      void this.refresh();
      this.timer = setInterval(() => void this.refresh(), 2e3);
    }
    disconnectedCallback() {
      if (this.timer) clearInterval(this.timer);
      this.timer = null;
    }
    async refresh() {
      try {
        const j = await api("/api/logs?limit=50");
        const logs = j.data && j.data.logs || [];
        this.innerHTML = `<ul class="qs-log">` + logs.map((l) => `<li><span>${esc2(l.Time)}</span> <span>${esc2(l.Dir)}</span> <span>${esc2(l.Summary)}</span></li>`).join("") + `</ul>`;
      } catch (e) {
        this.innerHTML = `<div class="qs-log error">logs error: ${e.message}</div>`;
      }
    }
  };
  customElements.define("qs-log", QsLog);
})();
