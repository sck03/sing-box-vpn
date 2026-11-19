(() => {
  try {
    const saved = localStorage.getItem("panel-theme");
    document.documentElement.dataset.theme = ["light", "dark"].includes(saved)
      ? saved
      : matchMedia("(prefers-color-scheme: light)").matches
        ? "light"
        : "dark";
  } catch {}
  document.addEventListener("click", (event) => {
    if (!(event.target instanceof document.defaultView.Element) || !event.target.closest("#theme-toggle")) return;
    const theme =
      document.documentElement.dataset.theme === "light" ? "dark" : "light";
    document.documentElement.dataset.theme = theme;
    try {
      localStorage.setItem("panel-theme", theme);
    } catch {}
  });
})();
