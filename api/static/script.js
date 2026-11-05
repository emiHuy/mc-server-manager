const $ = (selector) => document.querySelector(selector);
const KEY = "mcsm.token";
const STATES = ["stopped", "starting", "running", "stopping", "crashed"];
const MAX_FAILURES = 3;
const players = new Map();
const counts = { warnings: 0, errors: 0 };

let eventsCount = 0;

let disconnected = false;
let state = "stopped";
let startedAt = 0;
let toastTimer;

const samples = []; // { t, cpu, mem, players }

class AuthError extends Error {}

const MC_PORT = 25565;
$("#addr").textContent = `${location.hostname}:${MC_PORT}`;

function getToken() {
  let token = localStorage.getItem(KEY);
  if (!token) {
    token = (prompt("Enter your access token") || "").trim();
    if (token) {
      localStorage.setItem(KEY, token);
    }
  }
  return token;
}

function toast(msg) {
  const toast = $("#toast");
  toast.textContent = msg;
  toast.hidden = false;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => {
    toast.hidden = true;
  }, 4000);
}

async function authFetch(path, { method = "GET", body } = {}) {
  const res = await fetch(path, {
    method,
    headers: {
      Authorization: "Bearer " + getToken(),
      ...(body ? { "Content-Type": "application/json" } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });

  if (res.status === 401) {
    localStorage.removeItem(KEY);
    location.reload();
    throw new AuthError("unauthorized");
  }
  if (!res.ok) {
    let msg = res.statusText;
    try {
      msg = (await res.json()).error || msg;
    } catch {}
    throw new Error(msg);
  }

  return res;
}

async function api(path, opts) {
  const res = await authFetch(path, opts);
  return res.status === 204 ? null : res.json();
}

function giveUp() {
  if (disconnected) {
    return;
  }
  disconnected = true;
  toast("Disconnected from the manager.");
}

async function stream(path, onMessage, onOpen, onStatus) {
  let failures = 0;

  while (true) {
    try {
      const res = await authFetch(path);
      failures = 0;
      onStatus?.("live");
      onOpen?.();

      const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
      let buffer = "";

      while (true) {
        const { value, done } = await reader.read();
        if (done) {
          break;
        }

        buffer += value;
        const blocks = buffer.split(/\r?\n\r?\n/);
        buffer = blocks.pop();

        for (const block of blocks) {
          const data = block
            .split("\n")
            .filter((line) => line.startsWith("data:"))
            .map((line) => line.slice(5).trim())
            .join("\n");
          if (!data) {
            continue;
          }

          try {
            onMessage(JSON.parse(data));
          } catch (e) {
            console.error("bad message", data, e);
          }
        }
      }

      throw new Error("connection closed");
    } catch (e) {
      if (e instanceof AuthError) {
        return;
      }

      console.error(path, e);
      failures++;
      if (failures >= MAX_FAILURES) {
        onStatus?.("offline");
        giveUp();
        return; // stop making requests
      }
      onStatus?.("retrying");
    }
    await new Promise((r) => setTimeout(r, 2000 * failures));
  }
}

function setConnection(s) {
  $("#conn").textContent = {
    live: "Live",
    retrying: "Reconnecting…",
    offline: "Disconnected",
  }[s];
  $("#conn").dataset.state = s;
  if (s !== "live") markStale();
}

function markStale() {
  // don't keep showing "Running" when we can't know
  $("#state-value").textContent = "Unknown";
  for (const id of ["#state-dot", "#console-dot"])
    $(id).classList.remove(...STATES);
}

function recordSample(m) {
  samples.push({
    t: Date.now(),
    cpu: m.cpu_percent,
    mem: m.memory_bytes,
    players: m.players_online,
  });

  // drop readings older than the window, so memory stays flat
  const cutoff = Date.now() - WINDOW_MS;
  while (samples.length && samples[0].t < cutoff) samples.shift();
}

function init() {
  stream("/api/server/events", handleEvent, () =>
    loadStatus().catch((e) => toast(e.message), setConnection),
  );

  stream("/api/server/metrics/stream", handleMetrics);

  stream(
    "/api/server/console/stream?lines=200",
    (d) => logLine(d.line),
    () => {
      $("#log").replaceChildren(); // the server resends recent lines on connect, so clear first
      clearUnread();
      replayUntil = Date.now() + 1000;
    },
  );
}

function handleEvent(e) {
  switch (e.type) {
    case "state_changed":
      applyState(e.state);
      break;
    case "player_joined":
      if (!players.has(e.player)) {
        players.set(e.player, Date.now());
      }
      renderPlayers();
      break;
    case "player_left":
      players.delete(e.player);
      renderPlayers();
      break;
    case "player_chat":
      addToLog($("#chat"), chatRow(e), 10);
      $("#no-chat").hidden = true;
      break;
  }

  eventsCount += 1;
  $("#events-count").textContent = eventsCount;
  bump(GROUPS[e.type]);
  addToLog($("#events"), eventRow(e));
}

function bump(group) {
  if (!(group in counts)) return;
  counts[group]++;
  $(`[data-count="${group}"]`).textContent = `(${counts[group]})`;
}

function chatRow(e) {
  const row = document.createElement("div");

  const body = document.createElement("span");
  const name = document.createElement("b");
  name.textContent = `${e.player}:`;
  body.append(name, " " + e.message);

  const time = document.createElement("time");
  time.textContent = new Date(e.timestamp).toLocaleTimeString([], {
    hour12: false,
  });

  row.append(body, time);
  return row;
}

async function loadStatus() {
  const res = await api("/api/server/status");
  applyState(res.state);
  if (res.uptime_seconds) {
    startedAt = Date.now() - res.uptime_seconds * 1000;
  }
  players.clear();
  for (const p of res.players ?? []) {
    players.set(p, Date.now());
  }
  renderPlayers();
}

function renderPlayers() {
  const rows = [...players.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([name, since]) => {
      const tr = document.createElement("tr");

      const nameCell = document.createElement("td");
      nameCell.textContent = name;

      const timeCell = document.createElement("td");
      timeCell.dataset.since = since;
      timeCell.textContent = fmtDuration(Date.now() - since);

      tr.append(nameCell, timeCell);
      return tr;
    });
  $("#player-list").replaceChildren(...rows);
  $("#no-players").hidden = players.size > 0;
  $("#player-count").textContent = `${players.size} online`;
  $("#player-value").textContent = players.size;
}

function fmtDuration(ms) {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s >= 3600) {
    return `${Math.floor(s / 3600)}h ${Math.floor(s / 60) % 60}m`;
  }
  if (s >= 60) {
    return `${Math.floor(s / 60)}m ${s % 60}s`;
  }
  return s + "s";
}

setInterval(() => {
  for (const cell of document.querySelectorAll("#player-list td[data-since]")) {
    cell.textContent = fmtDuration(Date.now() - Number(cell.dataset.since));
  }
}, 1000);

function setStateClass(dot, state) {
  dot.classList.remove(...STATES);
  dot.classList.add(state);
}

let prevState = null;
function applyState(s) {
  if (prevState !== "stopped" && s === "stopped") {
    resetRun();
  }

  prevState = state;
  state = s;

  setStateClass($("#state-dot"), s);
  setStateClass($("#console-dot"), s);

  $("#state-value").textContent = s[0].toUpperCase() + s.slice(1);
  $("#console-sub").textContent = s;

  $("#btn-start").disabled = !(s === "stopped" || s === "crashed");
  $("#btn-stop").disabled = !(s === "starting" || s === "running");
  $("#btn-kill").disabled = s === "stopped";
}

function resetRun() {
  counts.warnings = counts.errors = 0;
  for (const s of document.querySelectorAll("[data-count]")) {
    s.textContent = "";
  }
  startedAt = 0;
  players.clear();
  renderPlayers();
  $("#chat").replaceChildren();
  $("#no-chat").hidden = false; // show "No chat yet" again, if you have that element
  $("#events").replaceChildren();

  $("#uptime-value").textContent = "--:--:--";
  $("#cpu-value").textContent = "0%";
  $("#cpu-sub").textContent = "0 of 0 cores";
  $("#mem-value").textContent = "0.0 GB";
  $("#disk-value").textContent = "0%";
  $("#disk-sub").textContent = "0.0 GB free";

  // resources table
  $("#dd-cpu-core").textContent = "-";
  $("#dd-cpu-machine").textContent = "-";
  $("#dd-cores").textContent = "-";
  $("#dd-mem").textContent = "-";
  $("#dd-disk-used").textContent = "-";
  $("#dd-disk-free").textContent = "-";
  $("#dd-disk-total").textContent = "-";

  $("#disk-meter").style.width = "0";
  $("#disk-meter").classList.remove("hot");
}

const GROUPS = {
  state_changed: "state",
  server_ready: "state",
  player_joined: "players",
  player_left: "players",
  player_chat: "chat",
  server_warning: "warnings",
  server_error: "errors",
};

function eventRow(e) {
  const labels = {
    state_changed: ["state", "Changed to " + e.state],
    server_ready: ["ready", e.message],
    player_joined: ["joined", e.player + " joined the game"],
    player_left: ["left", e.player + " left the game"],
    player_chat: ["chat", `<${e.player}> ${e.message}`],
    server_warning: ["warning", e.message],
    server_error: ["error", e.message],
  };
  const [label, text] = labels[e.type] ?? ["event", e.message || e.type];

  const row = document.createElement("div");
  row.className = "event " + label;
  row.dataset.group = GROUPS[e.type] ?? "other";

  const time = document.createElement("time");
  time.className = "mono";
  time.textContent = new Date(e.timestamp).toLocaleTimeString([], {
    hour12: false,
  });

  const type = document.createElement("b");
  type.className = "mono";
  type.textContent = label;

  const message = document.createElement("span");
  message.textContent = text;

  row.append(time, type, message);
  return row;
}

function addToLog(list, row, max = 400) {
  const stuck = list.scrollTop + list.clientHeight >= list.scrollHeight - 30;
  list.append(row);
  while (list.children.length > max) {
    list.firstChild.remove();
  }
  if (stuck) {
    list.scrollTop = list.scrollHeight; // only follow if the user is at the bottom
  }
}

async function loadStatus() {
  const res = await api("/api/server/status");
  applyState(res.state);
}

async function act(path) {
  try {
    await api(path, { method: "POST" });
    await loadStatus();
  } catch (e) {
    toast(e.message);
  }
}
$("#btn-start").onclick = () => act("/api/server/start");
$("#btn-stop").onclick = () => act("/api/server/stop");
$("#btn-kill").onclick = () => act("/api/server/kill");

$("#theme").onclick = () => {
  const root = document.documentElement;
  const next = root.dataset.theme == "dark" ? "light" : "dark";
  root.dataset.theme = next;
  localStorage.setItem("mcsm.theme", next);
};

$("#event-filters").addEventListener("click", (ev) => {
  const btn = ev.target.closest("button[data-filter]");
  if (!btn) return;

  for (const b of $("#event-filters").children)
    b.classList.toggle("on", b === btn);
  $("#events").dataset.filter = btn.dataset.filter;
  $("#events").scrollTop = $("#events").scrollHeight; // jump to the newest row
});

init();

const GB = 1024 ** 3;
const fmtGB = (b) => (b / GB).toFixed(1) + " GB";
const fmtUptime = (s) =>
  [Math.floor(s / 3600), Math.floor(s / 60) % 60, s % 60]
    .map((n) => String(n).padStart(2, "0"))
    .join(":");

function handleMetrics(m) {
  if (state === "stopped") {
    return;
  }
  if (m.state !== state) {
    applyState(m.state); // safety net if an event was missed
  }
  if (m.uptime_seconds > 0) {
    startedAt = Date.now() - m.uptime_seconds * 1000;
  }

  const diskPercent = m.disk_total_bytes
    ? (m.disk_used_bytes / m.disk_total_bytes) * 100
    : 0;

  // metrics cards
  $("#cpu-value").textContent = Math.round(m.cpu_machine_percent) + "%";
  $("#cpu-sub").textContent =
    `${(m.cpu_percent / 100).toFixed(1)} of ${m.cpu_cores} cores`;
  $("#mem-value").textContent = fmtGB(m.memory_bytes);
  $("#disk-value").textContent = Math.round(diskPercent) + "%";
  $("#disk-sub").textContent = fmtGB(m.disk_free_bytes) + " free";

  // resources table
  $("#dd-cpu-core").textContent = m.cpu_percent.toFixed(1) + "%";
  $("#dd-cpu-machine").textContent = m.cpu_machine_percent.toFixed(1) + "%";
  $("#dd-cores").textContent = m.cpu_cores;
  $("#dd-mem").textContent = fmtGB(m.memory_bytes);
  $("#dd-disk-used").textContent = fmtGB(m.disk_used_bytes);
  $("#dd-disk-free").textContent = fmtGB(m.disk_free_bytes);
  $("#dd-disk-total").textContent = fmtGB(m.disk_total_bytes);

  $("#disk-meter").style.width = diskPercent + "%";
  $("#disk-meter").classList.toggle("hot", diskPercent > 85);

  recordSample(m);
  drawCharts();
}

setInterval(() => {
  const live = state === "running" || state === "stopping";
  if (live) {
    $("#uptime-value").textContent = startedAt
      ? fmtUptime(Math.floor((Date.now() - startedAt) / 1000))
      : "--:--:--";
  }
}, 1000);

let unread = 0;
let replayUntil = 0; // ignore the lines the server replays when the stream connects

function addUnread() {
  unread++;
  $("#unread").textContent = unread > 99 ? "99+" : unread;
  $("#unread").hidden = false;
}

function clearUnread() {
  unread = 0;
  $("#unread").hidden = true;
}

function logLine(line, cls = "") {
  const div = document.createElement("div");
  div.textContent = line;
  div.className =
    cls ||
    (line.includes("/WARN]")
      ? "warn"
      : line.includes("/ERROR]")
        ? "error"
        : "");
  addToLog($("#log"), div, 500);

  if ($("#console-window").hidden) {
    if ($("#console-window").hidden && Date.now() > replayUntil) addUnread();
  }
}

$("#btn-console").onclick = () => {
  const win = $("#console-window");
  win.hidden = !win.hidden;
  if (!win.hidden) {
    unread = 0;
    $("#unread").hidden = true;
    $("#log").scrollTop = $("#log").scrollHeight;
    $("#cmd").focus();
  }
};

async function sendCommand() {
  const input = $("#cmd");
  const command = input.value.trim();
  if (!command) return;
  input.value = "";
  logLine("> " + command, "echo");
  try {
    await api("/api/server/command", { method: "POST", body: { command } });
  } catch (e) {
    toast(e.message);
  }
}
$("#send").onclick = sendCommand;
$("#cmd").addEventListener("keydown", (e) => {
  if (e.key === "Enter") sendCommand();
});

function closeConsole() {
  $("#console-window").hidden = true;
}

$("#console-clear").onclick = () => {
  $("#log").replaceChildren();
};

let savedStyle = null;

$("#console-max").onclick = () => {
  const consoleWin = $("#console-window");
  const maximize = !consoleWin.classList.contains("max");

  if (maximize) {
    savedStyle = consoleWin.getAttribute("style"); // whatever drag or resize left behind
    consoleWin.removeAttribute("style");
    consoleWin.classList.add("max");
  } else {
    consoleWin.classList.remove("max");
    if (savedStyle) consoleWin.setAttribute("style", savedStyle);
    else consoleWin.removeAttribute("style");
  }

  $("#console-max").textContent = maximize ? "Restore" : "Maximize";
};
$("#console-close").onclick = closeConsole;
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && !$("#console-window").hidden) closeConsole();
});

