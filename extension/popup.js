const statusBadge = document.getElementById("statusBadge");
const interceptToggle = document.getElementById("interceptToggle");
const extensionsInput = document.getElementById("extensionsInput");
const saveExtsBtn = document.getElementById("saveExtsBtn");
const refreshBtn = document.getElementById("refreshBtn");
const quickUrlInput = document.getElementById("quickUrlInput");
const sendUrlBtn = document.getElementById("sendUrlBtn");
const feedback = document.getElementById("feedback");

function showFeedback(msg, isError = false) {
  feedback.textContent = msg;
  feedback.style.color = isError ? "#f87171" : "#93c5fd";
}

function setStatus(state, text) {
  statusBadge.className = `badge badge-${state}`;
  statusBadge.textContent = text;
}

async function checkConnection() {
  setStatus("checking", "Checking...");
  const resp = await chrome.runtime.sendMessage({ type: "ping" });
  if (!resp || resp.hostError) {
    setStatus("error", "Host Not Registered");
    showFeedback(resp?.error || "Run scripts/install-host.ps1 to register host.", true);
    return;
  }
  if (resp.success && resp.app_running) {
    setStatus("online", "Connected");
    showFeedback("MagicDown desktop app is online.");
  } else {
    setStatus("offline", "App Offline");
    showFeedback("Native host OK, but MagicDown desktop app is not running.");
  }
}

async function loadSettings() {
  const stored = await chrome.storage.local.get(["interceptEnabled", "extensions"]);
  interceptToggle.checked = stored.interceptEnabled !== false;
  const exts = Array.isArray(stored.extensions)
    ? stored.extensions
    : ["zip", "rar", "7z", "tar", "gz", "iso", "exe", "msi", "mp4", "mkv", "mp3", "pdf"];
  extensionsInput.value = exts.join(", ");
}

interceptToggle.addEventListener("change", async () => {
  await chrome.storage.local.set({ interceptEnabled: interceptToggle.checked });
  showFeedback(interceptToggle.checked ? "Download interception enabled." : "Download interception paused.");
});

saveExtsBtn.addEventListener("click", async () => {
  const parsed = extensionsInput.value
    .split(/[,\s]+/)
    .map((s) => s.trim().replace(/^\./, "").toLowerCase())
    .filter(Boolean);
  await chrome.storage.local.set({ extensions: parsed });
  extensionsInput.value = parsed.join(", ");
  showFeedback(`Saved ${parsed.length} file extension filter(s).`);
});

refreshBtn.addEventListener("click", checkConnection);

sendUrlBtn.addEventListener("click", async () => {
  const url = quickUrlInput.value.trim();
  if (!url || (!url.startsWith("http://") && !url.startsWith("https://"))) {
    showFeedback("Enter a valid http:// or https:// URL.", true);
    return;
  }
  const resp = await chrome.runtime.sendMessage({ type: "download", url });
  if (resp && resp.success) {
    quickUrlInput.value = "";
    showFeedback(`Sent to MagicDown (Task ${resp.taskId}).`);
  } else {
    showFeedback(resp?.error || "Failed to send download.", true);
  }
});

loadSettings();
checkConnection();
