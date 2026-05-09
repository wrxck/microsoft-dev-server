const state = { tab: "mail", items: { mail: [], meetings: [] }, selected: null };

const els = {
  tabs: document.querySelectorAll(".tab"),
  mailList: document.getElementById("mail-list"),
  meetingsList: document.getElementById("meetings-list"),
  detail: document.getElementById("detail"),
  refresh: document.getElementById("refresh"),
  clear: document.getElementById("clear"),
};

function activeListEl() {
  return state.tab === "mail" ? els.mailList : els.meetingsList;
}

async function load() {
  const url = state.tab === "mail" ? "/_dev/mail" : "/_dev/meetings";
  const res = await fetch(url);
  const data = await res.json();
  state.items[state.tab] = data || [];
  render();
}

async function clearAll() {
  const url = state.tab === "mail" ? "/_dev/mail" : "/_dev/meetings";
  await fetch(url, { method: "DELETE" });
  state.selected = null;
  await load();
}

function render() {
  const list = activeListEl();
  const items = state.items[state.tab];
  list.innerHTML = "";
  if (!items.length) {
    list.innerHTML = `<div class="empty">No captured ${state.tab} yet.</div>`;
    return;
  }
  for (const item of items) {
    const row = document.createElement("div");
    row.className = "row";
    if (state.selected && state.selected.id === item.id) row.classList.add("selected");
    if (state.tab === "mail") {
      row.innerHTML = `
        <div class="top">
          <span class="subject">${escapeHTML(item.subject || "(no subject)")}</span>
          <span class="ts">${formatTime(item.receivedAt)}</span>
        </div>
        <div class="to">to: ${escapeHTML((item.to || []).join(", "))}</div>
      `;
    } else {
      row.innerHTML = `
        <div class="top">
          <span class="subject">${escapeHTML(item.subject || "(no subject)")}</span>
          <span class="ts">${formatTime(item.createdAt)}</span>
        </div>
        <div class="to">${escapeHTML(item.startDateTime || "")} → ${escapeHTML(item.endDateTime || "")}</div>
      `;
    }
    row.onclick = () => {
      state.selected = item;
      render();
      renderDetail();
    };
    list.appendChild(row);
  }
  if (!state.selected && items.length) {
    state.selected = items[0];
    list.firstChild.classList.add("selected");
    renderDetail();
  } else if (state.selected) {
    renderDetail();
  }
}

function renderDetail() {
  const item = state.selected;
  if (!item) {
    els.detail.innerHTML = `<p class="muted">Select an item to inspect.</p>`;
    return;
  }
  if (state.tab === "mail") {
    els.detail.innerHTML = `
      <h2>${escapeHTML(item.subject || "(no subject)")}</h2>
      <div class="meta">
        from <strong>${escapeHTML(item.from)}</strong> to <strong>${escapeHTML((item.to || []).join(", "))}</strong>
        · ${formatTime(item.receivedAt)}
        ${item.messageId ? "· message-id <code>" + escapeHTML(item.messageId) + "</code>" : ""}
      </div>
      <h3>Body (${escapeHTML(item.bodyType || "")})</h3>
      <pre>${escapeHTML(item.bodyContent || "")}</pre>
      <h3>Raw request</h3>
      <pre>${escapeHTML(prettyJSON(item.rawRequest))}</pre>
    `;
  } else {
    els.detail.innerHTML = `
      <h2>${escapeHTML(item.subject || "(no subject)")}</h2>
      <div class="meta">${escapeHTML(item.startDateTime)} → ${escapeHTML(item.endDateTime)} · ${formatTime(item.createdAt)}</div>
      <h3>Join URL</h3>
      <pre>${escapeHTML(item.joinWebUrl)}</pre>
      <h3>Raw request</h3>
      <pre>${escapeHTML(prettyJSON(item.rawRequest))}</pre>
    `;
  }
}

function prettyJSON(s) {
  if (!s) return "";
  try {
    return JSON.stringify(typeof s === "string" ? JSON.parse(s) : s, null, 2);
  } catch (e) {
    return String(s);
  }
}

function formatTime(ts) {
  if (!ts) return "";
  return new Date(ts).toLocaleTimeString();
}

function escapeHTML(s) {
  return String(s ?? "")
    .replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;").replace(/'/g, "&#39;");
}

els.tabs.forEach(tab => tab.addEventListener("click", () => {
  els.tabs.forEach(t => t.classList.remove("active"));
  tab.classList.add("active");
  els.mailList.classList.toggle("active", tab.dataset.tab === "mail");
  els.meetingsList.classList.toggle("active", tab.dataset.tab === "meetings");
  state.tab = tab.dataset.tab;
  state.selected = null;
  load();
}));

els.refresh.addEventListener("click", load);
els.clear.addEventListener("click", clearAll);

load();
setInterval(load, 3000);
