(() => {
  const status = document.querySelector("[data-live-status]");
  const updated = document.querySelector("[data-updated-at]");
  const pressure = document.querySelector("[data-system-pressure]");
  if (!("EventSource" in window)) {
    if (status) status.textContent = "Live updates unavailable";
    return;
  }
  const events = new EventSource("/api/v1/events");
  events.addEventListener("snapshot", (event) => {
    const state = JSON.parse(event.data);
    if (status) status.textContent = state.demo ? "Live · demo data" : "Live · local only";
    if (updated) updated.textContent = new Date(state.timestampUnixMs).toLocaleTimeString();
    if (pressure) pressure.textContent = state.pressure.level;
  });
  events.onerror = () => {
    if (status) status.textContent = "Reconnecting locally…";
  };
})();