const MIN_SPAN_MS = 2 * 60 * 1000; // never zoom in further than 2 minutes

const WINDOW_MS = 60 * 15 * 1000;

function fmtAgo(ms) {
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s ago`;

  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);

  if (h === 0) return `${m}m ago`;
  return m === 0 ? `${h}h ago` : `${h}h ${m}m ago`;
}

function drawChart(prefix, pick, max, fmt) {
  const now = Date.now();
  const span = Math.min(WINDOW_MS, Math.max(MIN_SPAN_MS, now - samples[0].t));
  const start = now - span;
  const x = (t) => ((t - start) / span) * 100;
  const y = (t) => 100 - Math.min(t / max, 1) * 100;

  const segments = [];
  let seg = [];
  for (const s of samples) {
    if (seg.length && s.t - seg[seg.length - 1].t > 10_000) {
      segments.push(seg);
      seg = [];
    }
    seg.push(s);
  }
  if (seg.length) {
    segments.push(seg);
  }

  const grid = [25, 50, 75]
    .map((p) => `<line x1="0" x2="100" y1="${p}" y2="${p}" class="grid"/>`)
    .join("");

  const lines = segments
    .map((sg) => {
      if (sg.length < 2) return "";
      const pts = sg
        .map((s) => `${x(s.t).toFixed(2)},${y(pick(s)).toFixed(2)}`)
        .join(" ");
      const first = x(sg[0].t).toFixed(2);
      const last = x(sg[sg.length - 1].t).toFixed(2);
      return `<polygon points="${first},100 ${pts} ${last},100" class="area"/>
              <polyline points="${pts}" class="line"/>`;
    })
    .join("");

  $(`#${prefix}-chart`).innerHTML = grid + lines; // only numbers go in here, never server text
  $(`#${prefix}-max`).textContent = fmt(max);
  $(`#${prefix}-mid`).textContent = fmt(max / 2);
  $(`#${prefix}-start`).textContent = fmtAgo(span);

  const vis = samples.filter((s) => s.t >= start).map(pick);
  if (vis.length) {
    const latest = vis[vis.length - 1];
    const avg = vis.reduce((a, b) => a + b, 0) / vis.length;
    const peak = Math.max(...vis);
    $(`#${prefix}-stats`).textContent =
      `now ${fmt(latest)} · avg ${fmt(avg)} · max ${fmt(peak)}`;
  }
}

