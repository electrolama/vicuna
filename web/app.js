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
    terminal: $("#terminal"), terminalEmpty: $("#terminalEmpty"), terminalSize: $("#terminalSize"),
    terminalSizeCommand: $("#terminalSizeCommand"), copyTerminalSize: $("#copyTerminalSize"),
    appVersion: $("#appVersion"), appBuild: $("#appBuild"),
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
    constructor(container) {
      if (typeof Terminal !== "function" || !globalThis.FitAddon?.FitAddon) {
        throw new Error("xterm.js failed to load");
      }
      this.container = container;
      this.fitAddon = new globalThis.FitAddon.FitAddon();
      this.instance = new Terminal({
        convertEol: false,
        cursorBlink: true,
        cursorStyle: "block",
        disableStdin: true,
        drawBoldTextInBrightColors: true,
        fontFamily: '"Cascadia Code", "SFMono-Regular", Consolas, "Liberation Mono", monospace',
        fontSize: 13,
        lineHeight: 1.35,
        macOptionIsMeta: true,
        minimumContrastRatio: 1,
        scrollback: 5000,
        windowOptions: {
          getCellSizePixels: true,
          getWinSizeChars: true,
          getWinSizePixels: true
        }
      });
      this.instance.loadAddon(this.fitAddon);
      this.instance.open(container);
      this.instance.onResize(({ cols, rows }) => this.updateGeometry(cols, rows));
      this.inputBound = false;
      this.resizeQueued = false;
      this.resizeObserver = typeof ResizeObserver === "function" ? new ResizeObserver(() => this.resize()) : null;
      this.resizeObserver?.observe(container);
      this.setTheme(settings.theme || "dark", settings.ansi !== false);
      this.updateGeometry(this.instance.cols, this.instance.rows);
    }

    bindInput() {
      if (this.inputBound) return;
      this.inputBound = true;
      this.instance.onData((data) => queueDirect(data));
      this.instance.onBinary((data) => queueDirectBytes(Uint8Array.from(data, (char) => char.charCodeAt(0) & 0xff)));
    }

    writeBytes(bytes) {
      const viewport = this.instance.buffer.active.viewportY;
      this.instance.write(bytes, () => {
        if (elements.autoscroll.checked) this.instance.scrollToBottom();
        else this.instance.scrollToLine(viewport);
      });
    }

    resize() {
      if (this.resizeQueued || !this.container.clientWidth || !this.container.clientHeight) return;
      this.resizeQueued = true;
      requestAnimationFrame(() => {
        this.resizeQueued = false;
        if (!this.container.clientWidth || !this.container.clientHeight) return;
        this.fitAddon.fit();
        this.updateGeometry(this.instance.cols, this.instance.rows);
      });
    }

    updateGeometry(cols, rows) {
      if (!elements.terminalSize || !elements.terminalSizeCommand) return;
      elements.terminalSize.textContent = `${cols} × ${rows}`;
      elements.terminalSizeCommand.textContent = `stty rows ${rows} cols ${cols}`;
    }

    setTheme(theme, colorsEnabled) {
      const light = theme === "light";
      const palette = ansiPalettes[light ? "light" : "dark"];
      const foreground = light ? "#1f1f1f" : "#d4d4d4";
      const background = light ? "#ffffff" : "#181818";
      const colors = colorsEnabled ? palette : Array(16).fill(foreground);
      this.instance.options.theme = {
        foreground,
        background,
        cursor: light ? "#16825d" : "#8affca",
        cursorAccent: background,
        selectionBackground: light ? "#0066b840" : "#4daafc40",
        black: colors[0], red: colors[1], green: colors[2], yellow: colors[3],
        blue: colors[4], magenta: colors[5], cyan: colors[6], white: colors[7],
        brightBlack: colors[8], brightRed: colors[9], brightGreen: colors[10], brightYellow: colors[11],
        brightBlue: colors[12], brightMagenta: colors[13], brightCyan: colors[14], brightWhite: colors[15]
      };
    }

    setInputEnabled(enabled) { this.instance.options.disableStdin = !enabled; }
    focus() { this.instance.focus(); }

    clearDisplay() {
      this.instance.reset();
      this.instance.clear();
    }

    dump() {
      const buffer = this.instance.buffer.active;
      const output = [];
      let logicalLine = "";
      for (let row = 0; row < buffer.length; row++) {
        const line = buffer.getLine(row);
        if (!line) continue;
        const text = line.translateToString(true);
        if (line.isWrapped) logicalLine += text;
        else {
          if (row > 0) output.push(logicalLine.trimEnd());
          logicalLine = text;
        }
      }
      output.push(logicalLine.trimEnd());
      return output.join("\n").replace(/\n+$/, "");
    }
  }

  const terminal = new TerminalEmulator(elements.terminal);

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
    connectionSignals() { return { dtr: !Boolean(settings.pt1Vbus), rts: false }; }
    render() {
      const vbus = connected ? !signals.dtr : Boolean(settings.pt1Vbus);
      elements.hardwarePanel.innerHTML = this.toggle("vbus", "VBUS", vbus, "ON", "OFF") + this.indicator("ri", "Overcurrent", true, "FAULT", "CLEAR");
    }
    async toggleControl(control) {
      if (control !== "vbus") return;
      const current = connected ? !signals.dtr : Boolean(settings.pt1Vbus);
      const value = !current;
      if (connected) await this.setControl(control, value);
      settings.pt1Vbus = value;
      if (connected) signals.dtr = !value;
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
      pt1Vbus: config.hardware === "pt1" ? !Boolean(serial.dtr) : false
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
    terminal.setTheme(settings.theme, settings.ansi);
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
    settings.theme = theme === "light" ? "light" : "dark";
    ansiPalette = ansiPalettes[settings.theme];
    document.documentElement.dataset.theme = settings.theme;
    document.documentElement.style.colorScheme = settings.theme;
    terminal.setTheme(settings.theme, elements.ansi.checked);
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
      if (!ports.length) elements.port.add(new Option("No serial ports found", ""));
      for (const port of ports) {
        const details = [port.product, port.usb && port.vid && port.pid ? `${port.vid}:${port.pid}` : ""].filter(Boolean).join(" · ");
        elements.port.add(new Option(details ? `${port.name} — ${details}` : port.name, port.name));
      }
      if ([...elements.port.options].some((option) => option.value === selected)) elements.port.value = selected;
      elements.connect.disabled = connected ? false : !elements.port.value;
    } catch (error) { toast(error.message, true); }
    finally { elements.refresh.disabled = connected; }
  }

  async function loadBuildInformation() {
    const information = await api("/api/about");
    elements.appVersion.textContent = information.version;
    elements.appBuild.textContent = information.build;
    elements.appBuild.title = information.build;
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
    terminal.setInputEnabled(connected);
    elements.connect.disabled = connected ? false : !elements.port.value;
    if (connected && status.config) {
      elements.port.value = status.config.port;
      setBaud(status.config.baud);
      signals = { ...signals, connected: true, dtr: status.config.dtr, rts: status.config.rts };
      elements.terminalEmpty.hidden = true;
      if (!wasConnected) { toast(`Connected to ${status.config.port} at ${status.config.baud} baud`); terminal.focus(); }
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
    if (!text) return;
    queueDirectBytes(encoder.encode(text));
  }
  function queueDirectBytes(bytes) {
    if (!connected || !bytes.length) return;
    directChunks.push(bytes);
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

  function setView(view) {
    activeView = ["terminal", "monitor", "hex"].includes(view) ? view : "terminal";
    $$(".view-tab").forEach((tab) => { const active = tab.dataset.view === activeView; tab.classList.toggle("active", active); tab.setAttribute("aria-selected", String(active)); });
    $$(".console-view").forEach((panel) => { const active = panel.id === `${activeView}View`; panel.classList.toggle("active", active); panel.hidden = !active; });
    saveSettings();
    if (activeView === "terminal") { terminal.resize(); if (connected) terminal.focus(); }
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
    terminal.bindInput();
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
    elements.ansi.addEventListener("change", () => { terminal.setTheme(settings.theme, elements.ansi.checked); scheduleLogRender(); saveSettings(); });
    elements.autoscroll.addEventListener("change", saveSettings);
    elements.theme.addEventListener("click", () => { applyTheme(settings.theme === "dark" ? "light" : "dark"); scheduleLogRender(); saveSettings(); });
    elements.localEcho.addEventListener("change", saveSettings);
    elements.lineEnding.addEventListener("change", saveSettings);
    elements.clear.addEventListener("click", clearViews);
    elements.export.addEventListener("click", exportActiveView);
    elements.send.addEventListener("click", sendComposer);
    elements.sendInput.addEventListener("keydown", (event) => { if (event.key === "Enter" && !event.shiftKey) { event.preventDefault(); sendComposer(); } });
    elements.terminal.addEventListener("mousedown", () => terminal.focus());
    elements.copyTerminalSize.addEventListener("click", async () => {
      try { await navigator.clipboard.writeText(elements.terminalSizeCommand.textContent); toast("stty command copied"); }
      catch { toast("Could not copy the terminal-size command", true); }
    });
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
    try { await loadBuildInformation(); } catch (error) { toast(`Build information unavailable: ${error.message}`, true); }
    const portsReady = refreshPorts();
    try { applyStatus(await api("/api/status")); applySignals(await api("/api/signals")); } catch (error) { toast(error.message, true); }
    await portsReady;
  }

  init();
})();
