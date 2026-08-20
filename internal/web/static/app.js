(() => {
  const status = document.querySelector("[data-live-status]");
  const renderedTimestamp = Number(document.body.dataset.snapshotTimestamp || 0);
  if (!("EventSource" in window)) {
    if (status) status.textContent = "Live updates unavailable";
    return;
  }
  const events = new EventSource("/api/v1/events");
  events.addEventListener("snapshot", (event) => {
    const state = JSON.parse(event.data);
    if (!status) return;
    if (state.timestampUnixMs > renderedTimestamp) {
      status.textContent = "New local snapshot available · refresh to view · local only";
      return;
    }
    status.textContent = state.demo ? "Live · demo data · local only" : "Live · local only";
  });
  events.onerror = () => {
    if (status) status.textContent = "Reconnecting locally…";
  };
})();
