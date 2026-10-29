// Client-side search, live reload, and delete confirmation for the note web view.
(function () {
  "use strict";

  // Search: filter the note list as you type. The query lives in the URL
  // hash so it survives live reloads.
  const search = document.getElementById("search");
  const list = document.getElementById("notes");
  if (search && list) {
    const rows = Array.from(list.querySelectorAll("li[data-search]"));
    const none = document.getElementById("no-results");

    const apply = () => {
      const terms = search.value.toLowerCase().split(/\s+/).filter(Boolean);
      let shown = 0;
      for (const row of rows) {
        const hay = row.dataset.search;
        const ok = terms.every((t) => hay.includes(t));
        row.hidden = !ok;
        if (ok) shown++;
      }
      if (none) none.hidden = shown > 0 || rows.length === 0;
      const hash = search.value ? "#q=" + encodeURIComponent(search.value) : "";
      history.replaceState(null, "", location.pathname + location.search + hash);
    };

    const m = location.hash.match(/^#q=(.*)$/);
    if (m) {
      search.value = decodeURIComponent(m[1]);
      apply();
    }
    search.addEventListener("input", apply);
    search.addEventListener("keydown", (e) => {
      if (e.key === "Escape") {
        search.value = "";
        apply();
        search.blur();
      }
    });
    document.addEventListener("keydown", (e) => {
      const typing = e.target.closest && e.target.closest("input, textarea, select");
      if (e.key === "/" && !typing && !e.metaKey && !e.ctrlKey && !e.altKey) {
        e.preventDefault();
        search.focus();
      }
    });
  }

  // Confirm destructive form submissions.
  for (const form of document.querySelectorAll("form[data-confirm]")) {
    form.addEventListener("submit", (e) => {
      if (!confirm(form.dataset.confirm)) e.preventDefault();
    });
  }

  // Live reload: the server sends "reload" when a note file changes.
  const live = document.getElementById("live");
  if (window.EventSource) {
    const es = new EventSource("/events");
    es.addEventListener("open", () => {
      if (live) {
        live.classList.add("on");
        live.title = "Live reload on";
      }
    });
    es.addEventListener("error", () => {
      if (live) {
        live.classList.remove("on");
        live.title = "Live reload disconnected; retrying…";
      }
    });
    es.addEventListener("reload", () => {
      es.close();
      location.reload();
    });
  }
})();