const niceMax = (v, step, min) =>
  Math.max(min, Math.ceil((v * 1.15) / step) * step);

function drawCharts() {
  if (document.hidden || samples.length < 2) return;
  const maxOf = (pick) => Math.max(...samples.map(pick));

  drawChart(
    "cpu",
    (s) => s.cpu,
    niceMax(
      maxOf((s) => s.cpu),
      25,
      100,
    ),
    (v) => Math.round(v) + "%",
  );
  drawChart(
    "mem",
    (s) => s.mem,
    niceMax(
      maxOf((s) => s.mem),
      GB / 2,
      2 * GB,
    ),
    fmtGB,
  );
  drawChart(
    "players",
    (s) => s.players,
    niceMax(
      maxOf((s) => s.players),
      2,
      4,
    ),
    (v) => String(Math.round(v)),
  );
}

function setupConsoleWindow() {
  const win = $("#console-window");
  const view = document.documentElement;

  // shared by dragging and resizing: pin to left/top, then follow the pointer
  function drag(handle, makeMove) {
    handle.addEventListener("pointerdown", (e) => {
      if (
        e.target.closest("button") ||
        win.classList.contains("maximized") ||
        matchMedia("(max-width: 780px)").matches
      ) {
        return;
      }
      e.preventDefault();

      const r = win.getBoundingClientRect();
      Object.assign(win.style, {
        left: r.left + "px",
        top: r.top + "px",
        right: "auto",
        bottom: "auto",
        width: r.width + "px",
        height: r.height + "px",
      });

      const onMove = makeMove(e, r);
      handle.setPointerCapture(e.pointerId);
      handle.addEventListener("pointermove", onMove);
      handle.addEventListener(
        "lostpointercapture",
        () => handle.removeEventListener("pointermove", onMove),
        { once: true },
      );
    });
  }

  const clamp = (n, max) => Math.min(Math.max(n, 0), Math.max(0, max));

  drag($("#console-bar"), (e, r) => (ev) => {
    win.style.left =
      clamp(
        r.left + ev.clientX - e.clientX,
        view.clientWidth - win.offsetWidth,
      ) + "px";
    win.style.top =
      clamp(
        r.top + ev.clientY - e.clientY,
        view.clientHeight - win.offsetHeight,
      ) + "px";
  });

  drag($("#console-resize"), (e, r) => (ev) => {
    win.style.width =
      Math.min(view.clientWidth - r.left, r.width + ev.clientX - e.clientX) +
      "px";
    win.style.height =
      Math.min(view.clientHeight - r.top, r.height + ev.clientY - e.clientY) +
      "px";
  });
}

setupConsoleWindow();
