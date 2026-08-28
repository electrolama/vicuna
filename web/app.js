(() => {
  "use strict";

  const $ = (selector) => document.querySelector(selector);
  const $$ = (selector) => [...document.querySelectorAll(selector)];
  const encoder = new TextEncoder();
  const storageKey = "vicuna.settings.v1";
  const maxMonitorLines = 5000;
  const maxHexRows = 5000;

  const elements = {
    port: $("#portSelect"), refresh: $("#refreshPorts"), baud: $("#baudSelect"),
    customBaud: $("#customBaudInput"), customBaudField: $("#customBaudField"),
    connect: $("#connectButton"), connectLabel: $(".connect-label"), stream: $("#streamStatus"),
    hardware: $("#hardwareSelect"), hardwarePanel: $("#hardwarePanel"), hardwareDescription: $("#hardwareDescription"),
    settingsButton: $("#serialSettingsButton"), settingsPopover: $("#serialPopover"),
    dataBits: $("#dataBits"), parity: $("#parity"), stopBits: $("#stopBits"), framing: $("#framingSummary"), framingInline: $("#framingInline"),
    timestamps: $("#timestampsToggle"), ansi: $("#ansiToggle"), autoscroll: $("#autoscrollToggle"), theme: $("#themeButton"),
    clear: $("#clearButton"), export: $("#exportButton"), more: $("#moreButton"), linePopover: $("#linePopover"),
    sendBreak: $("#breakButton"), localEcho: $("#localEchoToggle"), resetBuffers: $("#resetBuffersButton"),
    terminal: $("#terminal"), terminalScreen: $(".terminal-screen"), terminalCursor: $(".terminal-cursor"), terminalEmpty: $("#terminalEmpty"),
    monitor: $("#monitorOutput"), hex: $("#hexOutput"), workspace: $(".workspace"),
    sendInput: $("#sendInput"), lineEnding: $("#lineEnding"), send: $("#sendButton"),
    toast: $("#toastRegion")
  };

  let settings = loadSettings();
  let applyingSettings = false;
  let activeView = settings.view || "terminal";
  let sendMode = settings.sendMode || "text";
  let connected = false;
  let signals = { connected: false, available: false, dtr: false, rts: false, cts: false, dsr: false, ri: false, dcd: false };
  let eventSource = null;
  let monitorEntries = [];
  let monitorPending = { rx: null, tx: null };
  let hexRows = [];
  let hexOffsets = { rx: 0, tx: 0 };
  let logRenderPending = false;
  const monitorDecoders = { rx: new TextDecoder(), tx: new TextDecoder() };

  const ansiPalettes = {
    dark: [
      "#1b2028", "#e66571", "#62c989", "#dfbc62", "#5fa9ea", "#b98be1", "#59c7ca", "#cbd2db",
      "#65707f", "#ff7b86", "#74e39c", "#f3d276", "#77bdff", "#d19af8", "#6ce2e5", "#f4f6f8"
    ],
    light: [
      "#000000", "#cd3131", "#008000", "#795e26", "#0451a5", "#af00db", "#008080", "#6a737d",
      "#5a5a5a", "#e51400", "#16c60c", "#b89500", "#007acc", "#c586c0", "#00a2a2", "#1f1f1f"
    ]
  };
  let ansiPalette = ansiPalettes.dark;

  class TerminalEmulator {
    constructor(container, screen, cursor) {
      this.container = container;
      this.screenNode = screen;
      this.cursorNode = cursor;
      this.cols = 80;
      this.rows = 24;
      this.row = 0;
      this.col = 0;
      this.scrollTop = 0;
      this.scrollBottom = 23;
      this.savedCursor = { row: 0, col: 0, attr: null };
      this.attr = this.defaultAttr();
      this.lines = this.makeLines(this.rows);
      this.scrollback = [];
      this.state = "normal";
      this.sequence = "";
      this.decoders = { rx: new TextDecoder(), tx: new TextDecoder() };
      this.colorsEnabled = true;
      this.renderQueued = false;
      this.appCursor = false;
      this.bracketedPaste = false;
      this.altSnapshot = null;
      this.cellWidth = 8;
      this.lineHeight = 18;
    }

    defaultAttr() { return { fg: null, bg: null, bold: false, dim: false, underline: false, inverse: false }; }
    blank(attr = this.attr) { return { ch: " ", attr: { ...attr } }; }
    blankLine() { return Array.from({ length: this.cols }, () => this.blank(this.defaultAttr())); }
    makeLines(count) { return Array.from({ length: count }, () => this.blankLine()); }

    reset() {
      this.row = 0; this.col = 0; this.scrollTop = 0; this.scrollBottom = this.rows - 1;
      this.attr = this.defaultAttr(); this.lines = this.makeLines(this.rows); this.scrollback = [];
      this.state = "normal"; this.sequence = ""; this.altSnapshot = null; this.appCursor = false; this.bracketedPaste = false;
      this.scheduleRender();
    }

    clearDisplay() {
      this.lines = this.makeLines(this.rows); this.scrollback = []; this.row = 0; this.col = 0;
      this.scheduleRender();
    }

    writeBytes(bytes, direction = "rx") { this.write(this.decoders[direction].decode(bytes, { stream: true })); }

    write(text) {
      for (const char of text) this.consume(char);
      this.scheduleRender();
    }

    consume(char) {
      if (this.state === "osc") {
        if (char === "\u0007") { this.state = "normal"; this.sequence = ""; }
        else if (char === "\u001b") this.state = "osc-esc";
        return;
      }
      if (this.state === "osc-esc") {
        this.state = char === "\\" ? "normal" : "osc";
        return;
      }
      if (this.state === "csi") {
        if (char >= "@" && char <= "~") {
          this.handleCSI(this.sequence, char);
          this.sequence = ""; this.state = "normal";
        } else if (this.sequence.length < 96) this.sequence += char;
        return;
      }
      if (this.state === "esc") {
        this.state = "normal";
        if (char === "[") { this.state = "csi"; this.sequence = ""; return; }
        if (char === "]") { this.state = "osc"; this.sequence = ""; return; }
        if (char === "7") this.saveCursor();
        else if (char === "8") this.restoreCursor();
        else if (char === "D") this.lineFeed();
        else if (char === "E") { this.col = 0; this.lineFeed(); }
        else if (char === "M") this.reverseIndex();
        else if (char === "c") this.reset();
        return;
      }
      if (char === "\u001b") { this.state = "esc"; return; }
      if (char === "\n" || char === "\u000b" || char === "\u000c") { this.lineFeed(); return; }
      if (char === "\r") { this.col = 0; return; }
      if (char === "\b") { this.col = Math.max(0, this.col - 1); return; }
      if (char === "\t") { this.col = Math.min(this.cols - 1, (Math.floor(this.col / 8) + 1) * 8); return; }
      if (char < " " || char === "\u007f") return;
      this.put(char);
    }

    put(char) {
      if (this.col >= this.cols) { this.col = 0; this.lineFeed(); }
      this.lines[this.row][this.col] = { ch: char, attr: { ...this.attr } };
      this.col += 1;
    }

    lineFeed() {
      if (this.row === this.scrollBottom) this.scrollUp(1);
      else this.row = Math.min(this.rows - 1, this.row + 1);
    }

    reverseIndex() {
      if (this.row === this.scrollTop) this.scrollDown(1);
      else this.row = Math.max(0, this.row - 1);
    }

    scrollUp(count) {
      for (let i = 0; i < count; i++) {
        const removed = this.lines.splice(this.scrollTop, 1)[0];
        this.lines.splice(this.scrollBottom, 0, this.blankLine());
        if (this.scrollTop === 0 && !this.altSnapshot) {
          this.scrollback.push(removed);
          if (this.scrollback.length > 750) this.scrollback.shift();
        }
      }
    }

    scrollDown(count) {
      for (let i = 0; i < count; i++) {
        this.lines.splice(this.scrollBottom, 1);
        this.lines.splice(this.scrollTop, 0, this.blankLine());
      }
    }

    saveCursor() { this.savedCursor = { row: this.row, col: this.col, attr: { ...this.attr } }; }
    restoreCursor() {
      this.row = clamp(this.savedCursor.row, 0, this.rows - 1);
      this.col = clamp(this.savedCursor.col, 0, this.cols - 1);
      if (this.savedCursor.attr) this.attr = { ...this.savedCursor.attr };
    }

    handleCSI(raw, command) {
      let privateMode = "";
      if (raw.startsWith("?") || raw.startsWith(">") || raw.startsWith("!")) { privateMode = raw[0]; raw = raw.slice(1); }
      const params = raw === "" ? [] : raw.split(";").map((value) => value === "" ? 0 : Number.parseInt(value, 10));
      const amount = (index = 0, fallback = 1) => params[index] || fallback;
      switch (command) {
        case "A": this.row = Math.max(this.scrollTop, this.row - amount()); break;
        case "B": this.row = Math.min(this.scrollBottom, this.row + amount()); break;
        case "C": this.col = Math.min(this.cols - 1, this.col + amount()); break;
        case "D": this.col = Math.max(0, this.col - amount()); break;
        case "E": this.row = Math.min(this.scrollBottom, this.row + amount()); this.col = 0; break;
        case "F": this.row = Math.max(this.scrollTop, this.row - amount()); this.col = 0; break;
        case "G": case "`": this.col = clamp(amount(0, 1) - 1, 0, this.cols - 1); break;
        case "d": this.row = clamp(amount(0, 1) - 1, 0, this.rows - 1); break;
        case "H": case "f": this.row = clamp(amount(0, 1) - 1, 0, this.rows - 1); this.col = clamp(amount(1, 1) - 1, 0, this.cols - 1); break;
        case "J": this.eraseDisplay(params[0] || 0); break;
        case "K": this.eraseLine(params[0] || 0); break;
        case "m": this.sgr(params); break;
        case "s": this.saveCursor(); break;
        case "u": this.restoreCursor(); break;
        case "r": {
          const top = amount(0, 1) - 1, bottom = amount(1, this.rows) - 1;
          if (top >= 0 && bottom < this.rows && top < bottom) { this.scrollTop = top; this.scrollBottom = bottom; this.row = top; this.col = 0; }
          break;
        }
        case "@": this.insertChars(amount()); break;
        case "P": this.deleteChars(amount()); break;
        case "L": this.insertLines(amount()); break;
        case "M": this.deleteLines(amount()); break;
        case "S": this.scrollUp(amount()); break;
        case "T": this.scrollDown(amount()); break;
        case "h": case "l": if (privateMode === "?") this.setPrivateModes(params, command === "h"); break;
      }
    }

    eraseDisplay(mode) {
      if (mode === 2 || mode === 3) {
        this.lines = this.makeLines(this.rows);
        if (mode === 3) this.scrollback = [];
      } else if (mode === 0) {
        this.eraseLine(0);
        for (let row = this.row + 1; row < this.rows; row++) this.lines[row] = this.blankLine();
      } else if (mode === 1) {
        this.eraseLine(1);
        for (let row = 0; row < this.row; row++) this.lines[row] = this.blankLine();
      }
    }

    eraseLine(mode) {
      const start = mode === 1 || mode === 2 ? 0 : this.col;
      const end = mode === 0 || mode === 2 ? this.cols : this.col + 1;
      for (let col = start; col < end; col++) this.lines[this.row][col] = this.blank(this.defaultAttr());
    }

    insertChars(count) {
      const line = this.lines[this.row];
      line.splice(this.col, 0, ...Array.from({ length: count }, () => this.blank()));
      line.length = this.cols;
    }

    deleteChars(count) {
      const line = this.lines[this.row];
      line.splice(this.col, count);
      while (line.length < this.cols) line.push(this.blank());
    }

    insertLines(count) {
      if (this.row < this.scrollTop || this.row > this.scrollBottom) return;
      for (let i = 0; i < count; i++) { this.lines.splice(this.row, 0, this.blankLine()); this.lines.splice(this.scrollBottom + 1, 1); }
    }

    deleteLines(count) {
      if (this.row < this.scrollTop || this.row > this.scrollBottom) return;
      for (let i = 0; i < count; i++) { this.lines.splice(this.row, 1); this.lines.splice(this.scrollBottom, 0, this.blankLine()); }
    }

    sgr(params) {
      if (!params.length) params = [0];
      for (let i = 0; i < params.length; i++) {
        const code = params[i];
        if (code === 0) this.attr = this.defaultAttr();
        else if (code === 1) this.attr.bold = true;
        else if (code === 2) this.attr.dim = true;
        else if (code === 4) this.attr.underline = true;
        else if (code === 7) this.attr.inverse = true;
        else if (code === 22) { this.attr.bold = false; this.attr.dim = false; }
        else if (code === 24) this.attr.underline = false;
        else if (code === 27) this.attr.inverse = false;
        else if (code >= 30 && code <= 37) this.attr.fg = ansiPalette[code - 30];
        else if (code >= 90 && code <= 97) this.attr.fg = ansiPalette[code - 90 + 8];
        else if (code === 39) this.attr.fg = null;
        else if (code >= 40 && code <= 47) this.attr.bg = ansiPalette[code - 40];
        else if (code >= 100 && code <= 107) this.attr.bg = ansiPalette[code - 100 + 8];
        else if (code === 49) this.attr.bg = null;
        else if ((code === 38 || code === 48) && params[i + 1] === 5 && params[i + 2] !== undefined) {
          this.attr[code === 38 ? "fg" : "bg"] = color256(params[i + 2]); i += 2;
        } else if ((code === 38 || code === 48) && params[i + 1] === 2 && params[i + 4] !== undefined) {
          this.attr[code === 38 ? "fg" : "bg"] = `rgb(${clamp(params[i + 2],0,255)},${clamp(params[i + 3],0,255)},${clamp(params[i + 4],0,255)})`; i += 4;
        }
      }
    }

    setPrivateModes(params, enabled) {
      for (const mode of params) {
        if (mode === 1) this.appCursor = enabled;
        else if (mode === 2004) this.bracketedPaste = enabled;
        else if (mode === 1047 || mode === 1049) this.setAlternateScreen(enabled);
      }
    }

    setAlternateScreen(enabled) {
      if (enabled && !this.altSnapshot) {
        this.altSnapshot = { lines: this.lines, row: this.row, col: this.col, scrollTop: this.scrollTop, scrollBottom: this.scrollBottom };
        this.lines = this.makeLines(this.rows); this.row = 0; this.col = 0; this.scrollTop = 0; this.scrollBottom = this.rows - 1;
      } else if (!enabled && this.altSnapshot) {
        Object.assign(this, this.altSnapshot); this.altSnapshot = null;
      }
    }

    resize() {
      const probe = document.createElement("span");
      probe.textContent = "MMMMMMMMMM"; probe.style.visibility = "hidden"; probe.style.position = "absolute";
      this.screenNode.appendChild(probe);
      const rect = probe.getBoundingClientRect(); probe.remove();
      if (rect.width) this.cellWidth = rect.width / 10;
      if (rect.height) this.lineHeight = rect.height;
      const cols = Math.max(20, Math.floor((this.container.clientWidth - 34) / this.cellWidth));
      const rows = Math.max(4, Math.floor((this.container.clientHeight - 30) / this.lineHeight));
      if (cols === this.cols && rows === this.rows) { this.scheduleRender(); return; }
      for (const line of [...this.scrollback, ...this.lines]) {
        if (line.length > cols) line.length = cols;
        while (line.length < cols) line.push(this.blank(this.defaultAttr()));
      }
      if (rows > this.rows) while (this.lines.length < rows) this.lines.push(this.blankLineSized(cols));
      else if (rows < this.rows) {
        const remove = Math.min(this.lines.length - rows, this.row);
        if (remove > 0 && !this.altSnapshot) this.scrollback.push(...this.lines.splice(0, remove));
        while (this.lines.length > rows) this.lines.pop();
      }
      this.cols = cols; this.rows = rows;
      for (const line of this.lines) { if (line.length > cols) line.length = cols; while (line.length < cols) line.push(this.blank(this.defaultAttr())); }
      while (this.lines.length < rows) this.lines.push(this.blankLine());
      this.row = clamp(this.row, 0, rows - 1); this.col = clamp(this.col, 0, cols - 1);
      this.scrollTop = 0; this.scrollBottom = rows - 1;
      this.scheduleRender();
    }

    blankLineSized(cols) { return Array.from({ length: cols }, () => this.blank(this.defaultAttr())); }
    scheduleRender() {
      if (this.renderQueued) return;
      this.renderQueued = true;
      requestAnimationFrame(() => { this.renderQueued = false; this.render(); });
    }

    render() {
      const allLines = [...this.scrollback, ...this.lines];
      this.screenNode.innerHTML = allLines.map((line) => `<div class="terminal-line">${this.renderLine(line)}</div>`).join("");
      this.cursorNode.style.left = `${17 + this.col * this.cellWidth}px`;
      this.cursorNode.style.top = `${15 + (this.scrollback.length + this.row) * this.lineHeight}px`;
      if (elements.autoscroll.checked) this.container.scrollTop = this.container.scrollHeight;
    }

    renderLine(line) {
      let html = "", currentKey = null, text = "", attr = null;
      const flush = () => {
        if (!text) return;
        const content = escapeHTML(text);
        if (!this.colorsEnabled || !attr) html += content;
        else {
          let fg = attr.fg, bg = attr.bg;
          if (attr.inverse) [fg, bg] = [bg || "#d8dee9", fg || "#090c11"];
          const styles = [fg && `color:${fg}`, bg && `background:${bg}`].filter(Boolean).join(";");
          const classes = [attr.bold && "bold", attr.dim && "dim", attr.underline && "underline"].filter(Boolean).join(" ");
          html += styles || classes ? `<span${classes ? ` class="${classes}"` : ""}${styles ? ` style="${styles}"` : ""}>${content}</span>` : content;
        }
        text = "";
      };
      for (const cell of line) {
        const key = JSON.stringify(cell.attr);
        if (key !== currentKey) { flush(); currentKey = key; attr = cell.attr; }
        text += cell.ch;
      }
      flush();
      return html || " ";
    }

    dump() { return [...this.scrollback, ...this.lines].map((line) => line.map((cell) => cell.ch).join("").trimEnd()).join("\n"); }
  }

  const terminal = new TerminalEmulator(elements.terminal, elements.terminalScreen, elements.terminalCursor);

  class HardwareModule {
    constructor(id, label) { this.id = id; this.label = label; this.description = ""; }
    updateDefinition(definition) {
      this.label = definition.label || this.label;
      this.description = definition.description || "";
    }
    async setControl(control, value) {
      await api("/api/hardware/control", { method: "POST", body: JSON.stringify({ module: this.id, control, value }) });
    }
    indicator(name, label, alarm = false, assertedLabel = "HIGH", clearLabel = "LOW") {
      const known = connected && signals.available;
      const active = known && Boolean(signals[name]);
      const state = !known ? "unknown" : active ? (alarm ? "alarm" : "active") : "";
      const level = known ? (active ? assertedLabel : clearLabel) : "—";
      const title = !connected ? `${label}: disconnected` : !signals.available ? `${label}: unavailable` : `${label}: ${level}`;
      return `<span class="hardware-indicator ${state}" title="${escapeHTML(title)}"><i></i><strong>${escapeHTML(label)}</strong><b>${escapeHTML(level)}</b></span>`;
    }
    toggle(control, label, value, assertedLabel = "HIGH", clearLabel = "LOW") {
      const level = value ? assertedLabel : clearLabel;
      return `<button class="hardware-toggle ${value ? "active" : ""}" data-control="${control}" aria-pressed="${value}" title="${escapeHTML(`${label}: ${level}`)}"><i></i><strong>${escapeHTML(label)}</strong><b>${escapeHTML(level)}</b></button>`;
    }
  }

  // The baseline module leaves the modem-control lines in their standard form.
  class GenericRS232Module extends HardwareModule {
    constructor() { super("generic-rs232", "Generic RS232"); }
    connectionSignals() { return { dtr: Boolean(settings.dtr), rts: Boolean(settings.rts) }; }
    render() {
      const dtr = connected ? signals.dtr : Boolean(settings.dtr), rts = connected ? signals.rts : Boolean(settings.rts);
      elements.hardwarePanel.innerHTML = this.toggle("dtr", "DTR", dtr) + this.toggle("rts", "RTS", rts) +
        this.indicator("cts", "CTS") + this.indicator("dsr", "DSR") + this.indicator("ri", "RI") + this.indicator("dcd", "DCD");
    }
    async toggleControl(control) {
      if (control !== "dtr" && control !== "rts") return;
      const current = connected ? Boolean(signals[control]) : Boolean(settings[control]);
      const value = !current;
      if (connected) await this.setControl(control, value);
      settings[control] = value;
      if (connected) signals[control] = value;
    }
  }

  // pt1 is a worked example of assigning device-specific meaning to those lines.
  class PT1Module extends HardwareModule {
    constructor() { super("pt1", "pt1"); }
    connectionSignals() { return { dtr: Boolean(settings.pt1Vbus), rts: false }; }
    render() {
      const vbus = connected ? signals.dtr : Boolean(settings.pt1Vbus);
      elements.hardwarePanel.innerHTML = this.toggle("vbus", "VBUS", vbus, "ON", "OFF") + this.indicator("ri", "Overcurrent", true, "FAULT", "CLEAR");
    }
    async toggleControl(control) {
      if (control !== "vbus") return;
      const current = connected ? Boolean(signals.dtr) : Boolean(settings.pt1Vbus);
      const value = !current;
      if (connected) await this.setControl(control, value);
      settings.pt1Vbus = value;
      if (connected) signals.dtr = value;
    }
  }

  const hardwareModules = new Map();
  function registerHardwareModule(module) {
    if (hardwareModules.has(module.id)) throw new Error(`Duplicate browser hardware module: ${module.id}`);
    hardwareModules.set(module.id, module);
  }
  registerHardwareModule(new GenericRS232Module());
  registerHardwareModule(new PT1Module());

  function activeHardwareModule() { return hardwareModules.get(elements.hardware.value) || hardwareModules.get("generic-rs232"); }

  function renderHardwarePanel() {
    const module = activeHardwareModule();
    elements.hardwarePanel.setAttribute("aria-label", `${module.label} hardware controls`);
    elements.hardwarePanel.dataset.module = module.id;
    elements.hardwareDescription.textContent = module.description;
    module.render();
  }

  async function loadHardwareModules() {
    const { modules } = await api("/api/hardware");
    const selected = settings.hardware || elements.hardware.value;
    elements.hardware.innerHTML = "";
    for (const definition of modules) {
      const module = hardwareModules.get(definition.id);
      if (!module) {
        console.warn(`No browser implementation registered for hardware module: ${definition.id}`);
        continue;
      }
      module.updateDefinition(definition);
      const option = new Option(module.label, module.id);
      option.title = module.description;
      elements.hardware.add(option);
    }
    if ([...elements.hardware.options].some((option) => option.value === selected)) elements.hardware.value = selected;
    renderHardwarePanel();
  }

  function clamp(value, min, max) { return Math.max(min, Math.min(max, value)); }
  function color256(index) {
    index = clamp(index, 0, 255);
    if (index < 16) return ansiPalette[index];
    if (index >= 232) { const level = 8 + (index - 232) * 10; return `rgb(${level},${level},${level})`; }
    const value = index - 16, red = Math.floor(value / 36), green = Math.floor((value % 36) / 6), blue = value % 6;
    const channel = (part) => part === 0 ? 0 : 55 + part * 40;
    return `rgb(${channel(red)},${channel(green)},${channel(blue)})`;
  }

  function escapeHTML(value) {
    return String(value).replace(/[&<>"']/g, (char) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[char]);
  }

  function loadSettings() {
    const defaults = { theme: "dark", baud: 115200, dataBits: 8, parity: "none", stopBits: "1", timestamps: true, ansi: true, autoscroll: true, localEcho: false, lineEnding: "crlf", hardware: "generic-rs232", dtr: true, rts: true, pt1Vbus: false, view: "terminal", sendMode: "text" };
    try { return { ...defaults, ...JSON.parse(localStorage.getItem(storageKey) || "{}") }; }
    catch { return defaults; }
  }

  function applyDeploymentConfig(config) {
    if (!config || !config.configured) return;
    const serial = config.serial || {};
    settings = {
      ...settings,
      theme: config.theme,
      hardware: config.hardware,
      port: serial.port || "",
      baud: Number(serial.baud),
      dataBits: Number(serial.dataBits),
      parity: serial.parity,
      stopBits: serial.stopBits,
      dtr: Boolean(serial.dtr),
      rts: Boolean(serial.rts),
      pt1Vbus: config.hardware === "pt1" ? Boolean(serial.dtr) : false
    };
    if (config.mode === "embedded") {
      settings.view = "monitor";
      settings.timestamps = true;
    } else {
      settings.view = "terminal";
      settings.ansi = true;
    }
  }

  function saveSettings() {
    if (applyingSettings) return;
    settings = {
      ...settings, port: elements.port.value, baud: currentBaud(), dataBits: Number(elements.dataBits.value),
      parity: elements.parity.value, stopBits: elements.stopBits.value, timestamps: elements.timestamps.checked,
      ansi: elements.ansi.checked, autoscroll: elements.autoscroll.checked, localEcho: elements.localEcho.checked,
      lineEnding: elements.lineEnding.value, hardware: elements.hardware.value,
      view: activeView, sendMode
    };
    try { localStorage.setItem(storageKey, JSON.stringify(settings)); } catch { /* Browser storage is optional. */ }
  }

  function applySavedSettings() {
    applyingSettings = true;
    setBaud(settings.baud);
    elements.dataBits.value = settings.dataBits;
    elements.parity.value = settings.parity;
    elements.stopBits.value = settings.stopBits;
    elements.timestamps.checked = settings.timestamps;
    elements.ansi.checked = settings.ansi;
    elements.autoscroll.checked = settings.autoscroll;
    elements.localEcho.checked = settings.localEcho;
    elements.lineEnding.value = settings.lineEnding;
    if (hardwareModules.has(settings.hardware)) elements.hardware.value = settings.hardware;
    applyTheme(settings.theme);
    terminal.colorsEnabled = settings.ansi;
    updateTimestampClass(); updateFramingSummary(); setView(activeView); setSendMode(sendMode); renderHardwarePanel();
    applyingSettings = false;
  }

  function currentBaud() {
    return elements.baud.value === "custom" ? Number(elements.customBaud.value) : Number(elements.baud.value);
  }

  function setBaud(value) {
    const normalized = String(Number(value) || 115200);
    const preset = [...elements.baud.options].some((option) => option.value === normalized);
    elements.baud.value = preset ? normalized : "custom";
    if (!preset) elements.customBaud.value = normalized;
    elements.customBaudField.hidden = preset;
  }

  function applyTheme(theme) {
    const previousPalette = ansiPalette;
    settings.theme = theme === "light" ? "light" : "dark";
    ansiPalette = ansiPalettes[settings.theme];
    if (previousPalette !== ansiPalette) {
      const remap = (attr) => {
        if (!attr) return;
        const foreground = previousPalette.indexOf(attr.fg), background = previousPalette.indexOf(attr.bg);
        if (foreground >= 0) attr.fg = ansiPalette[foreground];
        if (background >= 0) attr.bg = ansiPalette[background];
      };
      for (const line of [...terminal.scrollback, ...terminal.lines]) for (const cell of line) remap(cell.attr);
      remap(terminal.attr); remap(terminal.savedCursor.attr);
    }
    document.documentElement.dataset.theme = settings.theme;
    document.documentElement.style.colorScheme = settings.theme;
    const light = settings.theme === "light";
    elements.theme.querySelector("span").textContent = light ? "Light" : "Dark";
    elements.theme.title = light ? "Switch to dark theme" : "Switch to light theme";
    elements.theme.setAttribute("aria-label", elements.theme.title);
  }

  async function api(path, options = {}) {
    const response = await fetch(path, {
      ...options,
      headers: { "Content-Type": "application/json", ...(options.headers || {}) }
    });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(payload.error || `${response.status} ${response.statusText}`);
    return payload;
  }

  async function refreshPorts() {
    elements.refresh.disabled = true;
    const selected = elements.port.value || settings.port || "";
    try {
      const { ports } = await api("/api/ports");
      elements.port.innerHTML = "";
      if (!ports.length && !selected) elements.port.add(new Option("No serial ports found", ""));
      for (const port of ports) {
        const details = [port.product, port.usb && port.vid && port.pid ? `${port.vid}:${port.pid}` : ""].filter(Boolean).join(" · ");
        elements.port.add(new Option(details ? `${port.name} — ${details}` : port.name, port.name));
      }
      if (selected && ![...elements.port.options].some((option) => option.value === selected)) {
        elements.port.add(new Option(`${selected} — preset`, selected), 0);
      }
      if ([...elements.port.options].some((option) => option.value === selected)) elements.port.value = selected;
      elements.connect.disabled = connected ? false : !elements.port.value;
    } catch (error) { toast(error.message, true); }
    finally { elements.refresh.disabled = connected; }
  }

  async function toggleConnection() {
    if (connected) {
      try { await api("/api/disconnect", { method: "POST", body: "{}" }); }
      catch (error) { toast(error.message, true); }
      return;
    }
    const outputSignals = activeHardwareModule().connectionSignals();
    const config = {
      port: elements.port.value, baud: currentBaud(), dataBits: Number(elements.dataBits.value),
      parity: elements.parity.value, stopBits: elements.stopBits.value,
      dtr: outputSignals.dtr, rts: outputSignals.rts
    };
    elements.connect.disabled = true;
    try { applyStatus(await api("/api/connect", { method: "POST", body: JSON.stringify(config) })); saveSettings(); }
    catch (error) { toast(error.message, true); }
    finally { elements.connect.disabled = connected ? false : !elements.port.value; }
  }

  function applyStatus(status) {
    const wasConnected = connected;
    connected = Boolean(status.connected);
    elements.connect.classList.toggle("connected", connected);
    elements.connectLabel.textContent = connected ? "Disconnect" : "Connect";
    elements.send.disabled = !connected;
    elements.sendInput.disabled = !connected;
    elements.port.disabled = connected;
    elements.baud.disabled = connected;
    elements.customBaud.disabled = connected;
    elements.refresh.disabled = connected;
    elements.settingsButton.disabled = connected;
    elements.hardware.disabled = connected;
    elements.connect.disabled = connected ? false : !elements.port.value;
    if (connected && status.config) {
      elements.port.value = status.config.port;
      setBaud(status.config.baud);
      signals = { ...signals, connected: true, dtr: status.config.dtr, rts: status.config.rts };
      elements.terminalEmpty.hidden = true;
      if (!wasConnected) { toast(`Connected to ${status.config.port} at ${status.config.baud} baud`); elements.terminal.focus(); }
    } else if (wasConnected) {
      signals = { connected: false, available: false, dtr: false, rts: false, cts: false, dsr: false, ri: false, dcd: false };
      toast(status.error ? `Serial link closed: ${status.error}` : "Serial port disconnected", Boolean(status.error));
    }
    document.title = connected ? "✅ Vicuña — Connected" : "❌ Vicuña — Disconnected";
    renderHardwarePanel();
  }

  function applySignals(value) {
    signals = { ...signals, ...value };
    renderHardwarePanel();
  }

  function connectEventStream() {
    if (eventSource) eventSource.close();
    eventSource = new EventSource("/api/events");
    elements.stream.className = "stream-status";
    eventSource.onopen = () => { elements.stream.className = "stream-status online"; elements.stream.innerHTML = "<i></i> UI link"; };
    eventSource.onerror = () => { elements.stream.className = "stream-status offline"; elements.stream.innerHTML = "<i></i> Reconnecting"; };
    eventSource.onmessage = (message) => {
      try {
        const value = JSON.parse(message.data);
        if (value.type === "status" && value.status) applyStatus(value.status);
        if (value.type === "signals" && value.signals) applySignals(value.signals);
        if (value.type === "data" && value.data) receiveData(value.direction, base64Bytes(value.data), new Date(value.time));
      } catch (error) { console.warn("Bad event", error); }
    };
  }

  function base64Bytes(value) {
    const binary = atob(value), bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
    return bytes;
  }

  function bytesBase64(bytes) {
    let binary = "";
    for (let start = 0; start < bytes.length; start += 0x8000) binary += String.fromCharCode(...bytes.subarray(start, start + 0x8000));
    return btoa(binary);
  }

  function receiveData(direction, bytes, time) {
    elements.terminalEmpty.hidden = true;
    if (direction === "rx" || elements.localEcho.checked) terminal.writeBytes(bytes, direction);
    appendMonitor(direction, bytes, time);
    appendHex(direction, bytes, time);
    scheduleLogRender();
  }

  function appendMonitor(direction, bytes, time) {
    let text = monitorDecoders[direction].decode(bytes, { stream: true });
    let pending = monitorPending[direction];
    if (!pending) pending = { time, direction, text: "" };
    for (let i = 0; i < text.length; i++) {
      const char = text[i];
      if (char === "\r" || char === "\n") {
        if (char === "\r" && text[i + 1] === "\n") i++;
        monitorEntries.push(pending);
        pending = { time, direction, text: "" };
      } else {
        pending.text += char;
        if (pending.text.length >= 8192) { monitorEntries.push(pending); pending = { time, direction, text: "" }; }
      }
    }
    monitorPending[direction] = pending;
    if (monitorEntries.length > maxMonitorLines) monitorEntries.splice(0, monitorEntries.length - maxMonitorLines);
  }

  function appendHex(direction, bytes, time) {
    for (let start = 0; start < bytes.length; start += 16) {
      const data = bytes.slice(start, start + 16);
      hexRows.push({ time, direction, offset: hexOffsets[direction], data });
      hexOffsets[direction] += data.length;
    }
    if (hexRows.length > maxHexRows) hexRows.splice(0, hexRows.length - maxHexRows);
  }

  function scheduleLogRender() {
    if (logRenderPending) return;
    logRenderPending = true;
    setTimeout(() => { logRenderPending = false; renderMonitor(); renderHex(); }, 50);
  }

  function renderMonitor() {
    const rows = monitorEntries.slice(-1500);
    for (const pending of Object.values(monitorPending)) if (pending && pending.text) rows.push(pending);
    if (!rows.length) { elements.monitor.innerHTML = '<div class="empty-log">Decoded serial text will appear here.</div>'; return; }
    elements.monitor.innerHTML = rows.map((entry) => `<div class="monitor-row ${entry.direction}"><span class="time-cell">${formatTime(entry.time)}</span><span class="direction-cell ${entry.direction}">${entry.direction.toUpperCase()}</span><span class="monitor-text">${renderMonitorText(entry.text)}</span></div>`).join("");
    follow(elements.monitor.parentElement);
  }

  function renderMonitorText(text) {
    if (elements.ansi.checked) return ansiTextHTML(text);
    return escapeHTML(visibleControls(stripANSI(text)));
  }

  function ansiTextHTML(text) {
    let result = "", last = 0, attr = { fg: null, bg: null, bold: false }, regex = /\x1b\[([0-9;]*)m/g, match;
    const span = (value) => {
      if (!value) return "";
      const style = [attr.fg && `color:${attr.fg}`, attr.bg && `background:${attr.bg}`, attr.bold && "font-weight:700"].filter(Boolean).join(";");
      const escaped = escapeHTML(visibleControls(value));
      return style ? `<span style="${style}">${escaped}</span>` : escaped;
    };
    while ((match = regex.exec(text))) {
      result += span(stripANSI(text.slice(last, match.index)));
      const codes = match[1] ? match[1].split(";").map(Number) : [0];
      for (const code of codes) {
        if (code === 0) attr = { fg: null, bg: null, bold: false };
        else if (code === 1) attr.bold = true;
        else if (code === 22) attr.bold = false;
        else if (code >= 30 && code <= 37) attr.fg = ansiPalette[code - 30];
        else if (code >= 90 && code <= 97) attr.fg = ansiPalette[code - 90 + 8];
        else if (code === 39) attr.fg = null;
        else if (code >= 40 && code <= 47) attr.bg = ansiPalette[code - 40];
        else if (code === 49) attr.bg = null;
      }
      last = regex.lastIndex;
    }
    result += span(stripANSI(text.slice(last)));
    return result;
  }

  function stripANSI(text) { return text.replace(/\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\)?|.)/g, ""); }
  function visibleControls(text) {
    return text.replace(/[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]/g, (char) => char === "\x7f" ? "␡" : String.fromCodePoint(0x2400 + char.charCodeAt(0)));
  }

  function renderHex() {
    if (!hexRows.length) { elements.hex.innerHTML = '<div class="empty-log">Binary-safe serial data will appear here.</div>'; return; }
    elements.hex.innerHTML = hexRows.slice(-1500).map((row) => {
      const bytes = [...row.data], first = bytes.slice(0, 8).map(hexByte).join(" "), second = bytes.slice(8).map(hexByte).join(" ");
      const hex = `${first.padEnd(23, " ")}  ${second.padEnd(23, " ")}`;
      const ascii = bytes.map((byte) => byte >= 32 && byte <= 126 ? String.fromCharCode(byte) : ".").join("");
      return `<div class="hex-row ${row.direction}"><span class="time-cell">${formatTime(row.time)}</span><span class="direction-cell ${row.direction}">${row.direction.toUpperCase()}</span><span class="offset-cell">${row.offset.toString(16).toUpperCase().padStart(8,"0")}</span><span class="byte-cell">${hex}</span><span class="ascii-cell">${escapeHTML(ascii)}</span></div>`;
    }).join("");
    follow(elements.hex.parentElement);
  }

  function hexByte(value) { return value.toString(16).toUpperCase().padStart(2, "0"); }
  function formatTime(value) {
    const date = value instanceof Date ? value : new Date(value);
    return `${date.toLocaleTimeString([], { hour12: false, hour: "2-digit", minute: "2-digit", second: "2-digit" })}.${String(date.getMilliseconds()).padStart(3,"0")}`;
  }
  function follow(node) { if (elements.autoscroll.checked && activeView !== "terminal") node.scrollTop = node.scrollHeight; }

  async function sendComposer() {
    let data = elements.sendInput.value;
    if (!data || !connected) return;
    if (sendMode === "text") data += ({ crlf: "\r\n", lf: "\n", cr: "\r", none: "" })[elements.lineEnding.value];
    try {
      await api("/api/send", { method: "POST", body: JSON.stringify({ encoding: sendMode, data }) });
      elements.sendInput.value = "";
    } catch (error) { toast(error.message, true); }
  }

  let directChunks = [], directTimer = null;
  function queueDirect(text) {
    if (!connected || !text) return;
    directChunks.push(encoder.encode(text));
    if (!directTimer) directTimer = setTimeout(flushDirect, 8);
  }
  async function flushDirect() {
    directTimer = null;
    const size = directChunks.reduce((sum, chunk) => sum + chunk.length, 0), bytes = new Uint8Array(size);
    let offset = 0; for (const chunk of directChunks) { bytes.set(chunk, offset); offset += chunk.length; }
    directChunks = [];
    try { await api("/api/send", { method: "POST", body: JSON.stringify({ encoding: "base64", data: bytesBase64(bytes) }) }); }
    catch (error) { toast(error.message, true); }
  }

  function terminalKey(event) {
    if (!connected || event.metaKey) return;
    let output = null;
    const cursorPrefix = terminal.appCursor ? "\x1bO" : "\x1b[";
    const keys = {
      Enter: "\r", Backspace: "\x7f", Tab: "\t", Escape: "\x1b",
      ArrowUp: `${cursorPrefix}A`, ArrowDown: `${cursorPrefix}B`, ArrowRight: `${cursorPrefix}C`, ArrowLeft: `${cursorPrefix}D`,
      Home: "\x1b[H", End: "\x1b[F", Insert: "\x1b[2~", Delete: "\x1b[3~", PageUp: "\x1b[5~", PageDown: "\x1b[6~",
      F1: "\x1bOP", F2: "\x1bOQ", F3: "\x1bOR", F4: "\x1bOS", F5: "\x1b[15~", F6: "\x1b[17~",
      F7: "\x1b[18~", F8: "\x1b[19~", F9: "\x1b[20~", F10: "\x1b[21~", F11: "\x1b[23~", F12: "\x1b[24~"
    };
    if (event.ctrlKey && event.key.length === 1) {
      const code = event.key.toUpperCase().charCodeAt(0);
      if (code >= 64 && code <= 95) output = String.fromCharCode(code - 64);
    } else if (keys[event.key] !== undefined) output = keys[event.key];
    else if (event.key.length === 1 && !event.altKey) output = event.key;
    if (event.altKey && event.key.length === 1 && !event.ctrlKey) output = `\x1b${event.key}`;
    if (output !== null) { event.preventDefault(); queueDirect(output); }
  }

  function terminalPaste(event) {
    if (!connected) return;
    event.preventDefault();
    let text = event.clipboardData.getData("text");
    if (terminal.bracketedPaste) text = `\x1b[200~${text}\x1b[201~`;
    queueDirect(text);
  }

  function setView(view) {
    activeView = ["terminal", "monitor", "hex"].includes(view) ? view : "terminal";
    $$(".view-tab").forEach((tab) => { const active = tab.dataset.view === activeView; tab.classList.toggle("active", active); tab.setAttribute("aria-selected", String(active)); });
    $$(".console-view").forEach((panel) => { const active = panel.id === `${activeView}View`; panel.classList.toggle("active", active); panel.hidden = !active; });
    saveSettings();
    if (activeView === "terminal") { terminal.resize(); if (connected) elements.terminal.focus(); }
  }

  function setSendMode(mode) {
    sendMode = mode === "hex" ? "hex" : "text";
    $$(".send-mode").forEach((button) => button.classList.toggle("active", button.dataset.mode === sendMode));
    elements.sendInput.placeholder = sendMode === "hex" ? "e.g. 55 AA 00 FF" : "Type a command…";
    elements.lineEnding.closest("label").style.display = sendMode === "hex" ? "none" : "flex";
    saveSettings();
  }

  function updateTimestampClass() { elements.workspace.classList.toggle("timestamps-off", !elements.timestamps.checked); }
  function updateFramingSummary() {
    const parity = elements.parity.value === "none" ? "no parity" : `${elements.parity.value} parity`;
    elements.framing.textContent = `${elements.dataBits.value} data bits · ${parity} · ${elements.stopBits.value} stop bit${elements.stopBits.value === "1" ? "" : "s"}`;
    const parityCode = elements.parity.value === "none" ? "N" : elements.parity.value[0].toUpperCase();
    elements.framingInline.textContent = `${elements.dataBits.value}-${parityCode}-${elements.stopBits.value}`;
  }

  function positionPopover(button, popover) {
    const rect = button.getBoundingClientRect();
    popover.hidden = false;
    const width = popover.offsetWidth;
    popover.style.top = `${rect.bottom + 8}px`;
    popover.style.left = `${Math.max(8, Math.min(window.innerWidth - width - 8, rect.right - width))}px`;
    button.setAttribute("aria-expanded", "true");
  }

  function togglePopover(button, popover) {
    const wasHidden = popover.hidden;
    closePopovers();
    if (wasHidden) positionPopover(button, popover);
  }
  function closePopovers() { for (const [button, popover] of [[elements.settingsButton, elements.settingsPopover], [elements.more, elements.linePopover]]) { popover.hidden = true; button.setAttribute("aria-expanded", "false"); } }

  function clearViews() {
    terminal.clearDisplay(); monitorEntries = []; monitorPending = { rx: null, tx: null }; hexRows = []; hexOffsets = { rx: 0, tx: 0 };
    scheduleLogRender();
  }

  function exportActiveView() {
    let content, extension;
    if (activeView === "terminal") { content = terminal.dump(); extension = "txt"; }
    else if (activeView === "monitor") {
      const rows = [...monitorEntries, ...Object.values(monitorPending).filter((row) => row && row.text)];
      content = rows.map((row) => `${formatTime(row.time)} ${row.direction.toUpperCase()} ${stripANSI(row.text)}`).join("\n"); extension = "log";
    } else {
      content = hexRows.map((row) => `${formatTime(row.time)} ${row.direction.toUpperCase()} ${row.offset.toString(16).padStart(8,"0")}  ${[...row.data].map(hexByte).join(" ")}`).join("\n"); extension = "hex.txt";
    }
    const link = document.createElement("a"), blob = new Blob([content], { type: "text/plain;charset=utf-8" });
    link.href = URL.createObjectURL(blob); link.download = `vicuna-${activeView}-${new Date().toISOString().replace(/[:.]/g,"-")}.${extension}`; link.click();
    setTimeout(() => URL.revokeObjectURL(link.href), 1000);
  }

  function toast(message, error = false) {
    const node = document.createElement("div"); node.className = `toast${error ? " error" : ""}`; node.textContent = message;
    elements.toast.appendChild(node); setTimeout(() => node.remove(), error ? 6000 : 3200);
  }

  function bindEvents() {
    elements.refresh.addEventListener("click", refreshPorts);
    elements.connect.addEventListener("click", toggleConnection);
    elements.port.addEventListener("change", () => { elements.connect.disabled = !elements.port.value; saveSettings(); });
    elements.hardware.addEventListener("change", () => { renderHardwarePanel(); saveSettings(); });
    elements.hardwarePanel.addEventListener("click", async (event) => {
      const button = event.target.closest("[data-control]");
      if (!button) return;
      button.disabled = true;
      try { await activeHardwareModule().toggleControl(button.dataset.control); saveSettings(); renderHardwarePanel(); }
      catch (error) { button.disabled = false; toast(error.message, true); }
    });
    elements.baud.addEventListener("change", () => { elements.customBaudField.hidden = elements.baud.value !== "custom"; if (!elements.customBaudField.hidden) elements.customBaud.focus(); saveSettings(); });
    elements.customBaud.addEventListener("change", saveSettings);
    elements.settingsButton.addEventListener("click", (event) => { event.stopPropagation(); togglePopover(elements.settingsButton, elements.settingsPopover); });
    elements.more.addEventListener("click", (event) => { event.stopPropagation(); togglePopover(elements.more, elements.linePopover); });
    for (const input of [elements.dataBits, elements.parity, elements.stopBits]) input.addEventListener("change", () => { updateFramingSummary(); saveSettings(); });
    $$(".view-tab").forEach((tab) => tab.addEventListener("click", () => setView(tab.dataset.view)));
    $$(".send-mode").forEach((button) => button.addEventListener("click", () => setSendMode(button.dataset.mode)));
    elements.timestamps.addEventListener("change", () => { updateTimestampClass(); saveSettings(); });
    elements.ansi.addEventListener("change", () => { terminal.colorsEnabled = elements.ansi.checked; terminal.scheduleRender(); scheduleLogRender(); saveSettings(); });
    elements.autoscroll.addEventListener("change", saveSettings);
    elements.theme.addEventListener("click", () => { applyTheme(settings.theme === "dark" ? "light" : "dark"); terminal.scheduleRender(); scheduleLogRender(); saveSettings(); });
    elements.localEcho.addEventListener("change", saveSettings);
    elements.lineEnding.addEventListener("change", saveSettings);
    elements.clear.addEventListener("click", clearViews);
    elements.export.addEventListener("click", exportActiveView);
    elements.send.addEventListener("click", sendComposer);
    elements.sendInput.addEventListener("keydown", (event) => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); sendComposer(); } });
    elements.terminal.addEventListener("keydown", terminalKey);
    elements.terminal.addEventListener("paste", terminalPaste);
    elements.terminal.addEventListener("mousedown", () => elements.terminal.focus());
    elements.sendBreak.addEventListener("click", async () => { try { await api("/api/control", { method: "POST", body: JSON.stringify({ action: "break", durationMs: 250 }) }); toast("250 ms break sent"); } catch (error) { toast(error.message, true); } });
    elements.resetBuffers.addEventListener("click", async () => { try { await api("/api/control", { method: "POST", body: JSON.stringify({ action: "reset-buffers" }) }); toast("Serial buffers reset"); } catch (error) { toast(error.message, true); } });
    document.addEventListener("click", (event) => { if (!event.target.closest(".popover")) closePopovers(); });
    window.addEventListener("resize", () => { closePopovers(); terminal.resize(); });
  }

  async function init() {
    try { applyDeploymentConfig(await api("/api/config")); } catch (error) { toast(`Configuration unavailable: ${error.message}`, true); }
    activeView = settings.view || "terminal";
    applySavedSettings(); bindEvents(); terminal.resize(); renderMonitor(); renderHex(); connectEventStream();
    try { await loadHardwareModules(); } catch (error) { toast(`Hardware definitions unavailable: ${error.message}`, true); }
    const portsReady = refreshPorts();
    try { applyStatus(await api("/api/status")); applySignals(await api("/api/signals")); } catch (error) { toast(error.message, true); }
    await portsReady;
  }

  init();
})();
